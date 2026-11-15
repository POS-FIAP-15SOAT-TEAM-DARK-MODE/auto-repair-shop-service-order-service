package saga

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingQueue struct {
	deleted []string
}

func (q *recordingQueue) ReceiveMessage(context.Context, string, int32, int32) ([]messaging.Message, error) {
	return nil, nil
}

func (q *recordingQueue) DeleteMessage(_ context.Context, _, receipt string) error {
	q.deleted = append(q.deleted, receipt)
	return nil
}

// deliver pushes one reply through the real consumer: dedupe insert, handler and commit.
func deliver(t *testing.T, c *messaging.Consumer, mock sqlmock.Sqlmock, env messaging.Envelope) {
	t.Helper()
	body, err := json.Marshal(env)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WithArgs(env.MessageID).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	require.NoError(t, c.Handle(context.Background(), messaging.Message{Body: string(body), ReceiptHandle: env.MessageID}))
}

func newReplyConsumer(t *testing.T, f *fixture) (*messaging.Consumer, sqlmock.Sqlmock, *recordingQueue) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := &recordingQueue{}
	c := messaging.NewConsumer(db, q, "http://queue/so-saga-replies")
	f.orch.RegisterReplies(c)
	return c, mock, q
}

func TestRepliesThroughConsumerDriveTheSaga(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	c, mock, q := newReplyConsumer(t, f)

	require.NoError(t, f.orch.Send(ctx, soID, "req-1"))
	inst := f.saga(t)
	require.NotNil(t, inst)

	env := func(typ string, sagaID string, payload any) messaging.Envelope {
		e, err := messaging.NewEnvelope(typ, sagaID, soID, "req-1", payload)
		require.NoError(t, err)
		return e
	}

	// a reply for a step that is not current is handled (deleted) without changing the saga
	deliver(t, c, mock, env("PaymentApproved", inst.ID, map[string]string{}))
	assert.Equal(t, StepBudget, f.saga(t).Step)

	deliver(t, c, mock, env("BudgetGenerated", inst.ID, map[string]string{"total_amount": "221.00"}))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL, f.status(t))

	require.NoError(t, f.orch.Accept(ctx, soID, userID, "req-1"))
	deliver(t, c, mock, env("PaymentApproved", inst.ID, map[string]string{}))
	assert.Equal(t, StepExecutionQueue, f.saga(t).Step)

	deliver(t, c, mock, env("ExecutionQueued", inst.ID, map[string]string{}))
	assert.Equal(t, StepExecution, f.saga(t).Step)

	deliver(t, c, mock, env("ExecutionCompleted", inst.ID, map[string]string{}))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_COMPLETED, f.status(t))
	assert.Equal(t, StatusCompleted, f.sagas.items[inst.ID].Status)

	// informational replies are logged and discarded
	for _, typ := range []string{"BudgetCancelled", "PaymentRefunded", "ExecutionWorkStatusChanged"} {
		deliver(t, c, mock, env(typ, inst.ID, map[string]string{}))
	}
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_COMPLETED, f.status(t))
	assert.Len(t, q.deleted, 8, "every handled message is deleted from the queue")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepliesThroughConsumerRoutePaymentFailureAndExecutionFailure(t *testing.T) {
	ctx := context.Background()

	t.Run("payment failed compensates", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		c, mock, _ := newReplyConsumer(t, f)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		inst := f.saga(t)
		deliver(t, c, mock, mustEnv(t, "BudgetGenerated", inst.ID, map[string]string{"total_amount": "1.00"}))
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		f.resetOutbox()
		deliver(t, c, mock, mustEnv(t, "PaymentFailed", inst.ID, map[string]string{}))
		assert.Equal(t, domain.SERVICE_ORDER_STATUS_REJECTED, f.status(t))
		assert.Equal(t, []string{"ServiceOrderStatusChanged", "CancelBudget", "ReleaseStock"}, f.sent(t))
	})
	t.Run("execution failed cancels", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
		c, mock, _ := newReplyConsumer(t, f)
		require.NoError(t, f.orch.Send(ctx, soID, ""))
		inst := f.saga(t)
		deliver(t, c, mock, mustEnv(t, "BudgetGenerated", inst.ID, map[string]string{"total_amount": "1.00"}))
		require.NoError(t, f.orch.Accept(ctx, soID, userID, ""))
		deliver(t, c, mock, mustEnv(t, "PaymentApproved", inst.ID, map[string]string{}))
		f.resetOutbox()
		deliver(t, c, mock, mustEnv(t, "ExecutionFailed", inst.ID, map[string]string{}))
		assert.Equal(t, domain.SERVICE_ORDER_STATUS_CANCELLED, f.status(t))
		assert.Equal(t, []string{"ServiceOrderStatusChanged", "RefundPayment", "CancelBudget", "ReleaseStock"}, f.sent(t))
	})
}

func mustEnv(t *testing.T, typ, sagaID string, payload any) messaging.Envelope {
	t.Helper()
	env, err := messaging.NewEnvelope(typ, sagaID, soID, "req-1", payload)
	require.NoError(t, err)
	return env
}

func TestRepliesThroughConsumerDeliverErrorsToTheQueue(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
	require.NoError(t, f.orch.Send(context.Background(), soID, ""))
	inst := f.saga(t)
	c, mock, q := newReplyConsumer(t, f)

	// a malformed payload makes the handler fail: the transaction rolls back and the message stays queued
	bad := messaging.Envelope{MessageID: "11111111-2222-4333-8444-555555555555", Type: "BudgetGenerated",
		SagaID: inst.ID, ServiceOrderID: soID, Payload: json.RawMessage(`{"total_amount":"nope"}`)}
	body, err := json.Marshal(bad)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WithArgs(bad.MessageID).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectRollback()
	assert.Error(t, c.Handle(context.Background(), messaging.Message{Body: string(body), ReceiptHandle: "rh"}))
	assert.Empty(t, q.deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}
