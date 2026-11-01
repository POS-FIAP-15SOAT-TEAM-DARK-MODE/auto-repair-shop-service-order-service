package saga

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// Config holds the timeouts of the saga steps.
type Config struct {
	StepTimeout    time.Duration
	PaymentTimeout time.Duration
}

// Orchestrator drives the approval saga. Every public method runs in one unit of work; when a
// messaging transaction is already attached to the context the unit joins it.
type Orchestrator struct {
	uow    uow.Executor
	sos    domain.ServiceOrderRepository
	sagas  Repository
	outbox Outbox
	status *StatusChanger
	cfg    Config
	now    func() time.Time
}

func NewOrchestrator(u uow.Executor, sos domain.ServiceOrderRepository, sagas Repository, outbox Outbox, notifier domain.StatusNotifier, cfg Config) *Orchestrator {
	return &Orchestrator{
		uow:    u,
		sos:    sos,
		sagas:  sagas,
		outbox: outbox,
		status: NewStatusChanger(sos, outbox, notifier),
		cfg:    cfg,
		now:    time.Now,
	}
}

// Status exposes the status changer so the service can apply the local transitions.
func (o *Orchestrator) Status() *StatusChanger {
	return o.status
}

// Send starts the saga: IN_DIAGNOSIS order with at least one work, no RUNNING saga.
// The order stays IN_DIAGNOSIS until BudgetGenerated arrives.
func (o *Orchestrator) Send(ctx context.Context, soID, requestID string) error {
	return o.uow.Execute(ctx, func(ctx context.Context) error {
		so, err := domain.LoadAggregate(ctx, o.sos, soID)
		if err != nil {
			return err
		}
		if so.Status != domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS {
			return domain.ErrServiceOrderNotInDiagnosis
		}
		if len(so.Works) == 0 {
			return domain.ErrEmptyServicesList
		}
		running, err := o.sagas.FindRunningBySO(ctx, soID)
		if err != nil {
			return err
		}
		if running != nil {
			return domain.ErrSagaAlreadyRunning
		}

		deadline := o.now().Add(o.cfg.StepTimeout)
		inst := &Instance{
			ID: uuid.NewString(), ServiceOrderID: soID, Step: StepBudget, Status: StatusRunning,
			BudgetRequested: true, Deadline: &deadline,
		}
		if err := o.sagas.Insert(ctx, inst); err != nil {
			return err
		}

		return sendCommand(ctx, o.outbox, inst.ID, soID, requestID,
			specGenerateBudget, buildGenerateBudget(so))
	})
}

// Accept approves the budget on behalf of the customer identified by userID (JWT).
// The order goes to IN_PROGRESS at once and the payment is requested.
func (o *Orchestrator) Accept(ctx context.Context, soID, userID, requestID string) error {
	return o.decide(ctx, soID, userID, requestID, true)
}

// Reject refuses the budget: the order becomes REJECTED and the budget and stock are released.
func (o *Orchestrator) Reject(ctx context.Context, soID, userID, requestID string) error {
	return o.decide(ctx, soID, userID, requestID, false)
}

func (o *Orchestrator) decide(ctx context.Context, soID, userID, requestID string, accept bool) error {
	return o.uow.Execute(ctx, func(ctx context.Context) error {
		so, err := o.sos.FindByID(ctx, soID)
		if err != nil {
			return err
		}
		if so.Customer.UserID != userID {
			return domain.ErrInvalidCustomerProperty
		}
		if so.Status != domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL {
			return domain.ErrServiceOrderNotAwaitingApproval
		}
		inst, err := o.sagas.FindRunningBySO(ctx, soID)
		if err != nil {
			return err
		}
		if inst == nil || inst.Step != StepAwaitingCustomer {
			return domain.ErrSagaNotAwaitingCustomer
		}

		meta := Meta{SagaID: inst.ID, RequestID: requestID}
		if accept {
			if err := o.status.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_IN_PROGRESS, "", meta); err != nil {
				return err
			}
			deadline := o.now().Add(o.cfg.PaymentTimeout)
			inst.Step = StepPayment
			inst.PaymentRequested = true
			inst.Deadline = &deadline
			if err := o.sagas.Update(ctx, inst); err != nil {
				return err
			}
			return sendCommand(ctx, o.outbox, inst.ID, soID, requestID, specRequestPayment,
				requestPaymentPayload{Amount: money(so.TotalAmount)})
		}

		if err := o.status.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_REJECTED, domain.ReasonBudgetRejected, meta); err != nil {
			return err
		}
		return o.finishCompensated(ctx, inst, requestID, specCancelBudget, specReleaseStock)
	})
}

// Cancel is the user cancellation (any cancelable status). It compensates what the saga has
// already requested and releases the stock of the order.
func (o *Orchestrator) Cancel(ctx context.Context, soID, requestID string) error {
	return o.uow.Execute(ctx, func(ctx context.Context) error {
		so, err := o.sos.FindByID(ctx, soID)
		if err != nil {
			return err
		}
		if !so.Status.IsCancelable() {
			return domain.ErrServiceOrderNotCancelable
		}
		supplies, err := o.sos.ListSupplies(ctx, soID)
		if err != nil {
			return err
		}
		hasSupplies := len(supplies) > 0

		inst, err := o.sagas.FindRunningBySO(ctx, soID)
		if err != nil {
			return err
		}

		meta := Meta{RequestID: requestID}
		var specs []commandSpec
		if inst != nil {
			meta.SagaID = inst.ID
			specs = cancelCommands(inst, hasSupplies)
		} else if hasSupplies {
			specs = []commandSpec{specReleaseStock}
		}

		if err := o.status.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_CANCELLED, domain.ReasonUserCancelled, meta); err != nil {
			return err
		}
		if inst != nil {
			inst.Status = StatusCompensated
			inst.Deadline = nil
			if err := o.sagas.Update(ctx, inst); err != nil {
				return err
			}
		}
		return o.sendAll(ctx, meta.SagaID, soID, requestID, specs)
	})
}

// ExpireDeadlines compensates the RUNNING sagas whose step deadline has passed:
// BUDGET -> FAILED (order stays IN_DIAGNOSIS); PAYMENT or EXECUTION_QUEUE -> compensated and the
// order is CANCELLED with SAGA_TIMEOUT. Returns how many sagas were processed.
func (o *Orchestrator) ExpireDeadlines(ctx context.Context, limit int) (int, error) {
	candidates, err := o.sagas.FindExpired(ctx, o.now(), limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, cand := range candidates {
		err := o.uow.Execute(ctx, func(ctx context.Context) error {
			inst, err := o.sagas.FindByID(ctx, cand.ID)
			if err != nil {
				return err
			}
			if inst == nil || inst.Status != StatusRunning || inst.Deadline == nil || !inst.Deadline.Before(o.now()) {
				return nil
			}
			switch inst.Step {
			case StepBudget:
				inst.Status = StatusFailed
				inst.Deadline = nil
				if err := o.sagas.Update(ctx, inst); err != nil {
					return err
				}
				processed++
				return sendCommand(ctx, o.outbox, inst.ID, inst.ServiceOrderID, "", specCancelBudget, emptyPayload)
			case StepPayment, StepExecutionQueue:
				so, err := domain.LoadAggregate(ctx, o.sos, inst.ServiceOrderID)
				if err != nil {
					return err
				}
				supplies := len(so.Supplies) > 0
				meta := Meta{SagaID: inst.ID}
				if so.Status != domain.SERVICE_ORDER_STATUS_IN_PROGRESS {
					logger.Of(ctx).Warn("saga.deadline_unexpected_status", zap.String("saga_id", inst.ID),
						zap.String("service_order_id", so.ID), zap.String("status", so.Status.String()))
					inst.Status = StatusFailed
					inst.Deadline = nil
					processed++
					return o.sagas.Update(ctx, inst)
				}
				if err := o.status.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_CANCELLED, domain.ReasonSagaTimeout, meta); err != nil {
					return err
				}
				processed++
				return o.finishCompensated(ctx, inst, "", cancelCommands(inst, supplies)...)
			default:
				return nil
			}
		})
		if err != nil {
			return processed, err
		}
	}
	return processed, nil
}

// finishCompensated marks the saga COMPENSATED and sends the compensation commands in order.
func (o *Orchestrator) finishCompensated(ctx context.Context, inst *Instance, requestID string, specs ...commandSpec) error {
	inst.Status = StatusCompensated
	inst.Deadline = nil
	if err := o.sagas.Update(ctx, inst); err != nil {
		return err
	}
	return o.sendAll(ctx, inst.ID, inst.ServiceOrderID, requestID, specs)
}

// sendAll sends the compensation commands in order. Compensations carry an empty payload.
func (o *Orchestrator) sendAll(ctx context.Context, sagaID, soID, requestID string, specs []commandSpec) error {
	for _, spec := range specs {
		if err := sendCommand(ctx, o.outbox, sagaID, soID, requestID, spec, emptyPayload); err != nil {
			return err
		}
	}
	return nil
}

// RegisterReplies subscribes the saga handlers to the consumer. Unknown or log-only replies are
// accepted and discarded.
func (o *Orchestrator) RegisterReplies(c *messaging.Consumer) {
	c.RegisterHandler("BudgetGenerated", o.reply(o.onBudgetGenerated))
	c.RegisterHandler("PaymentApproved", o.reply(o.onPaymentApproved))
	c.RegisterHandler("PaymentFailed", o.reply(o.onPaymentFailed))
	c.RegisterHandler("ExecutionQueued", o.reply(o.onExecutionQueued))
	c.RegisterHandler("ExecutionCompleted", o.reply(o.onExecutionCompleted))
	c.RegisterHandler("ExecutionFailed", o.reply(o.onExecutionFailed))
	for _, typ := range []string{"BudgetCancelled", "PaymentRefunded", "ExecutionWorkStatusChanged"} {
		c.RegisterHandler(typ, func(ctx context.Context, _ *sql.Tx, env messaging.Envelope) error {
			logger.Of(ctx).Info("saga.reply_logged", zap.String("type", env.Type), zap.String("saga_id", env.SagaID))
			return nil
		})
	}
}

type replyFunc func(ctx context.Context, env messaging.Envelope) error

func (o *Orchestrator) reply(fn replyFunc) messaging.Handler {
	return func(ctx context.Context, _ *sql.Tx, env messaging.Envelope) error {
		return o.uow.Execute(ctx, func(ctx context.Context) error {
			return fn(ctx, env)
		})
	}
}

// sagaFor loads the saga named by the message and checks that it is RUNNING, belongs to the
// order and waits for one of the expected steps. Otherwise the message is ignored (nil, false).
func (o *Orchestrator) sagaFor(ctx context.Context, env messaging.Envelope, steps ...Step) (*Instance, bool, error) {
	inst, err := o.sagas.FindByID(ctx, env.SagaID)
	if err != nil {
		return nil, false, err
	}
	if inst == nil || inst.ServiceOrderID != env.ServiceOrderID || inst.Status != StatusRunning || !slices.Contains(steps, inst.Step) {
		logger.Of(ctx).Info("saga.reply_ignored", zap.String("type", env.Type), zap.String("saga_id", env.SagaID))
		return nil, false, nil
	}
	return inst, true, nil
}

func (o *Orchestrator) onBudgetGenerated(ctx context.Context, env messaging.Envelope) error {
	var p struct {
		TotalAmount string `json:"total_amount"`
	}
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	total, err := decimal.NewFromString(p.TotalAmount)
	if err != nil {
		return err
	}
	inst, ok, err := o.sagaFor(ctx, env, StepBudget)
	if err != nil || !ok {
		return err
	}
	so, err := o.sos.FindByID(ctx, env.ServiceOrderID)
	if err != nil {
		return err
	}
	if so.Status != domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS {
		logger.Of(ctx).Info("saga.reply_ignored", zap.String("type", env.Type), zap.String("saga_id", env.SagaID))
		return nil
	}

	so.TotalAmount = total
	quoted := o.now()
	so.QuoteSentAt = &quoted
	if err := o.status.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL, "", Meta{SagaID: inst.ID, RequestID: env.RequestID}); err != nil {
		return err
	}
	inst.Step = StepAwaitingCustomer
	inst.Deadline = nil
	return o.sagas.Update(ctx, inst)
}

func (o *Orchestrator) onPaymentApproved(ctx context.Context, env messaging.Envelope) error {
	inst, ok, err := o.sagaFor(ctx, env, StepPayment)
	if err != nil || !ok {
		return err
	}
	so, err := domain.LoadAggregate(ctx, o.sos, env.ServiceOrderID)
	if err != nil {
		return err
	}

	deadline := o.now().Add(o.cfg.StepTimeout)
	inst.PaymentApproved = true
	inst.Step = StepExecutionQueue
	inst.ExecutionEnqueued = true
	inst.Deadline = &deadline
	if err := o.sagas.Update(ctx, inst); err != nil {
		return err
	}

	works := make([]executionWork, 0, len(so.Works))
	for _, w := range so.Works {
		works = append(works, executionWork{WorkID: w.ID, Name: w.Name})
	}
	return sendCommand(ctx, o.outbox, inst.ID, so.ID, env.RequestID, specEnqueueExecution,
		enqueueExecutionPayload{Works: works})
}

func (o *Orchestrator) onPaymentFailed(ctx context.Context, env messaging.Envelope) error {
	inst, ok, err := o.sagaFor(ctx, env, StepPayment)
	if err != nil || !ok {
		return err
	}
	so, err := o.sos.FindByID(ctx, env.ServiceOrderID)
	if err != nil {
		return err
	}
	if so.Status != domain.SERVICE_ORDER_STATUS_IN_PROGRESS {
		logger.Of(ctx).Info("saga.reply_ignored", zap.String("type", env.Type), zap.String("saga_id", env.SagaID))
		return nil
	}
	if err := o.status.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_REJECTED, domain.ReasonPaymentRejected,
		Meta{SagaID: inst.ID, RequestID: env.RequestID}); err != nil {
		return err
	}
	return o.finishCompensated(ctx, inst, env.RequestID, specCancelBudget, specReleaseStock)
}

func (o *Orchestrator) onExecutionQueued(ctx context.Context, env messaging.Envelope) error {
	inst, ok, err := o.sagaFor(ctx, env, StepExecutionQueue)
	if err != nil || !ok {
		return err
	}
	inst.Step = StepExecution
	inst.Deadline = nil
	return o.sagas.Update(ctx, inst)
}

func (o *Orchestrator) onExecutionCompleted(ctx context.Context, env messaging.Envelope) error {
	inst, ok, err := o.sagaFor(ctx, env, StepExecution)
	if err != nil || !ok {
		return err
	}
	so, err := o.sos.FindByID(ctx, env.ServiceOrderID)
	if err != nil {
		return err
	}
	if so.Status != domain.SERVICE_ORDER_STATUS_IN_PROGRESS {
		logger.Of(ctx).Info("saga.reply_ignored", zap.String("type", env.Type), zap.String("saga_id", env.SagaID))
		return nil
	}
	if err := o.status.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_COMPLETED, "", Meta{SagaID: inst.ID, RequestID: env.RequestID}); err != nil {
		return err
	}
	inst.Status = StatusCompleted
	inst.Step = StepDone
	inst.Deadline = nil
	return o.sagas.Update(ctx, inst)
}

func (o *Orchestrator) onExecutionFailed(ctx context.Context, env messaging.Envelope) error {
	inst, ok, err := o.sagaFor(ctx, env, StepExecutionQueue, StepExecution)
	if err != nil || !ok {
		return err
	}
	so, err := o.sos.FindByID(ctx, env.ServiceOrderID)
	if err != nil {
		return err
	}
	if so.Status != domain.SERVICE_ORDER_STATUS_IN_PROGRESS {
		logger.Of(ctx).Info("saga.reply_ignored", zap.String("type", env.Type), zap.String("saga_id", env.SagaID))
		return nil
	}
	if err := o.status.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_CANCELLED, domain.ReasonExecutionFailed,
		Meta{SagaID: inst.ID, RequestID: env.RequestID}); err != nil {
		return err
	}
	return o.finishCompensated(ctx, inst, env.RequestID, specRefundPayment, specCancelBudget, specReleaseStock)
}
