package saga

import (
	"context"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingRepo wraps the in-memory saga repository and fails the chosen operations.
type failingRepo struct {
	*MemoryRepository
	insertErr, updateErr, findErr, runningErr, expiredErr error
	hideByID                                              bool
}

func (r *failingRepo) Insert(ctx context.Context, inst *Instance) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	return r.MemoryRepository.Insert(ctx, inst)
}

func (r *failingRepo) Update(ctx context.Context, inst *Instance) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.MemoryRepository.Update(ctx, inst)
}

func (r *failingRepo) FindByID(ctx context.Context, id string) (*Instance, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.hideByID {
		return nil, nil
	}
	return r.MemoryRepository.FindByID(ctx, id)
}

func (r *failingRepo) FindRunningBySO(ctx context.Context, soID string) (*Instance, error) {
	if r.runningErr != nil {
		return nil, r.runningErr
	}
	return r.MemoryRepository.FindRunningBySO(ctx, soID)
}

func (r *failingRepo) FindExpired(ctx context.Context, now time.Time, limit int) ([]Instance, error) {
	if r.expiredErr != nil {
		return nil, r.expiredErr
	}
	return r.MemoryRepository.FindExpired(ctx, now, limit)
}

// failingOutbox fails the call number failAt (1-based); 0 means never.
type failingOutbox struct {
	*MemoryOutbox
	failAt int
	calls  int
}

var errOutbox = assert.AnError

func (o *failingOutbox) Enqueue(ctx context.Context, topic, target string, env messaging.Envelope) error {
	o.calls++
	if o.failAt > 0 && o.calls == o.failAt {
		return errOutbox
	}
	return o.MemoryOutbox.Enqueue(ctx, topic, target, env)
}

// failingSOS wraps the in-memory order repository and fails the chosen operations.
type failingSOS struct {
	domain.ServiceOrderRepository
	findErr, updateErr, listWorksErr, listSuppliesErr error
}

func (r *failingSOS) FindByID(ctx context.Context, id string) (domain.ServiceOrder, error) {
	if r.findErr != nil {
		return domain.ServiceOrder{}, r.findErr
	}
	return r.ServiceOrderRepository.FindByID(ctx, id)
}

func (r *failingSOS) Update(ctx context.Context, so *domain.ServiceOrder) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.ServiceOrderRepository.Update(ctx, so)
}

func (r *failingSOS) ListWorks(ctx context.Context, id string) ([]domain.WorkItem, error) {
	if r.listWorksErr != nil {
		return nil, r.listWorksErr
	}
	return r.ServiceOrderRepository.ListWorks(ctx, id)
}

func (r *failingSOS) ListSupplies(ctx context.Context, id string) ([]domain.SupplyItem, error) {
	if r.listSuppliesErr != nil {
		return nil, r.listSuppliesErr
	}
	return r.ServiceOrderRepository.ListSupplies(ctx, id)
}

// failingUoW never runs the steps.
type failingUoW struct{ err error }

func (u failingUoW) Execute(context.Context, ...uow.Step) error { return u.err }

// wire replaces the orchestrator dependencies with failing ones, keeping the same stores.
func (f *fixture) wire(sos domain.ServiceOrderRepository, sagas Repository, box Outbox) {
	f.orch.sos = sos
	f.orch.sagas = sagas
	f.orch.outbox = box
	f.orch.status = NewStatusChanger(sos, box, f.notifier)
}

func TestSendPropagatesFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("order lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), assert.AnError)
	})
	t.Run("works lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, listWorksErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), assert.AnError)
	})
	t.Run("running saga lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, runningErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), assert.AnError)
	})
	t.Run("saga insert fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, insertErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), assert.AnError)
		assert.Empty(t, f.sent(t), "nothing is sent when the saga is not stored")
	})
	t.Run("outbox fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 1})
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), errOutbox)
	})
	t.Run("unit of work fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.orch.uow = failingUoW{err: assert.AnError}
		assert.ErrorIs(t, f.orch.Send(ctx, soID, ""), assert.AnError)
	})
}

func TestDecisionPropagatesFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("order lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, userID, ""), assert.AnError)
	})
	t.Run("running saga lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, runningErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, userID, ""), assert.AnError)
	})
	t.Run("accept: status change fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, updateErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, userID, ""), assert.AnError)
		assert.Equal(t, domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL, f.status(t))
	})
	t.Run("accept: saga update fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, userID, ""), assert.AnError)
	})
	t.Run("accept: payment command cannot be stored", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		// accept emits the status event (1) and RequestPayment (2)
		f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 2})
		assert.ErrorIs(t, f.orch.Accept(ctx, soID, userID, ""), errOutbox)
	})
	t.Run("reject: status change fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, updateErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.Reject(ctx, soID, userID, ""), assert.AnError)
	})
	t.Run("reject: saga update fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.Reject(ctx, soID, userID, ""), assert.AnError)
	})
	t.Run("reject: compensation command fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		// reject emits the status event (1), CancelBudget (2), ReleaseStock (3)
		f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 3})
		assert.ErrorIs(t, f.orch.Reject(ctx, soID, userID, ""), errOutbox)
	})
}

func TestCancelPropagatesFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("order lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.Cancel(ctx, soID, ""), assert.AnError)
	})
	t.Run("supplies lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, listSuppliesErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.Cancel(ctx, soID, ""), assert.AnError)
	})
	t.Run("running saga lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, runningErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.Cancel(ctx, soID, ""), assert.AnError)
	})
	t.Run("status change fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, updateErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.Cancel(ctx, soID, ""), assert.AnError)
	})
	t.Run("running saga update fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.Cancel(ctx, soID, ""), assert.AnError)
	})
	t.Run("compensation command fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW)
		// status event (1), ReleaseStock (2)
		f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 2})
		assert.ErrorIs(t, f.orch.Cancel(ctx, soID, ""), errOutbox)
	})
}

func TestDeadlinesPropagateFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("expired lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, expiredErr: assert.AnError}, f.box)
		n, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, 0, n)
	})
	t.Run("unit of work fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		f.now = f.now.Add(3 * time.Minute)
		f.orch.uow = failingUoW{err: assert.AnError}
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("saga lookup inside the transaction fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		f.now = f.now.Add(3 * time.Minute)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, findErr: assert.AnError}, f.box)
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("candidate vanished is skipped", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		f.resetOutbox()
		f.now = f.now.Add(3 * time.Minute)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, hideByID: true}, f.box)
		n, err := f.orch.ExpireDeadlines(ctx, 10)
		require.NoError(t, err)
		assert.Equal(t, 0, n)
		assert.Empty(t, f.sent(t))
	})
	t.Run("budget: saga update fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		f.now = f.now.Add(3 * time.Minute)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("budget: CancelBudget cannot be stored", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		f.now = f.now.Add(3 * time.Minute)
		f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 1})
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, errOutbox)
	})
	t.Run("payment: order lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.now = f.now.Add(25 * time.Hour)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("payment: order status change fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.now = f.now.Add(25 * time.Hour)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, updateErr: assert.AnError}, f.sagas, f.box)
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("payment: saga update fails on the unexpected status branch", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		so, _ := f.sos.FindByID(ctx, soID)
		so.Status = domain.SERVICE_ORDER_STATUS_COMPLETED
		require.NoError(t, f.sos.Update(ctx, &so))
		f.now = f.now.Add(25 * time.Hour)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("payment: saga update fails after cancelling the order", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.now = f.now.Add(25 * time.Hour)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("payment: compensation command fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.now = f.now.Add(25 * time.Hour)
		// status event (1), RefundPayment (2)
		f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 2})
		_, err := f.orch.ExpireDeadlines(ctx, 10)
		assert.ErrorIs(t, err, errOutbox)
	})
}

func TestRepliesPropagateFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("budget: malformed total", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		inst := f.saga(t)
		err := f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", inst.ID, map[string]string{"total_amount": "abc"}))
		assert.Error(t, err)
	})
	t.Run("budget: malformed payload", func(t *testing.T) {
		f := newFixture(t)
		env := messaging.Envelope{Type: "BudgetGenerated", SagaID: "x", Payload: []byte(`"not-an-object"`)}
		assert.Error(t, f.orch.onBudgetGenerated(ctx, env))
	})
	t.Run("budget: order lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		inst := f.saga(t)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
		err := f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", inst.ID, map[string]string{"total_amount": "1.00"}))
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("budget: order no longer in diagnosis is ignored", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		inst := f.saga(t)
		so, _ := f.sos.FindByID(ctx, soID)
		so.Status = domain.SERVICE_ORDER_STATUS_RECEIVED
		require.NoError(t, f.sos.Update(ctx, &so))
		require.NoError(t, f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", inst.ID, map[string]string{"total_amount": "1.00"})))
		assert.Equal(t, StepBudget, f.saga(t).Step)
	})
	t.Run("budget: status change fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		inst := f.saga(t)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, updateErr: assert.AnError}, f.sagas, f.box)
		err := f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", inst.ID, map[string]string{"total_amount": "1.00"}))
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("budget: saga update fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		inst := f.saga(t)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		err := f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", inst.ID, map[string]string{"total_amount": "1.00"}))
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("payment approved: saga lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, findErr: assert.AnError}, f.box)
		err := f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", sagaID, map[string]string{}))
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("payment approved: order lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
		err := f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", sagaID, map[string]string{}))
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("payment approved: saga update fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		err := f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", sagaID, map[string]string{}))
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("payment approved: EnqueueExecution cannot be stored", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndBudget(t)
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 1})
		err := f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", sagaID, map[string]string{}))
		assert.ErrorIs(t, err, errOutbox)
	})
	t.Run("payment failed: lookups and changes fail", func(t *testing.T) {
		for name, mut := range map[string]func(f *fixture){
			"saga lookup": func(f *fixture) {
				f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, findErr: assert.AnError}, f.box)
			},
			"order lookup": func(f *fixture) {
				f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
			},
			"status change": func(f *fixture) {
				f.wire(&failingSOS{ServiceOrderRepository: f.sos, updateErr: assert.AnError}, f.sagas, f.box)
			},
			"saga update": func(f *fixture) {
				f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
			},
			"compensation": func(f *fixture) { f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 3}) },
		} {
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
				sagaID := f.startAndBudget(t)
				require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
				mut(f)
				err := f.orch.onPaymentFailed(ctx, reply(t, "PaymentFailed", sagaID, map[string]string{}))
				assert.Error(t, err)
			})
		}
	})
	t.Run("payment failed: saga not found is ignored", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		assert.NoError(t, f.orch.onPaymentFailed(ctx, reply(t, "PaymentFailed", "missing", map[string]string{})))
	})
	t.Run("execution queued: saga update fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndPay(t)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		err := f.orch.onExecutionQueued(ctx, reply(t, "ExecutionQueued", sagaID, map[string]string{}))
		assert.ErrorIs(t, err, assert.AnError)
	})
	t.Run("execution queued: wrong step is ignored", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndBudget(t)
		assert.NoError(t, f.orch.onExecutionQueued(ctx, reply(t, "ExecutionQueued", sagaID, map[string]string{})))
		assert.Equal(t, StepAwaitingCustomer, f.saga(t).Step)
	})
	t.Run("execution completed: failures and guards", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndPay(t)
		require.NoError(t, f.orch.onExecutionQueued(ctx, reply(t, "ExecutionQueued", sagaID, map[string]string{})))
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.onExecutionCompleted(ctx, reply(t, "ExecutionCompleted", sagaID, map[string]string{})), assert.AnError)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, updateErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.onExecutionCompleted(ctx, reply(t, "ExecutionCompleted", sagaID, map[string]string{})), assert.AnError)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.onExecutionCompleted(ctx, reply(t, "ExecutionCompleted", sagaID, map[string]string{})), assert.AnError)
	})
	t.Run("execution completed: order not in progress is ignored", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndPay(t)
		require.NoError(t, f.orch.onExecutionQueued(ctx, reply(t, "ExecutionQueued", sagaID, map[string]string{})))
		so, _ := f.sos.FindByID(ctx, soID)
		so.Status = domain.SERVICE_ORDER_STATUS_CANCELLED
		require.NoError(t, f.sos.Update(ctx, &so))
		require.NoError(t, f.orch.onExecutionCompleted(ctx, reply(t, "ExecutionCompleted", sagaID, map[string]string{})))
		assert.Equal(t, StepExecution, f.saga(t).Step)
	})
	t.Run("execution failed: failures and guards", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndPay(t)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, findErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.onExecutionFailed(ctx, reply(t, "ExecutionFailed", sagaID, map[string]string{})), assert.AnError)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, findErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.onExecutionFailed(ctx, reply(t, "ExecutionFailed", sagaID, map[string]string{})), assert.AnError)
		f.wire(&failingSOS{ServiceOrderRepository: f.sos, updateErr: assert.AnError}, f.sagas, f.box)
		assert.ErrorIs(t, f.orch.onExecutionFailed(ctx, reply(t, "ExecutionFailed", sagaID, map[string]string{})), assert.AnError)
		f.wire(f.sos, &failingRepo{MemoryRepository: f.sagas, updateErr: assert.AnError}, f.box)
		assert.ErrorIs(t, f.orch.onExecutionFailed(ctx, reply(t, "ExecutionFailed", sagaID, map[string]string{})), assert.AnError)
	})
	t.Run("execution failed: order not in progress is ignored", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndPay(t)
		so, _ := f.sos.FindByID(ctx, soID)
		so.Status = domain.SERVICE_ORDER_STATUS_CANCELLED
		require.NoError(t, f.sos.Update(ctx, &so))
		require.NoError(t, f.orch.onExecutionFailed(ctx, reply(t, "ExecutionFailed", sagaID, map[string]string{})))
		assert.Equal(t, StepExecutionQueue, f.saga(t).Step)
	})
	t.Run("execution failed: compensation command fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		sagaID := f.startAndPay(t)
		f.wire(f.sos, f.sagas, &failingOutbox{MemoryOutbox: f.box, failAt: 2})
		assert.ErrorIs(t, f.orch.onExecutionFailed(ctx, reply(t, "ExecutionFailed", sagaID, map[string]string{})), errOutbox)
	})
}
