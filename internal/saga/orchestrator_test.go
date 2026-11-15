package saga

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/repository"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	soID   = "01JD8Z4R3Q8M2V7K5N9XWABCDE"
	userID = "3b123072-5ad5-4df8-8d3b-3c2770f11b2a"
)

type countingNotifier struct {
	calls int
	err   error
}

func (n *countingNotifier) NotifyStatusChange(context.Context, domain.StatusNotification) error {
	n.calls++
	return n.err
}

type fixture struct {
	orch     *Orchestrator
	sos      *repository.InMemory
	sagas    *MemoryRepository
	box      *MemoryOutbox
	notifier *countingNotifier
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		sos:      repository.NewInMemory(),
		sagas:    NewMemoryRepository(),
		box:      NewMemoryOutbox(),
		notifier: &countingNotifier{},
		now:      time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
	f.orch = NewOrchestrator(uow.NewInMemoryUoW(), f.sos, f.sagas, f.box, f.notifier, Config{
		StepTimeout:    2 * time.Minute,
		PaymentTimeout: 24 * time.Hour,
	})
	f.orch.now = func() time.Time { return f.now }
	return f
}

// seed stores an order with one work and one supply, in the given status.
func (f *fixture) seed(t *testing.T, status domain.SERVICE_ORDER_STATUS) {
	t.Helper()
	ctx := context.Background()
	so := &domain.ServiceOrder{
		ID:       soID,
		Status:   status,
		Customer: domain.CustomerSnapshot{ID: "c1", UserID: userID, Name: "João", Email: "joao@example.com"},
		Vehicle:  domain.VehicleSnapshot{ID: "v1", LicensePlate: "ABC1D23", Brand: "Fiat", Model: "Uno", Year: 2018},
	}
	require.NoError(t, f.sos.Insert(ctx, so))
	require.NoError(t, f.sos.AddWork(ctx, soID, domain.WorkItem{ID: "w1", Name: "Troca de óleo", UnitPrice: decimal.RequireFromString("150.00")}))
	require.NoError(t, f.sos.AddSupply(ctx, soID, domain.SupplyItem{ID: "s1", ReservationID: "r1", Name: "Filtro",
		UnitPrice: decimal.RequireFromString("35.50"), Version: 3, Quantity: 2}))
	total := domain.ComputeTotal([]domain.WorkItem{{UnitPrice: decimal.RequireFromString("150.00")}},
		[]domain.SupplyItem{{UnitPrice: decimal.RequireFromString("35.50"), Quantity: 2}})
	require.NoError(t, f.sos.UpdateTotal(ctx, soID, total))
}

func (f *fixture) status(t *testing.T) domain.SERVICE_ORDER_STATUS {
	t.Helper()
	st, err := f.sos.FindStatus(context.Background(), soID)
	require.NoError(t, err)
	return st
}

func (f *fixture) saga(t *testing.T) *Instance {
	t.Helper()
	inst, err := f.sagas.FindRunningBySO(context.Background(), soID)
	require.NoError(t, err)
	return inst
}

func (f *fixture) sent(t *testing.T) []string {
	t.Helper()
	return f.box.Types()
}

func (f *fixture) resetOutbox() {
	f.box.Sent = nil
}

func reply(t *testing.T, typ, sagaID string, payload any) messaging.Envelope {
	t.Helper()
	env, err := messaging.NewEnvelope(typ, sagaID, soID, "req-1", payload)
	require.NoError(t, err)
	return env
}

// startAndBudget runs send and BudgetGenerated, leaving the order AWAITING_APPROVAL.
func (f *fixture) startAndBudget(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.orch.Send(ctx, soID, "req-1"))
	inst := f.saga(t)
	require.NotNil(t, inst)
	require.NoError(t, f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", inst.ID,
		map[string]string{"budget_id": "b1", "total_amount": "221.00"})))
	f.resetOutbox()
	return inst.ID
}

// startAndPay runs through accept and the approved payment, leaving the saga in EXECUTION_QUEUE.
func (f *fixture) startAndPay(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	sagaID := f.startAndBudget(t)
	require.NoError(t, f.orch.Accept(ctx, soID, userID, "req-1"))
	require.NoError(t, f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", sagaID,
		map[string]string{"payment_id": "p1", "amount": "221.00"})))
	f.resetOutbox()
	return sagaID
}

func TestSendStartsSagaAndKeepsStatus(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()

	require.NoError(t, f.orch.Send(ctx, soID, "req-1"))

	assert.Equal(t, []string{"GenerateBudget"}, f.sent(t))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, f.status(t))
	inst := f.saga(t)
	require.NotNil(t, inst)
	assert.Equal(t, StepBudget, inst.Step)
	assert.True(t, inst.BudgetRequested)
	require.NotNil(t, inst.Deadline)
	assert.True(t, inst.Deadline.Equal(f.now.Add(2*time.Minute)))

	env := f.box.Sent[0]
	assert.Equal(t, TopicSagaCommands, env.Topic)
	assert.Equal(t, TargetBilling, env.Target)
	assert.Equal(t, inst.ID, env.Env.SagaID)
	var payload generateBudgetPayload
	require.NoError(t, json.Unmarshal(env.Env.Payload, &payload))
	assert.Equal(t, userID, payload.CustomerUserID)
	require.Len(t, payload.Works, 1)
	assert.Equal(t, "150.00", payload.Works[0].UnitPrice)
	require.Len(t, payload.Supplies, 1)
	assert.Equal(t, 2, payload.Supplies[0].Quantity)
	assert.Equal(t, "35.50", payload.Supplies[0].UnitPrice)
}

func TestSendGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("order not in diagnosis", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_RECEIVED)
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), domain.ErrServiceOrderNotInDiagnosis)
	})
	t.Run("no works", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.sos.RemoveWork(ctx, soID, "w1"))
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), domain.ErrEmptyServicesList)
	})
	t.Run("saga already running", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), domain.ErrSagaAlreadyRunning)
	})
	t.Run("missing order", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), domain.ErrServiceOrderNotFound)
	})
}

func TestHappyPathToCompleted(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()

	sagaID := f.startAndBudget(t)
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL, f.status(t))
	inst := f.saga(t)
	assert.Equal(t, StepAwaitingCustomer, inst.Step)
	assert.Nil(t, inst.Deadline, "no deadline while the customer decides")

	// accept: IN_PROGRESS right away, payment requested with the total
	require.NoError(t, f.orch.Accept(ctx, soID, userID, "req-1"))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_IN_PROGRESS, f.status(t))
	assert.Equal(t, []string{"ServiceOrderStatusChanged", "RequestPayment"}, f.sent(t))
	var pay requestPaymentPayload
	require.NoError(t, json.Unmarshal(f.box.Sent[1].Env.Payload, &pay))
	assert.Equal(t, "221.00", pay.Amount)
	assert.Equal(t, StepPayment, f.saga(t).Step)
	f.resetOutbox()

	require.NoError(t, f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", sagaID, map[string]string{"payment_id": "p1", "amount": "221.00"})))
	assert.Equal(t, []string{"EnqueueExecution"}, f.sent(t))
	assert.Equal(t, TargetExecution, f.box.Sent[0].Target)
	assert.Equal(t, StepExecutionQueue, f.saga(t).Step)
	f.resetOutbox()

	require.NoError(t, f.orch.onExecutionQueued(ctx, reply(t, "ExecutionQueued", sagaID, map[string]string{"task_id": "t1"})))
	assert.Equal(t, StepExecution, f.saga(t).Step)

	require.NoError(t, f.orch.onExecutionCompleted(ctx, reply(t, "ExecutionCompleted", sagaID, map[string]string{"task_id": "t1"})))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_COMPLETED, f.status(t))
	inst, err := f.sagas.FindByID(ctx, sagaID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, inst.Status)
	assert.Equal(t, StepDone, inst.Step)
	assert.Equal(t, []string{"ServiceOrderStatusChanged"}, f.sent(t), "only the status event is published on completion")
}

func TestBudgetRejectedCompensates(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	sagaID := f.startAndBudget(t)

	require.NoError(t, f.orch.Reject(context.Background(), soID, userID, "req-1"))

	assert.Equal(t, domain.SERVICE_ORDER_STATUS_REJECTED, f.status(t))
	assert.Equal(t, []string{"ServiceOrderStatusChanged", "CancelBudget", "ReleaseStock"}, f.sent(t))
	assert.Equal(t, StatusCompensated, f.sagas.items[sagaID].Status)
	evt := f.box.Sent[0].Env
	assert.Contains(t, string(evt.Payload), `"reason":"BUDGET_REJECTED"`)
}

func TestPaymentFailedCompensates(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	sagaID := f.startAndBudget(t)
	require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
	f.resetOutbox()

	require.NoError(t, f.orch.onPaymentFailed(ctx, reply(t, "PaymentFailed", sagaID,
		map[string]string{"payment_id": "p1", "amount": "221.00", "reason": "PAYMENT_REJECTED"})))

	assert.Equal(t, domain.SERVICE_ORDER_STATUS_REJECTED, f.status(t))
	assert.Equal(t, []string{"ServiceOrderStatusChanged", "CancelBudget", "ReleaseStock"}, f.sent(t))
	assert.Contains(t, string(f.box.Sent[0].Env.Payload), `"reason":"PAYMENT_REJECTED"`)
}

func TestExecutionFailedRefundsAndCancels(t *testing.T) {
	for _, step := range []Step{StepExecutionQueue, StepExecution} {
		t.Run(string(step), func(t *testing.T) {
			f := newFixture(t)
			f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
			ctx := context.Background()
			sagaID := f.startAndPay(t)
			if step == StepExecution {
				require.NoError(t, f.orch.onExecutionQueued(ctx, reply(t, "ExecutionQueued", sagaID, map[string]string{"task_id": "t1"})))
			}

			require.NoError(t, f.orch.onExecutionFailed(ctx, reply(t, "ExecutionFailed", sagaID,
				map[string]string{"task_id": "t1", "reason": "ALL_WORKS_CANCELLED"})))

			assert.Equal(t, domain.SERVICE_ORDER_STATUS_CANCELLED, f.status(t))
			assert.Equal(t, []string{"ServiceOrderStatusChanged", "RefundPayment", "CancelBudget", "ReleaseStock"}, f.sent(t))
			assert.Contains(t, string(f.box.Sent[0].Env.Payload), `"reason":"EXECUTION_FAILED"`)
		})
	}
}

func TestRepliesOutOfStateAreIgnored(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	sagaID := f.startAndBudget(t) // saga in AWAITING_CUSTOMER

	// wrong step: PaymentApproved before the payment was requested
	require.NoError(t, f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", sagaID, map[string]string{})))
	// unknown saga
	require.NoError(t, f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", "missing", map[string]string{})))
	// budget reply for a saga that is not waiting for it
	require.NoError(t, f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", sagaID, map[string]string{"total_amount": "1.00"})))
	// execution events without a saga in those steps
	require.NoError(t, f.orch.onExecutionQueued(ctx, reply(t, "ExecutionQueued", sagaID, map[string]string{})))
	require.NoError(t, f.orch.onExecutionCompleted(ctx, reply(t, "ExecutionCompleted", sagaID, map[string]string{})))
	require.NoError(t, f.orch.onExecutionFailed(ctx, reply(t, "ExecutionFailed", sagaID, map[string]string{})))
	require.NoError(t, f.orch.onPaymentFailed(ctx, reply(t, "PaymentFailed", sagaID, map[string]string{})))

	assert.Empty(t, f.sent(t))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL, f.status(t))
	assert.Equal(t, StepAwaitingCustomer, f.saga(t).Step)
}

func TestReplyForWrongOrderIsIgnored(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	sagaID := f.startAndBudget(t)
	env, err := messaging.NewEnvelope("BudgetGenerated", sagaID, "OTHER", "req", map[string]string{"total_amount": "1.00"})
	require.NoError(t, err)
	require.NoError(t, f.orch.onBudgetGenerated(ctx, env))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL, f.status(t))
}

func TestReplyWithBadPayloadFails(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	f.startAndBudget(t)
	env := messaging.Envelope{MessageID: "b7a1c6f0-2f64-4f6e-9a49-0d5a0b1e6c11", SagaID: "x", Type: "BudgetGenerated", Payload: json.RawMessage(`{`)}
	assert.Error(t, f.orch.onBudgetGenerated(ctx, env))
}

func TestAcceptAndRejectGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("wrong customer", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, "someone-else", ""), domain.ErrInvalidCustomerProperty)
		assert.ErrorIs(t, f.orch.Reject(ctx, soID, "someone-else", ""), domain.ErrInvalidCustomerProperty)
	})
	t.Run("budget not generated yet", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, userID, ""), domain.ErrServiceOrderNotAwaitingApproval)
	})
	t.Run("already decided", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, userID, ""), domain.ErrServiceOrderNotAwaitingApproval)
		assert.ErrorIs(t, f.orch.Reject(ctx, soID, userID, ""), domain.ErrServiceOrderNotAwaitingApproval)
	})
	t.Run("saga not awaiting the customer", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL)
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, userID, ""), domain.ErrSagaNotAwaitingCustomer)
	})
}

// TestCancelCompensatesEachStep checks the commands sent by a user cancellation in every saga step.
func TestCancelCompensatesEachStep(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		setup    func(t *testing.T, f *fixture)
		expected []string
	}{
		"budget step": {
			setup: func(t *testing.T, f *fixture) {
				require.NoError(t, f.orch.Send(ctx, soID, ""))
			},
			expected: []string{"ServiceOrderStatusChanged", "CancelBudget", "ReleaseStock"},
		},
		"awaiting customer": {
			setup:    func(t *testing.T, f *fixture) { f.startAndBudget(t) },
			expected: []string{"ServiceOrderStatusChanged", "CancelBudget", "ReleaseStock"},
		},
		"payment step": {
			setup: func(t *testing.T, f *fixture) {
				f.startAndBudget(t)
				require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
			},
			expected: []string{"ServiceOrderStatusChanged", "RefundPayment", "CancelBudget", "ReleaseStock"},
		},
		"execution queue step": {
			setup:    func(t *testing.T, f *fixture) { f.startAndPay(t) },
			expected: []string{"ServiceOrderStatusChanged", "CancelExecution", "RefundPayment", "CancelBudget", "ReleaseStock"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
			tc.setup(t, f)
			f.resetOutbox()

			require.NoError(t, f.orch.Cancel(ctx, soID, "req-1"))

			assert.Equal(t, domain.SERVICE_ORDER_STATUS_CANCELLED, f.status(t))
			assert.Equal(t, tc.expected, f.sent(t))
			assert.Contains(t, string(f.box.Sent[0].Env.Payload), `"reason":"USER_CANCELLED"`)
			assert.Equal(t, StatusCompensated, f.sagas.items[f.box.Sent[0].Env.SagaID].Status)
		})
	}
}

func TestCancelWithoutSagaReleasesStockOnlyWhenSuppliesExist(t *testing.T) {
	ctx := context.Background()

	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW)
	require.NoError(t, f.orch.Cancel(ctx, soID, ""))
	assert.Equal(t, []string{"ServiceOrderStatusChanged", "ReleaseStock"}, f.sent(t))

	g := newFixture(t)
	g.seed(t, domain.SERVICE_ORDER_STATUS_NEW)
	_, err := g.sos.RemoveSupply(ctx, soID, "s1")
	require.NoError(t, err)
	require.NoError(t, g.orch.Cancel(ctx, soID, ""))
	assert.Equal(t, []string{"ServiceOrderStatusChanged"}, g.sent(t))
}

func TestCancelGuards(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_COMPLETED)
	assert.ErrorIs(t, f.orch.Cancel(ctx, soID, ""), domain.ErrServiceOrderNotCancelable)
	assert.ErrorIs(t, newFixture(t).orch.Cancel(ctx, soID, ""), domain.ErrServiceOrderNotFound)
}

func TestDeadlineBudgetFailsSagaAndKeepsDiagnosis(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	require.NoError(t, f.orch.Send(ctx, soID, ""))
	f.resetOutbox()

	f.now = f.now.Add(3 * time.Minute)
	n, err := f.orch.ExpireDeadlines(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	assert.Equal(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, f.status(t))
	assert.Equal(t, []string{"CancelBudget"}, f.sent(t))
	running, err := f.sagas.FindRunningBySO(ctx, soID)
	require.NoError(t, err)
	assert.Nil(t, running, "the failed saga no longer blocks a new send")

	// a new send is allowed again
	require.NoError(t, f.orch.Send(ctx, soID, ""))
}

func TestDeadlinePaymentCancelsOrder(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	f.startAndBudget(t)
	require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
	f.resetOutbox()

	f.now = f.now.Add(25 * time.Hour)
	n, err := f.orch.ExpireDeadlines(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	assert.Equal(t, domain.SERVICE_ORDER_STATUS_CANCELLED, f.status(t))
	assert.Equal(t, []string{"ServiceOrderStatusChanged", "RefundPayment", "CancelBudget", "ReleaseStock"}, f.sent(t))
	assert.Contains(t, string(f.box.Sent[0].Env.Payload), `"reason":"SAGA_TIMEOUT"`)
}

func TestDeadlineNotReachedDoesNothing(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	require.NoError(t, f.orch.Send(ctx, soID, ""))
	f.resetOutbox()

	n, err := f.orch.ExpireDeadlines(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, f.sent(t))
}

func TestDeadlineSkipsStepsWithoutDeadline(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	f.startAndBudget(t) // AWAITING_CUSTOMER has no deadline
	f.now = f.now.Add(48 * time.Hour)
	n, err := f.orch.ExpireDeadlines(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestDeadlineOrderNotInProgressMarksSagaFailed(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	f.startAndBudget(t)
	require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
	// inconsistent state: the order was moved away from IN_PROGRESS by someone else
	so, _ := f.sos.FindByID(ctx, soID)
	so.Status = domain.SERVICE_ORDER_STATUS_COMPLETED
	require.NoError(t, f.sos.Update(ctx, &so))
	f.now = f.now.Add(48 * time.Hour)

	n, err := f.orch.ExpireDeadlines(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	for _, inst := range f.sagas.items {
		assert.Equal(t, StatusFailed, inst.Status)
	}
}

func TestStatusChangesPublishEventsAndNotify(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	f.startAndBudget(t)
	require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))

	assert.GreaterOrEqual(t, f.notifier.calls, 2, "budget and accept notify the customer")
}

func TestNotifierFailureDoesNotFailTransition(t *testing.T) {
	f := newFixture(t)
	f.notifier.err = assert.AnError
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	require.NoError(t, f.orch.Send(ctx, soID, ""))
	require.NoError(t, f.orch.Cancel(ctx, soID, ""))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_CANCELLED, f.status(t))
}

func TestStatusChangerRejectsIllegalTransition(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	so, _ := f.sos.FindByID(ctx, soID)
	err := f.orch.Status().Change(ctx, &so, domain.SERVICE_ORDER_STATUS_DELIVERED, "", Meta{})
	assert.ErrorIs(t, err, domain.ErrServiceOrderInvalidTransition)
}

func TestPostgresOutboxRequiresTransaction(t *testing.T) {
	err := PGOutbox{}.Enqueue(context.Background(), TopicSOEvents, "", messaging.Envelope{})
	assert.ErrorIs(t, err, uow.ErrMissingPostgresTransaction)
}

func TestMoneyFormatting(t *testing.T) {
	assert.Equal(t, "221.00", money(decimal.RequireFromString("221")))
	assert.Equal(t, "35.50", money(decimal.RequireFromString("35.5")))
}
