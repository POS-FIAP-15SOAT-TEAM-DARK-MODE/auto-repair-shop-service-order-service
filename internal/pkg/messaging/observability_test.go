package messaging

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	obsMessageID = "c3f1d2e4-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	obsSagaID    = "9d8c7b6a-1234-4abc-9def-001122334455"
	obsSOID      = "01JD8Z4R3Q8M2V7K5N9XWABCDE"
)

func observedCtx() (context.Context, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return logger.WithZap(context.Background(), zap.New(core)), logs
}

func TestEnvelope_LogFields_IncludesOnlyPresentIDs(t *testing.T) {
	full := Envelope{MessageID: obsMessageID, SagaID: obsSagaID, ServiceOrderID: obsSOID}
	assert.Len(t, full.LogFields(), 3)

	bare := Envelope{MessageID: obsMessageID}
	fields := bare.LogFields()
	require.Len(t, fields, 1)
	assert.Equal(t, "message_id", fields[0].Key)
}

func TestConsumer_Handle_PutsIDsInTheLoggerContext(t *testing.T) {
	db, mock := newConsumerDB(t)
	q := &fakeQueue{}
	c := NewConsumer(db, q, "http://queue")

	var seen []string
	c.RegisterHandler("CancelBudget", func(ctx context.Context, _ *sql.Tx, env Envelope) error {
		logger.Of(ctx).Info("saga.reply_logged")
		return nil
	})
	body := `{"message_id":"` + obsMessageID + `","saga_id":"` + obsSagaID + `","service_order_id":"` + obsSOID +
		`","type":"CancelBudget","payload":{}}`

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WithArgs(obsMessageID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ctx, logs := observedCtx()
	require.NoError(t, c.Handle(ctx, Message{Body: body, ReceiptHandle: "r1"}))
	require.NoError(t, mock.ExpectationsWereMet())

	for _, e := range logs.All() {
		seen = append(seen, e.Message)
		assert.Equal(t, obsMessageID, e.ContextMap()["message_id"], e.Message)
		assert.Equal(t, obsSagaID, e.ContextMap()["saga_id"], e.Message)
		assert.Equal(t, obsSOID, e.ContextMap()["service_order_id"], e.Message)
	}
	assert.Equal(t, []string{"saga.reply_logged", "message.handled"}, seen)
	assert.Equal(t, []string{"r1"}, q.deleted)
}

func TestConsumer_Handle_DuplicateAndUnknownAreLoggedWithIDs(t *testing.T) {
	db, mock := newConsumerDB(t)
	q := &fakeQueue{}
	c := NewConsumer(db, q, "http://queue")
	c.RegisterHandler("CancelBudget", func(context.Context, *sql.Tx, Envelope) error { return nil })

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_message").WithArgs(obsMessageID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	body := `{"message_id":"` + obsMessageID + `","saga_id":"` + obsSagaID + `","type":"CancelBudget","payload":{}}`

	ctx, logs := observedCtx()
	require.NoError(t, c.Handle(ctx, Message{Body: body, ReceiptHandle: "dup"}))
	require.NoError(t, mock.ExpectationsWereMet())

	unknown := `{"message_id":"` + obsMessageID + `","type":"Nope","payload":{}}`
	require.NoError(t, c.Handle(ctx, Message{Body: unknown, ReceiptHandle: "unk"}))

	entries := logs.All()
	require.Len(t, entries, 2)
	assert.Equal(t, "message.duplicate_ignored", entries[0].Message)
	assert.Equal(t, "message.discarded", entries[1].Message)
	assert.Equal(t, "unknown_type", entries[1].ContextMap()["reason"])
	assert.Equal(t, []string{"dup", "unk"}, q.deleted)
}

func TestRelay_RunOnce_AddsSagaAndMessageAttributes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	pub := &fakePublisher{}
	relay := NewRelay(db, pub, map[string]string{"saga-commands": "arn:commands"})
	body := `{"message_id":"` + obsMessageID + `","saga_id":"` + obsSagaID + `","type":"GenerateBudget"}`

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(20).
		WillReturnRows(sqlmock.NewRows(pendingCols).AddRow(7, "saga-commands", "billing", "GenerateBudget", body))
	mock.ExpectExec("UPDATE outbox SET published_at").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ctx, logs := observedCtx()
	relay.db = db
	n, err := relay.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, map[string]string{
		"type":       "GenerateBudget",
		"message_id": obsMessageID,
		"saga_id":    obsSagaID,
		"target":     "billing",
	}, pub.attrs[0])

	require.Len(t, logs.All(), 1)
	entry := logs.All()[0]
	assert.Equal(t, "message.published", entry.Message)
	assert.Equal(t, obsSagaID, entry.ContextMap()["saga_id"])
	assert.Equal(t, obsMessageID, entry.ContextMap()["message_id"])
}

func TestRelay_RunOnce_InvalidBodyStaysPending(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	pub := &fakePublisher{}
	relay := NewRelay(db, pub, map[string]string{"billing-events": "arn:events"})

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE SKIP LOCKED").WithArgs(20).
		WillReturnRows(sqlmock.NewRows(pendingCols).AddRow(3, "billing-events", nil, "PaymentFailed", `not-json`))
	mock.ExpectCommit()

	n, err := relay.RunOnce(context.Background())
	assert.Equal(t, 0, n)
	assert.ErrorContains(t, err, "outbox row 3: body")
	assert.Empty(t, pub.calls)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRelay_RefreshPending_SetsGauge(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	relay := NewRelay(db, &fakePublisher{}, nil)
	mock.ExpectQuery("SELECT count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))
	relay.refreshPending(context.Background())
	assert.Equal(t, 4.0, testutil.ToFloat64(metrics.OutboxPending))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRelay_RefreshPending_DBErrorKeepsGauge(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	metrics.OutboxPending.Set(9)
	relay := NewRelay(db, &fakePublisher{}, nil)
	mock.ExpectQuery("SELECT count").WillReturnError(sql.ErrConnDone)

	ctx, logs := observedCtx()
	relay.refreshPending(ctx)
	assert.Equal(t, 9.0, testutil.ToFloat64(metrics.OutboxPending))
	require.Len(t, logs.All(), 1)
	assert.Equal(t, "outbox-relay", logs.All()[0].ContextMap()["component"])
}
