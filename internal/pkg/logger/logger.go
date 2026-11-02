package logger

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/env"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type customLogger struct {
	*zap.Logger
	// base is the logger without context fields; fields are the context fields that Logger
	// already carries. They are kept apart so WithFields can replace a key instead of repeating it.
	base   *zap.Logger
	fields []zap.Field
	// ctxKeys are the names of fields. A call-site field with one of these keys is dropped, so each
	// JSON line carries a key only once.
	ctxKeys map[string]struct{}
}

// newCtxLogger returns a logger whose lines carry fields on top of base.
func newCtxLogger(base *zap.Logger, fields []zap.Field) *customLogger {
	keys := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		keys[f.Key] = struct{}{}
	}
	return &customLogger{Logger: base.With(fields...), base: base, fields: fields, ctxKeys: keys}
}

type ctxKey struct{}

var (
	globalLog  *customLogger
	dsnPattern *regexp.Regexp
)

func init() {
	initGlobalLogger()
}

func initGlobalLogger() {
	globalLog = newCtxLogger(initLogger(), nil)
	dsnPattern = regexp.MustCompile(`://([^:/\s]+):([^@/\s]+)@`)
}

func initLogger() *zap.Logger {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.MessageKey = "message"
	cfg.Level = getLogLevel(env.GetString("LOG_LEVEL", "INFO"))

	l, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return l.With(zap.String("service", env.GetString("SERVICE_NAME", defaultServiceName)))
}

// defaultServiceName is the value of the "service" field when SERVICE_NAME is not set.
const defaultServiceName = "service-order-service"

func getLogLevel(level string) zap.AtomicLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return zap.NewAtomicLevelAt(zap.DebugLevel)
	case "PANIC":
		return zap.NewAtomicLevelAt(zap.PanicLevel)
	default:
		return zap.NewAtomicLevelAt(zap.InfoLevel)
	}
}

func Global() *customLogger {
	return globalLog
}

// WithZap returns a context whose loggers write through l. It lets tests capture the lines a
// component logs; production code keeps the logger created by Request and Global.
func WithZap(ctx context.Context, l *zap.Logger) context.Context {
	return withContext(ctx, newCtxLogger(l, nil))
}

// WithFields returns a context whose logger adds fields to every line logged through Of. A key
// already set in the context (for example request_id) is replaced by the new value.
func WithFields(ctx context.Context, fields ...zap.Field) context.Context {
	cur := Of(ctx)
	replaced := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		replaced[f.Key] = struct{}{}
	}
	merged := make([]zap.Field, 0, len(cur.fields)+len(fields))
	for _, f := range cur.fields {
		if _, ok := replaced[f.Key]; !ok {
			merged = append(merged, f)
		}
	}
	merged = append(merged, fields...)

	base := cur.base
	if base == nil {
		base = cur.Logger
	}
	return withContext(ctx, newCtxLogger(base, merged))
}

// Request attaches a per-request logger to ctx, tagging every log line with
// a request_id. If incomingID is non-empty (e.g. forwarded by an upstream
// API Gateway or a client-supplied X-Request-Id), it's reused as-is so log
// lines for the same request stay correlated across services; otherwise a
// new one is generated.
func Request(ctx context.Context, incomingID string) (context.Context, string) {
	reqId := incomingID
	if reqId == "" {
		reqId = uuid.New().String()
	}
	ctx = context.WithValue(ctx, requestIDKey{}, reqId)
	return withContext(ctx, newCtxLogger(initLogger(), []zap.Field{zap.String("request_id", reqId)})), reqId
}

type requestIDKey struct{}

// RequestID returns the request id attached by Request, or "" when there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func Of(ctx context.Context) *customLogger {
	if ctx == nil {
		return Global()
	}

	if l, ok := ctx.Value(ctxKey{}).(*customLogger); ok {
		return l
	}
	return Global()
}

func withContext(ctx context.Context, l *customLogger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

func (l *customLogger) Info(msg string, fields ...zap.Field) {
	l.Logger.Info(msg, sanitizeFields(l.dedupe(fields))...)
}

func (l *customLogger) Warn(msg string, fields ...zap.Field) {
	l.Logger.Warn(msg, sanitizeFields(l.dedupe(fields))...)
}

func (l *customLogger) Error(err error, fields ...zap.Field) {
	fields = append(fields, zap.Error(err))
	l.Logger.Error(err.Error(), sanitizeFields(l.dedupe(fields))...)
}

func (l *customLogger) Fatal(msg string, fields ...zap.Field) {
	l.Logger.Fatal(msg, sanitizeFields(l.dedupe(fields))...)
}

func (l *customLogger) Debug(msg string, fields ...zap.Field) {
	l.Logger.Debug(msg, sanitizeFields(l.dedupe(fields))...)
}

// dedupe drops the call-site fields whose key is already set by the context logger.
func (l *customLogger) dedupe(fields []zap.Field) []zap.Field {
	if len(l.ctxKeys) == 0 {
		return fields
	}
	out := make([]zap.Field, 0, len(fields))
	for _, f := range fields {
		if _, ok := l.ctxKeys[f.Key]; ok {
			continue
		}
		out = append(out, f)
	}
	return out
}

func (l *customLogger) Sync() {
	if l != nil {
		if err := l.Logger.Sync(); err != nil {
			err = fmt.Errorf("failed to sync logger: %w", err)
			l.Error(err)
		}
	}
}

func sanitizeFields(fields []zap.Field) []zap.Field {
	sanitized := make([]zap.Field, 0, len(fields))
	for _, field := range fields {
		key := strings.ToLower(strings.TrimSpace(field.Key))
		if isSensitiveKey(key) {
			sanitized = append(sanitized, zap.String(field.Key, "[REDACTED]"))
			continue
		}
		if field.Type == zapcore.StringType {
			value := redactDSNPassword(field.String)
			sanitized = append(sanitized, zap.String(field.Key, value))
			continue
		}
		sanitized = append(sanitized, field)
	}
	return sanitized
}

func isSensitiveKey(key string) bool {
	return key == "password" ||
		key == "senha" ||
		key == "pwd" ||
		key == "token" ||
		key == "authorization" ||
		key == "cpf" ||
		key == "cnpj" ||
		key == "document" ||
		key == "documento"
}

func redactDSNPassword(value string) string {
	return dsnPattern.ReplaceAllString(value, `://$1:[REDACTED]@`)
}
