package saga

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/repository"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lastStatusEvent(t *testing.T, box *MemoryOutbox) statusChangedPayload {
	t.Helper()
	require.NotEmpty(t, box.Sent)
	env := box.Sent[len(box.Sent)-1]
	assert.Equal(t, EvtServiceOrderStatusChanged, env.Env.Type)
	assert.Equal(t, TopicSOEvents, env.Topic)
	var p statusChangedPayload
	require.NoError(t, json.Unmarshal(env.Env.Payload, &p))
	return p
}

func TestCreatedPublishesNewStatus(t *testing.T) {
	ctx := context.Background()
	box := NewMemoryOutbox()
	sos := repository.NewInMemory()
	c := NewStatusChanger(sos, box, &countingNotifier{})
	so := &domain.ServiceOrder{ID: soID, Status: domain.SERVICE_ORDER_STATUS_NEW, TotalAmount: decimal.Zero}

	require.NoError(t, c.Created(ctx, so, Meta{}))
	p := lastStatusEvent(t, box)
	assert.Nil(t, p.Previous, "the creation has no previous status")
	assert.Equal(t, "NEW", p.New)
	assert.Nil(t, p.Reason)
	assert.Equal(t, "0.00", p.TotalAmount)
	assert.NotEmpty(t, box.Sent[0].Env.SagaID, "a saga id is generated when the meta has none")
}

func TestCreatedFailsWhenOutboxFails(t *testing.T) {
	c := NewStatusChanger(repository.NewInMemory(), &failingOutbox{MemoryOutbox: NewMemoryOutbox(), failAt: 1}, &countingNotifier{})
	err := c.Created(context.Background(), &domain.ServiceOrder{ID: soID}, Meta{})
	assert.ErrorIs(t, err, errOutbox)
}

func TestChangeStoresTimestampsAndReason(t *testing.T) {
	ctx := context.Background()
	box := NewMemoryOutbox()
	sos := repository.NewInMemory()
	notifier := &countingNotifier{}
	c := NewStatusChanger(sos, box, notifier)
	c.now = func() time.Time { return time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC) }

	so := domain.ServiceOrder{ID: soID, Status: domain.SERVICE_ORDER_STATUS_IN_PROGRESS, TotalAmount: decimal.RequireFromString("10")}
	require.NoError(t, sos.Insert(ctx, &so))
	require.NoError(t, c.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_CANCELLED, domain.ReasonUserCancelled, Meta{SagaID: "saga-1", RequestID: "req-9"}))
	p := lastStatusEvent(t, box)
	require.NotNil(t, p.Previous)
	assert.Equal(t, "IN_PROGRESS", *p.Previous)
	assert.Equal(t, "CANCELLED", p.New)
	require.NotNil(t, p.Reason)
	assert.Equal(t, domain.ReasonUserCancelled, *p.Reason)
	assert.Equal(t, "saga-1", box.Sent[0].Env.SagaID)
	assert.Equal(t, "req-9", box.Sent[0].Env.RequestID)
	assert.Equal(t, 1, notifier.calls)
}

func TestChangeSetsCompletionAndDeliveryDates(t *testing.T) {
	ctx := context.Background()
	box := NewMemoryOutbox()
	sos := repository.NewInMemory()
	c := NewStatusChanger(sos, box, &countingNotifier{})
	now := time.Date(2026, 11, 2, 15, 30, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	so := domain.ServiceOrder{ID: soID, Status: domain.SERVICE_ORDER_STATUS_IN_PROGRESS}
	require.NoError(t, sos.Insert(ctx, &so))
	require.NoError(t, c.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_COMPLETED, "", Meta{}))
	require.NotNil(t, so.CompletedAt)
	assert.True(t, so.CompletedAt.Equal(now))
	assert.Nil(t, so.DeliveredAt)

	require.NoError(t, c.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_DELIVERED, "", Meta{}))
	require.NotNil(t, so.DeliveredAt)
	assert.True(t, so.DeliveredAt.Equal(now))
	p := lastStatusEvent(t, box)
	assert.Nil(t, p.Reason, "no reason is sent when the change has none")
}

func TestChangeRejectsIllegalEdgeWithoutStoring(t *testing.T) {
	box := NewMemoryOutbox()
	c := NewStatusChanger(repository.NewInMemory(), box, &countingNotifier{})
	so := domain.ServiceOrder{ID: soID, Status: domain.SERVICE_ORDER_STATUS_NEW}
	err := c.Change(context.Background(), &so, domain.SERVICE_ORDER_STATUS_DELIVERED, "", Meta{})
	assert.ErrorIs(t, err, domain.ErrServiceOrderInvalidTransition)
	assert.Empty(t, box.Sent)
}

func TestChangeFailsWhenStoreOrOutboxFails(t *testing.T) {
	ctx := context.Background()
	t.Run("store fails", func(t *testing.T) {
		c := NewStatusChanger(&failingSOS{ServiceOrderRepository: repository.NewInMemory(), updateErr: errors.New("db down")},
			NewMemoryOutbox(), &countingNotifier{})
		so := domain.ServiceOrder{ID: soID, Status: domain.SERVICE_ORDER_STATUS_NEW}
		assert.Error(t, c.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_RECEIVED, "", Meta{}))
		assert.Equal(t, domain.SERVICE_ORDER_STATUS_RECEIVED, so.Status, "in-memory status is moved before the store fails")
	})
	t.Run("outbox fails", func(t *testing.T) {
		sos := repository.NewInMemory()
		c := NewStatusChanger(sos, &failingOutbox{MemoryOutbox: NewMemoryOutbox(), failAt: 1}, &countingNotifier{})
		so := domain.ServiceOrder{ID: soID, Status: domain.SERVICE_ORDER_STATUS_NEW}
		require.NoError(t, sos.Insert(ctx, &so))
		assert.ErrorIs(t, c.Change(ctx, &so, domain.SERVICE_ORDER_STATUS_RECEIVED, "", Meta{}), errOutbox)
	})
}

func TestMemoryRepositoryFindExpiredHonoursLimit(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b", "c"} {
		d := past
		require.NoError(t, repo.Insert(ctx, &Instance{ID: id, Status: StatusRunning, Deadline: &d}))
	}
	all, err := repo.FindExpired(ctx, now, 0)
	require.NoError(t, err)
	assert.Len(t, all, 3, "limit 0 means no limit")

	two, err := repo.FindExpired(ctx, now, 2)
	require.NoError(t, err)
	require.Len(t, two, 2)
	assert.Equal(t, "a", two[0].ID, "results are ordered by id")

	missing, err := repo.FindByID(ctx, "zzz")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
