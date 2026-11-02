package saga

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/metrics"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// stepSamples returns how many durations were observed for step in saga_step_duration_seconds.
func stepSamples(t *testing.T, step string) uint64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range families {
		if mf.GetName() != "saga_step_duration_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "step" && lp.GetValue() == step {
					return m.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	return 0
}

// backdate sets the update time of the running saga, as if its command had been stored that long ago.
func (f *fixture) backdate(t *testing.T, sagaID string, d time.Duration) {
	t.Helper()
	ctx := context.Background()
	inst, err := f.sagas.FindByID(ctx, sagaID)
	require.NoError(t, err)
	require.NotNil(t, inst)
	inst.UpdatedAt = f.now.Add(-d)
	require.NoError(t, f.sagas.Update(ctx, inst))
}

func observedContext() (context.Context, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return logger.WithZap(context.Background(), zap.New(core)), logs
}

func TestSagaMetrics_HappyPathCountsStartAndCompletion(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()

	started := testutil.ToFloat64(metrics.SagaStartedTotal)
	completed := testutil.ToFloat64(metrics.SagaCompletedTotal)
	budgetBefore, paymentBefore, queueBefore := stepSamples(t, "budget"), stepSamples(t, "payment"), stepSamples(t, "execution_queue")

	require.NoError(t, f.orch.Send(ctx, soID, "req-1"))
	inst := f.saga(t)
	require.NotNil(t, inst)
	assert.Equal(t, started+1, testutil.ToFloat64(metrics.SagaStartedTotal))

	f.backdate(t, inst.ID, 3*time.Second)
	require.NoError(t, f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", inst.ID,
		map[string]string{"budget_id": "b1", "total_amount": "221.00"})))
	require.NoError(t, f.orch.Accept(ctx, soID, userID, "req-1"))

	f.backdate(t, inst.ID, 5*time.Second)
	require.NoError(t, f.orch.onPaymentApproved(ctx, reply(t, "PaymentApproved", inst.ID,
		map[string]string{"payment_id": "p1", "amount": "221.00"})))

	f.backdate(t, inst.ID, 2*time.Second)
	require.NoError(t, f.orch.onExecutionQueued(ctx, reply(t, "ExecutionQueued", inst.ID, map[string]any{})))
	require.NoError(t, f.orch.onExecutionCompleted(ctx, reply(t, "ExecutionCompleted", inst.ID, map[string]any{})))

	assert.Equal(t, completed+1, testutil.ToFloat64(metrics.SagaCompletedTotal))
	assert.Equal(t, budgetBefore+1, stepSamples(t, "budget"))
	assert.Equal(t, paymentBefore+1, stepSamples(t, "payment"))
	assert.Equal(t, queueBefore+1, stepSamples(t, "execution_queue"))
}

func TestSagaMetrics_RejectCountsCompensation(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	compensated := testutil.ToFloat64(metrics.SagaCompensatedTotal)

	require.NoError(t, f.orch.Send(ctx, soID, "req-1"))
	inst := f.saga(t)
	require.NoError(t, f.orch.onBudgetGenerated(ctx, reply(t, "BudgetGenerated", inst.ID,
		map[string]string{"budget_id": "b1", "total_amount": "221.00"})))
	require.NoError(t, f.orch.Reject(ctx, soID, userID, "req-1"))

	assert.Equal(t, compensated+1, testutil.ToFloat64(metrics.SagaCompensatedTotal))
}

func TestSagaMetrics_UserCancelCountsCompensation(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	compensated := testutil.ToFloat64(metrics.SagaCompensatedTotal)

	require.NoError(t, f.orch.Send(ctx, soID, "req-1"))
	require.NoError(t, f.orch.Cancel(ctx, soID, "req-2"))

	assert.Equal(t, compensated+1, testutil.ToFloat64(metrics.SagaCompensatedTotal))
}

func TestSagaMetrics_DeadlineCompensationIsCounted(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx := context.Background()
	sagaID := f.startAndPay(t)
	compensated := testutil.ToFloat64(metrics.SagaCompensatedTotal)

	ctx, logs := observedWith(ctx)
	f.now = f.now.Add(10 * time.Minute)
	n, err := f.orch.ExpireDeadlines(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	assert.Equal(t, compensated+1, testutil.ToFloat64(metrics.SagaCompensatedTotal))
	assertSagaLines(t, logs, sagaID)
	assert.True(t, hasMessage(logs, "saga.deadline_expired"))
}

// deliver hands a reply to the handler the way the consumer does: through the reply wrapper that
// puts the envelope ids in the logger context.
func (f *fixture) deliver(ctx context.Context, fn replyFunc, env messaging.Envelope) error {
	return f.orch.reply(fn)(ctx, nil, env)
}

// observedWith attaches a capturing logger to ctx, keeping any logger already there.
func observedWith(ctx context.Context) (context.Context, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return logger.WithZap(ctx, zap.New(core)), logs
}

// TestSagaLogs_EveryOrchestratorLineCarriesSagaID runs the whole happy path and checks that each
// line of the saga and of the status transitions has the saga_id of the order.
func TestSagaLogs_EveryOrchestratorLineCarriesSagaID(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx, logs := observedContext()

	require.NoError(t, f.orch.Send(ctx, soID, "req-1"))
	inst := f.saga(t)
	require.NotNil(t, inst)
	require.NoError(t, f.deliver(ctx, f.orch.onBudgetGenerated, reply(t, "BudgetGenerated", inst.ID,
		map[string]string{"budget_id": "b1", "total_amount": "221.00"})))
	require.NoError(t, f.orch.Accept(ctx, soID, userID, "req-1"))
	require.NoError(t, f.deliver(ctx, f.orch.onPaymentApproved, reply(t, "PaymentApproved", inst.ID,
		map[string]string{"payment_id": "p1", "amount": "221.00"})))
	require.NoError(t, f.deliver(ctx, f.orch.onExecutionQueued, reply(t, "ExecutionQueued", inst.ID, map[string]any{})))
	require.NoError(t, f.deliver(ctx, f.orch.onExecutionCompleted, reply(t, "ExecutionCompleted", inst.ID, map[string]any{})))

	assertSagaLines(t, logs, inst.ID)
	assert.True(t, hasMessage(logs, "saga.started"))
	assert.True(t, hasMessage(logs, "saga.completed"))
	assert.True(t, hasMessage(logs, "service_order.status_transition"))
}

func TestSagaLogs_ReplyFromAnotherSagaIsIgnoredWithoutLeakingIDs(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	ctx, logs := observedContext()
	require.NoError(t, f.orch.Send(ctx, soID, "req-1"))

	require.NoError(t, f.deliver(ctx, f.orch.onBudgetGenerated, reply(t, "BudgetGenerated", "unknown-saga",
		map[string]string{"total_amount": "1.00"})))

	require.Len(t, logs.All(), 2)
	ignored := logs.All()[1]
	assert.Equal(t, "saga.reply_ignored", ignored.Message)
	assert.Equal(t, 1, countKey(ignored, "saga_id"), "saga_id must appear once per line")
}

func assertSagaLines(t *testing.T, logs *observer.ObservedLogs, sagaID string) {
	t.Helper()
	checked := 0
	for _, e := range logs.All() {
		if !strings.HasPrefix(e.Message, "saga.") && e.Message != "service_order.status_transition" {
			continue
		}
		checked++
		assert.Equal(t, sagaID, e.ContextMap()["saga_id"], e.Message)
		assert.Equal(t, 1, countKey(e, "saga_id"), e.Message)
	}
	assert.NotZero(t, checked)
}

func hasMessage(logs *observer.ObservedLogs, msg string) bool {
	return logs.FilterMessage(msg).Len() > 0
}

func countKey(e observer.LoggedEntry, key string) int {
	n := 0
	for _, f := range e.Context {
		if f.Key == key {
			n++
		}
	}
	return n
}
