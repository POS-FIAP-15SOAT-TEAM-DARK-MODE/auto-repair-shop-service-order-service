package domain

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusCancelable(t *testing.T) {
	cancelable := []SERVICE_ORDER_STATUS{SERVICE_ORDER_STATUS_NEW, SERVICE_ORDER_STATUS_RECEIVED,
		SERVICE_ORDER_STATUS_IN_DIAGNOSIS, SERVICE_ORDER_STATUS_AWAITING_APPROVAL, SERVICE_ORDER_STATUS_IN_PROGRESS}
	for _, s := range cancelable {
		assert.True(t, s.IsCancelable(), s.String())
	}
	for _, s := range []SERVICE_ORDER_STATUS{SERVICE_ORDER_STATUS_COMPLETED, SERVICE_ORDER_STATUS_DELIVERED,
		SERVICE_ORDER_STATUS_REJECTED, SERVICE_ORDER_STATUS_CANCELLED} {
		assert.False(t, s.IsCancelable(), s.String())
	}
}

func TestTransitionGraph(t *testing.T) {
	legal := map[SERVICE_ORDER_STATUS][]SERVICE_ORDER_STATUS{
		SERVICE_ORDER_STATUS_NEW:               {SERVICE_ORDER_STATUS_RECEIVED, SERVICE_ORDER_STATUS_CANCELLED},
		SERVICE_ORDER_STATUS_RECEIVED:          {SERVICE_ORDER_STATUS_IN_DIAGNOSIS, SERVICE_ORDER_STATUS_CANCELLED},
		SERVICE_ORDER_STATUS_IN_DIAGNOSIS:      {SERVICE_ORDER_STATUS_AWAITING_APPROVAL, SERVICE_ORDER_STATUS_CANCELLED},
		SERVICE_ORDER_STATUS_AWAITING_APPROVAL: {SERVICE_ORDER_STATUS_IN_PROGRESS, SERVICE_ORDER_STATUS_REJECTED, SERVICE_ORDER_STATUS_CANCELLED},
		SERVICE_ORDER_STATUS_IN_PROGRESS:       {SERVICE_ORDER_STATUS_COMPLETED, SERVICE_ORDER_STATUS_REJECTED, SERVICE_ORDER_STATUS_CANCELLED},
		SERVICE_ORDER_STATUS_COMPLETED:         {SERVICE_ORDER_STATUS_DELIVERED},
	}
	all := []SERVICE_ORDER_STATUS{SERVICE_ORDER_STATUS_NEW, SERVICE_ORDER_STATUS_RECEIVED, SERVICE_ORDER_STATUS_IN_DIAGNOSIS,
		SERVICE_ORDER_STATUS_AWAITING_APPROVAL, SERVICE_ORDER_STATUS_IN_PROGRESS, SERVICE_ORDER_STATUS_COMPLETED,
		SERVICE_ORDER_STATUS_DELIVERED, SERVICE_ORDER_STATUS_REJECTED, SERVICE_ORDER_STATUS_CANCELLED}
	for _, from := range all {
		for _, to := range all {
			want := false
			for _, l := range legal[from] {
				want = want || l == to
			}
			assert.Equal(t, want, from.CanTransitionTo(to), "%s -> %s", from, to)
		}
	}
}

func TestTransitionUpdatesStatusOrFails(t *testing.T) {
	so := NewServiceOrder(CustomerSnapshot{}, VehicleSnapshot{})
	assert.NotEmpty(t, so.ID)
	prev, err := so.Transition(SERVICE_ORDER_STATUS_RECEIVED)
	require.NoError(t, err)
	assert.Equal(t, SERVICE_ORDER_STATUS_NEW, prev)
	assert.Equal(t, SERVICE_ORDER_STATUS_RECEIVED, so.Status)

	prev, err = so.Transition(SERVICE_ORDER_STATUS_DELIVERED)
	assert.ErrorIs(t, err, ErrServiceOrderInvalidTransition)
	assert.Equal(t, SERVICE_ORDER_STATUS_RECEIVED, prev)
	assert.Equal(t, SERVICE_ORDER_STATUS_RECEIVED, so.Status)
}

func TestPricingRule(t *testing.T) {
	works := []WorkItem{{UnitPrice: decimal.RequireFromString("150.00")}, {UnitPrice: decimal.RequireFromString("80")}}
	supplies := []SupplyItem{{UnitPrice: decimal.RequireFromString("35.50"), Quantity: 2}}
	assert.Equal(t, "301.00", ComputeTotal(works, supplies).StringFixed(2))
	assert.Equal(t, "0", ComputeTotal(nil, nil).String())

	so := &ServiceOrder{Works: works, Supplies: supplies}
	so.RecalculateTotal()
	assert.Equal(t, "301.00", so.TotalAmount.StringFixed(2))
}

func TestWorkIDs(t *testing.T) {
	so := &ServiceOrder{Works: []WorkItem{{ID: "w1"}, {ID: "w2"}}}
	assert.Equal(t, []string{"w1", "w2"}, so.WorkIDs())
}
