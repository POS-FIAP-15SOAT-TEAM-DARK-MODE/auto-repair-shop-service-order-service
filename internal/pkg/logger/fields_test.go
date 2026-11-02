package logger

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func observed(t *testing.T) (context.Context, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	return WithZap(context.Background(), zap.New(core)), logs
}

func TestGlobal_CarriesServiceField(t *testing.T) {
	assert.NotNil(t, Global())
	core, logs := observer.New(zapcore.InfoLevel)
	l := newCtxLogger(zap.New(core).With(zap.String("service", defaultServiceName)), nil)
	l.Info("probe")
	assert.Equal(t, defaultServiceName, logs.All()[0].ContextMap()["service"])
}

func TestWithFields_AddsFieldsToEveryLine(t *testing.T) {
	ctx, logs := observed(t)
	ctx = WithFields(ctx, zap.String("saga_id", "s-1"), zap.String("message_id", "m-1"))

	Of(ctx).Info("saga.started")
	Of(ctx).Warn("saga.deadline_expired")

	entries := logs.All()
	assert.Len(t, entries, 2)
	for _, e := range entries {
		assert.Equal(t, "s-1", e.ContextMap()["saga_id"])
		assert.Equal(t, "m-1", e.ContextMap()["message_id"])
	}
}

func TestWithFields_DropsCallSiteKeysAlreadySet(t *testing.T) {
	ctx, logs := observed(t)
	ctx = WithFields(ctx, zap.String("saga_id", "s-1"))

	Of(ctx).Info("saga.reply_ignored", zap.String("saga_id", "s-1"), zap.String("type", "PaymentFailed"))

	entry := logs.All()[0]
	assert.Equal(t, "s-1", entry.ContextMap()["saga_id"])
	assert.Equal(t, "PaymentFailed", entry.ContextMap()["type"])
	count := 0
	for _, f := range entry.Context {
		if f.Key == "saga_id" {
			count++
		}
	}
	assert.Equal(t, 1, count, "a key must appear once per line")
}

func TestWithFields_NestedContextsKeepEarlierFields(t *testing.T) {
	ctx, logs := observed(t)
	ctx = WithFields(ctx, zap.String("message_id", "m-1"))
	ctx = WithFields(ctx, zap.String("saga_id", "s-1"), zap.String("message_id", "m-2"))

	Of(ctx).Info("message.handled")

	entry := logs.All()[0]
	assert.Equal(t, "m-2", entry.ContextMap()["message_id"])
	assert.Equal(t, "s-1", entry.ContextMap()["saga_id"])
	count := 0
	for _, f := range entry.Context {
		if f.Key == "message_id" {
			count++
		}
	}
	assert.Equal(t, 1, count, "a replaced key must not be repeated")
}

func TestDedupe_WithoutContextFieldsKeepsEverything(t *testing.T) {
	l := &customLogger{Logger: zap.NewNop()}
	fields := []zap.Field{zap.String("a", "1")}
	assert.Equal(t, fields, l.dedupe(fields))
}

func TestWithFields_OnPlainLoggerContextUsesItsLogger(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	ctx := withContext(context.Background(), &customLogger{Logger: zap.New(core)})
	ctx = WithFields(ctx, zap.String("saga_id", "s-1"))

	Of(ctx).Info("saga.started")

	assert.Equal(t, "s-1", logs.All()[0].ContextMap()["saga_id"])
}

func TestOf_ErrorLinesUseTheErrorText(t *testing.T) {
	ctx, logs := observed(t)
	ctx = WithFields(ctx, zap.String("service_order_id", "so-1"))

	Of(ctx).Error(assertErr("boom"), zap.String("component", "outbox-relay"))

	entry := logs.All()[0]
	assert.Equal(t, "boom", entry.Message)
	assert.Equal(t, "so-1", entry.ContextMap()["service_order_id"])
	assert.Equal(t, "outbox-relay", entry.ContextMap()["component"])
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
