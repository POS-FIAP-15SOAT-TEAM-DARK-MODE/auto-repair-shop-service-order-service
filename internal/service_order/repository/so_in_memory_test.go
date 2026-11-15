package repository

import (
	"context"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryOrdersLifecycle(t *testing.T) {
	ctx := context.Background()
	r := NewInMemory()
	so := &domain.ServiceOrder{ID: "so-1", Status: domain.SERVICE_ORDER_STATUS_NEW,
		Customer: domain.CustomerSnapshot{ID: "c1"}, Vehicle: domain.VehicleSnapshot{ID: "v1"}}
	require.NoError(t, r.Insert(ctx, so))

	got, err := r.FindByID(ctx, "so-1")
	require.NoError(t, err)
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_NEW, got.Status)

	_, err = r.FindByID(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrServiceOrderNotFound)
	_, err = r.FindStatus(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrServiceOrderNotFound)

	st, err := r.FindStatus(ctx, "so-1")
	require.NoError(t, err)
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_NEW, st)

	so.Status = domain.SERVICE_ORDER_STATUS_RECEIVED
	require.NoError(t, r.Update(ctx, so))
	st, _ = r.FindStatus(ctx, "so-1")
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_RECEIVED, st)
	assert.ErrorIs(t, r.Update(ctx, &domain.ServiceOrder{ID: "missing"}), domain.ErrServiceOrderNotFound)

	require.NoError(t, r.UpdateTotal(ctx, "so-1", decimal.NewFromInt(9)))
	got, _ = r.FindByID(ctx, "so-1")
	assert.Equal(t, "9", got.TotalAmount.String())
	assert.ErrorIs(t, r.UpdateTotal(ctx, "missing", decimal.Zero), domain.ErrServiceOrderNotFound)

	ok, err := r.Exists(ctx, "c1", "")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, _ = r.Exists(ctx, "", "v1")
	assert.True(t, ok)
	ok, _ = r.Exists(ctx, "x", "y")
	assert.False(t, ok)
}

func TestInMemoryWorksAndSupplies(t *testing.T) {
	ctx := context.Background()
	r := NewInMemory()
	w := domain.WorkItem{ID: "w1", UnitPrice: decimal.NewFromInt(1)}
	require.NoError(t, r.AddWork(ctx, "so-1", w))
	require.NoError(t, r.AddWork(ctx, "so-1", w), "adding the same work twice is a no-op")
	works, err := r.ListWorks(ctx, "so-1")
	require.NoError(t, err)
	assert.Len(t, works, 1)

	require.NoError(t, r.RemoveWork(ctx, "so-1", "w1"))
	assert.ErrorIs(t, r.RemoveWork(ctx, "so-1", "w1"), domain.ErrServiceOrderWorkNotFound)

	s := domain.SupplyItem{ID: "s1", ReservationID: "r1", Quantity: 2}
	require.NoError(t, r.AddSupply(ctx, "so-1", s))
	supplies, err := r.ListSupplies(ctx, "so-1")
	require.NoError(t, err)
	require.Len(t, supplies, 1)

	removed, err := r.RemoveSupply(ctx, "so-1", "s1")
	require.NoError(t, err)
	assert.Equal(t, "r1", removed.ReservationID)
	_, err = r.RemoveSupply(ctx, "so-1", "s1")
	assert.ErrorIs(t, err, domain.ErrServiceOrderSupplyNotFound)
}

func TestInMemoryListingFiltersAndPages(t *testing.T) {
	ctx := context.Background()
	r := NewInMemory()
	for _, o := range []domain.ServiceOrder{
		{ID: "a", Status: domain.SERVICE_ORDER_STATUS_NEW, Customer: domain.CustomerSnapshot{ID: "c1"}, Vehicle: domain.VehicleSnapshot{ID: "v1"}},
		{ID: "b", Status: domain.SERVICE_ORDER_STATUS_DELIVERED, Customer: domain.CustomerSnapshot{ID: "c1"}, Vehicle: domain.VehicleSnapshot{ID: "v2"}},
		{ID: "c", Status: domain.SERVICE_ORDER_STATUS_IN_PROGRESS, Customer: domain.CustomerSnapshot{ID: "c2"}, Vehicle: domain.VehicleSnapshot{ID: "v3"}},
	} {
		o := o
		require.NoError(t, r.Insert(ctx, &o))
	}

	n, err := r.Count(ctx, &domain.ServiceOrderFilterParams{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "terminal orders are excluded by default")

	n, _ = r.Count(ctx, &domain.ServiceOrderFilterParams{Status: "DELIVERED"})
	assert.Equal(t, int64(1), n)
	n, _ = r.Count(ctx, &domain.ServiceOrderFilterParams{CustomerID: "c1"})
	assert.Equal(t, int64(1), n)
	n, _ = r.Count(ctx, &domain.ServiceOrderFilterParams{VehicleID: "v3"})
	assert.Equal(t, int64(1), n)

	items, err := r.Search(ctx, &domain.ServiceOrderFilterParams{Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "c", items[0].ID)

	past, err := r.Search(ctx, &domain.ServiceOrderFilterParams{Limit: 1, Offset: 9})
	require.NoError(t, err)
	assert.Empty(t, past)

	clamped, err := r.Search(ctx, &domain.ServiceOrderFilterParams{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, clamped, 2)

	unlimited, err := r.Search(ctx, &domain.ServiceOrderFilterParams{})
	require.NoError(t, err)
	assert.Len(t, unlimited, 2)
}
