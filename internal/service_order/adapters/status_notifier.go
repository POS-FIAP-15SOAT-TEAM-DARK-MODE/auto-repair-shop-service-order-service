package adapters

import (
	"context"
	"strings"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"go.uber.org/zap"
)

type logStatusNotifier struct{}

func NewLogStatusNotifier() domain.StatusNotifier {
	return &logStatusNotifier{}
}

func (n *logStatusNotifier) NotifyStatusChange(ctx context.Context, msg domain.StatusNotification) error {
	previous := "-"
	if msg.PreviousStatus != nil {
		previous = msg.PreviousStatus.String()
	}

	logger.Of(ctx).Info("service_order.status_notification_sent",
		zap.String("channel", "email"),
		zap.String("entity", "service_order"),
		zap.String("service_order_id", msg.ServiceOrderID),
		zap.String("recipient", maskEmail(msg.CustomerEmail)),
		zap.String("previous_status", previous),
		zap.String("new_status", msg.NewStatus.String()),
		zap.String("subject", statusEmailSubject(msg.NewStatus)),
	)
	return nil
}

func statusEmailSubject(status domain.SERVICE_ORDER_STATUS) string {
	return "Your service order is now: " + status.String()
}

// maskEmail hides the local part of an address so PII never reaches the logs
// while still making it clear which recipient would have been notified.
func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "unknown"
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	return "***" + email[at:]
}
