package saga

import (
	"context"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/metrics"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// StatusChanger is the only place where the status of a service order changes. Each change
// stores the order, writes ServiceOrderStatusChanged to the outbox and, after commit, updates
// the transition metric and calls the customer notifier (best effort).
type StatusChanger struct {
	sos      domain.ServiceOrderRepository
	outbox   Outbox
	notifier domain.StatusNotifier
	now      func() time.Time
}

func NewStatusChanger(sos domain.ServiceOrderRepository, outbox Outbox, notifier domain.StatusNotifier) *StatusChanger {
	return &StatusChanger{sos: sos, outbox: outbox, notifier: notifier, now: time.Now}
}

type statusChangedPayload struct {
	Previous    *string `json:"previous"`
	New         string  `json:"new"`
	Reason      *string `json:"reason"`
	TotalAmount string  `json:"total_amount"`
}

// Created publishes the NEW status of an order that has just been inserted.
func (c *StatusChanger) Created(ctx context.Context, so *domain.ServiceOrder, m Meta) error {
	return c.publish(ctx, so, nil, domain.SERVICE_ORDER_STATUS_NEW, "", m)
}

// Change moves the order to next (validated by the status graph), stores it and publishes the
// event. reason is "" when not applicable.
func (c *StatusChanger) Change(ctx context.Context, so *domain.ServiceOrder, next domain.SERVICE_ORDER_STATUS, reason string, m Meta) error {
	prev, err := so.Transition(next)
	if err != nil {
		return err
	}
	now := c.now()
	switch next {
	case domain.SERVICE_ORDER_STATUS_COMPLETED:
		so.CompletedAt = &now
	case domain.SERVICE_ORDER_STATUS_DELIVERED:
		so.DeliveredAt = &now
	}
	if err := c.sos.Update(ctx, so); err != nil {
		return err
	}
	return c.publish(ctx, so, &prev, next, reason, m)
}

func (c *StatusChanger) publish(ctx context.Context, so *domain.ServiceOrder, prev *domain.SERVICE_ORDER_STATUS, next domain.SERVICE_ORDER_STATUS, reason string, m Meta) error {
	if m.SagaID == "" {
		m.SagaID = uuid.NewString()
	}
	payload := statusChangedPayload{New: next.String(), TotalAmount: so.TotalAmount.StringFixed(2)}
	if prev != nil {
		p := prev.String()
		payload.Previous = &p
	}
	if reason != "" {
		payload.Reason = &reason
	}

	env, err := messaging.NewEnvelope(EvtServiceOrderStatusChanged, m.SagaID, so.ID, m.RequestID, payload)
	if err != nil {
		return err
	}
	if err := c.outbox.Enqueue(ctx, TopicSOEvents, "", env); err != nil {
		return err
	}

	logger.Of(ctx).Info("service_order.status_transition",
		zap.String("entity", "service_order"),
		zap.String("service_order_id", so.ID),
		zap.String("new_status", next.String()),
	)

	msg := domain.StatusNotification{
		ServiceOrderID: so.ID,
		CustomerName:   so.Customer.Name,
		CustomerEmail:  so.Customer.Email,
		PreviousStatus: prev,
		NewStatus:      next,
	}
	uow.OnCommit(ctx, func(commitCtx context.Context) {
		metrics.ServiceOrderStatusTransitionsTotal.WithLabelValues(next.String()).Inc()
		if err := c.notifier.NotifyStatusChange(commitCtx, msg); err != nil {
			metrics.ServiceOrderNotificationFailuresTotal.Inc()
			logger.Of(commitCtx).Warn("service_order.status_notification_failed",
				zap.String("service_order_id", msg.ServiceOrderID),
				zap.String("new_status", next.String()),
				zap.Error(err),
			)
		}
	})
	return nil
}

// money formats a monetary value as the contract requires: a decimal string with two places.
func money(d decimal.Decimal) string {
	return d.StringFixed(2)
}
