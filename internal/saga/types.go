// Package saga implements the orchestrated approval saga "service-order-approval" (SPEC 3.2):
// the Service Order owns the saga state, sends commands to Billing, Execution and Catalog, and
// reacts to their replies. Every transition is one database transaction.
package saga

import (
	"context"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
)

type Step string

const (
	StepBudget           Step = "BUDGET"
	StepAwaitingCustomer Step = "AWAITING_CUSTOMER"
	StepPayment          Step = "PAYMENT"
	StepExecutionQueue   Step = "EXECUTION_QUEUE"
	StepExecution        Step = "EXECUTION"
	StepDone             Step = "DONE"
)

type Status string

const (
	StatusRunning     Status = "RUNNING"
	StatusCompleted   Status = "COMPLETED"
	StatusCompensated Status = "COMPENSATED"
	StatusFailed      Status = "FAILED"
)

// Logical topics and SNS target attribute values (contracts/messaging.md).
const (
	TopicSagaCommands = "saga-commands"
	TopicSOEvents     = "so-events"

	TargetBilling   = "billing"
	TargetExecution = "execution"
	TargetCatalog   = "catalog"
)

// Command and event types.
const (
	CmdGenerateBudget   = "GenerateBudget"
	CmdCancelBudget     = "CancelBudget"
	CmdRequestPayment   = "RequestPayment"
	CmdRefundPayment    = "RefundPayment"
	CmdEnqueueExecution = "EnqueueExecution"
	CmdCancelExecution  = "CancelExecution"
	CmdReleaseStock     = "ReleaseStock"

	EvtServiceOrderStatusChanged = "ServiceOrderStatusChanged"
)

// Instance is a row of saga_instance.
type Instance struct {
	ID                string
	ServiceOrderID    string
	Step              Step
	Status            Status
	BudgetRequested   bool
	PaymentRequested  bool
	PaymentApproved   bool
	ExecutionEnqueued bool
	Deadline          *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Repository persists saga instances. Lookups that are followed by a change lock the row.
type Repository interface {
	Insert(ctx context.Context, inst *Instance) error
	Update(ctx context.Context, inst *Instance) error
	// FindByID returns the instance (locked) or nil when it does not exist.
	FindByID(ctx context.Context, id string) (*Instance, error)
	// FindRunningBySO returns the RUNNING saga of the order or nil.
	FindRunningBySO(ctx context.Context, serviceOrderID string) (*Instance, error)
	// FindExpired lists RUNNING sagas whose deadline is before now.
	FindExpired(ctx context.Context, now time.Time, limit int) ([]Instance, error)
}

// Outbox stores a message to be published by the relay. Enqueue must run inside the
// transaction of the current unit of work.
type Outbox interface {
	Enqueue(ctx context.Context, topic, target string, env messaging.Envelope) error
}

// Meta carries the correlation ids of a transition.
type Meta struct {
	SagaID    string
	RequestID string
}

// commandSpec identifies a command to send; the payload is decided by the caller.
type commandSpec struct {
	typ    string
	target string
}

var (
	specCancelBudget  = commandSpec{CmdCancelBudget, TargetBilling}
	specRefundPayment = commandSpec{CmdRefundPayment, TargetBilling}
	specCancelExec    = commandSpec{CmdCancelExecution, TargetExecution}
	specReleaseStock  = commandSpec{CmdReleaseStock, TargetCatalog}
	emptyPayload      = struct{}{}
)

// cancelCommands returns the compensations of a saga, in reverse order of the steps:
// CancelExecution, RefundPayment, CancelBudget and ReleaseStock (when the order has supplies).
func cancelCommands(inst *Instance, hasSupplies bool) []commandSpec {
	var specs []commandSpec
	if inst.ExecutionEnqueued {
		specs = append(specs, specCancelExec)
	}
	if inst.PaymentRequested {
		specs = append(specs, specRefundPayment)
	}
	if inst.BudgetRequested {
		specs = append(specs, specCancelBudget)
	}
	if hasSupplies {
		specs = append(specs, specReleaseStock)
	}
	return specs
}

// sendCommand builds an envelope and stores it in the outbox.
func sendCommand(ctx context.Context, box Outbox, sagaID, soID, requestID string, spec commandSpec, payload any) error {
	env, err := messaging.NewEnvelope(spec.typ, sagaID, soID, requestID, payload)
	if err != nil {
		return err
	}
	return box.Enqueue(ctx, TopicSagaCommands, spec.target, env)
}

// PGOutbox writes the outbox row through the transaction attached to the context.
type PGOutbox struct{}

func (PGOutbox) Enqueue(ctx context.Context, topic, target string, env messaging.Envelope) error {
	tx, err := uow.GetTransaction(ctx)
	if err != nil {
		return err
	}
	return messaging.Enqueue(ctx, tx, topic, target, env)
}
