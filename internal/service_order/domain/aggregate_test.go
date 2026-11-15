package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aggregateRepo is a ServiceOrderRepository whose reads can be made to fail.
type aggregateRepo struct {
	ServiceOrderRepository
	header                ServiceOrder
	worksErr, suppliesErr error
	findErr               error
	works                 []WorkItem
	supplies              []SupplyItem
}

func (r *aggregateRepo) FindByID(context.Context, string) (ServiceOrder, error) {
	return r.header, r.findErr
}

func (r *aggregateRepo) ListWorks(context.Context, string) ([]WorkItem, error) {
	return r.works, r.worksErr
}

func (r *aggregateRepo) ListSupplies(context.Context, string) ([]SupplyItem, error) {
	return r.supplies, r.suppliesErr
}

func TestLoadAggregateReadsHeaderWorksAndSupplies(t *testing.T) {
	repo := &aggregateRepo{
		header:   ServiceOrder{ID: "so-1", Status: SERVICE_ORDER_STATUS_IN_DIAGNOSIS},
		works:    []WorkItem{{ID: "w1"}},
		supplies: []SupplyItem{{ID: "s1", Quantity: 2}},
	}
	so, err := LoadAggregate(context.Background(), repo, "so-1")
	require.NoError(t, err)
	assert.Equal(t, "so-1", so.ID)
	assert.Equal(t, []string{"w1"}, so.WorkIDs())
	require.Len(t, so.Supplies, 1)
	assert.Equal(t, 2, so.Supplies[0].Quantity)
}

func TestLoadAggregateStopsAtFirstFailure(t *testing.T) {
	boom := errors.New("boom")
	ctx := context.Background()
	_, err := LoadAggregate(ctx, &aggregateRepo{findErr: boom}, "so-1")
	assert.ErrorIs(t, err, boom)
	_, err = LoadAggregate(ctx, &aggregateRepo{worksErr: boom}, "so-1")
	assert.ErrorIs(t, err, boom)
	_, err = LoadAggregate(ctx, &aggregateRepo{suppliesErr: boom}, "so-1")
	assert.ErrorIs(t, err, boom)
}

func TestStringToServiceOrderStatus(t *testing.T) {
	assert.Equal(t, SERVICE_ORDER_STATUS_IN_PROGRESS, StringToServiceOrderStatus("IN_PROGRESS"))
	assert.Equal(t, "CANCELLED", SERVICE_ORDER_STATUS_CANCELLED.String())
	assert.Equal(t, decimal.Zero, NewServiceOrder(CustomerSnapshot{}, VehicleSnapshot{}).TotalAmount)
}
