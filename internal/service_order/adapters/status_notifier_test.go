package adapters

import (
	"context"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogStatusNotifier_NotifyStatusChange(t *testing.T) {
	notifier := NewLogStatusNotifier()

	prev := domain.SERVICE_ORDER_STATUS_NEW
	err := notifier.NotifyStatusChange(context.Background(), domain.StatusNotification{
		ServiceOrderID: "so-1",
		CustomerName:   "Ana",
		CustomerEmail:  "ana@example.com",
		PreviousStatus: &prev,
		NewStatus:      domain.SERVICE_ORDER_STATUS_RECEIVED,
	})

	require.NoError(t, err)
}

func TestLogStatusNotifier_NotifyStatusChange_NoPreviousStatus(t *testing.T) {
	notifier := NewLogStatusNotifier()

	err := notifier.NotifyStatusChange(context.Background(), domain.StatusNotification{
		ServiceOrderID: "so-2",
		NewStatus:      domain.SERVICE_ORDER_STATUS_NEW,
	})

	require.NoError(t, err)
}

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  string
	}{
		{"regular", "ana.motorista@example.com", "***@example.com"},
		{"trims spaces", "  bob@shop.io  ", "***@shop.io"},
		{"empty", "", "unknown"},
		{"no at sign", "not-an-email", "***"},
		{"at first position", "@example.com", "***"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, maskEmail(tc.email))
		})
	}
}

func TestStatusEmailSubject(t *testing.T) {
	assert.Equal(t,
		"Your service order is now: RECEIVED",
		statusEmailSubject(domain.SERVICE_ORDER_STATUS_RECEIVED),
	)
}
