package uow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	pglib "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/db/postgres"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"go.uber.org/zap"
)

type txKey struct{}

type hooksKey struct{}

var ErrMissingPostgresTransaction = errors.New("transaction not found")

// Queryer is the subset of *sql.DB and *sql.Tx used by repositories, so the same
// repository code runs both inside and outside a transaction.
type Queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// commitHooks collects side effects that must only run after the transaction commits
// (status notifications, metrics). They are dropped on rollback.
type commitHooks struct {
	fns []func(context.Context)
}

func NewTransactionalUoW(db *sql.DB) Executor {
	return &joinableUoW{inner: &UnitOfWork{
		OnStart:   onStart(db),
		OnSuccess: onSuccess,
		OnFailure: onFailure,
	}}
}

// joinableUoW lets a unit of work run inside a transaction that is already
// attached to the context (e.g. the consumer's dedupe + handler transaction).
// Nested calls join the outer transaction instead of opening a second one, so
// the outer unit of work alone decides commit or rollback.
type joinableUoW struct {
	inner *UnitOfWork
}

func (u *joinableUoW) Execute(ctx context.Context, steps ...Step) error {
	if _, found := txFrom(ctx); !found {
		return u.inner.Execute(ctx, steps...)
	}
	for _, step := range steps {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

func onStart(db *sql.DB) func(context.Context) (context.Context, error) {
	return func(ctx context.Context) (context.Context, error) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			logger.Global().Debug("failed to begin transaction", zap.String("operation", "uow_onStart"), zap.Error(err))
			return ctx, pglib.Error(ctx, fmt.Errorf("begin tx: %w", err))
		}
		logger.Global().Debug("transaction started", zap.String("operation", "uow_onStart"))
		return WithTx(ctx, tx), nil
	}
}

func onSuccess(ctx context.Context) error {
	tx, found := txFrom(ctx)
	if !found {
		logger.Global().Debug("postgres transaction not found on success", zap.String("operation", "uow_onSuccess"))
		return ErrMissingPostgresTransaction
	}
	if err := tx.Commit(); err != nil {
		logger.Global().Debug("failed to commit transaction", zap.String("operation", "uow_onSuccess"), zap.Error(err))
		return err
	}
	logger.Global().Debug("transaction committed successfully", zap.String("operation", "uow_onSuccess"))
	RunAfterCommit(ctx)
	return nil
}

func onFailure(ctx context.Context, _ error) error {
	tx, found := txFrom(ctx)
	if !found {
		logger.Global().Debug("postgres transaction not found on failure", zap.String("operation", "uow_onFailure"))
		return ErrMissingPostgresTransaction
	}
	if err := tx.Rollback(); err != nil {
		logger.Global().Debug("failed to rollback transaction", zap.String("operation", "uow_onFailure"), zap.Error(err))
		return err
	}
	logger.Global().Debug("transaction rolled back", zap.String("operation", "uow_onFailure"))
	return nil
}

// WithTx attaches tx (and a fresh after-commit hook collector) to ctx. The consumer uses it
// right after BeginTx so repositories and the outbox join the consumer's transaction.
func WithTx(ctx context.Context, tx *sql.Tx) context.Context {
	ctx = context.WithValue(ctx, txKey{}, tx)
	return context.WithValue(ctx, hooksKey{}, &commitHooks{})
}

func txFrom(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	return tx, tx != nil && ok
}

// GetTransaction returns the transaction attached to ctx, or ErrMissingPostgresTransaction.
func GetTransaction(ctx context.Context) (*sql.Tx, error) {
	tx, found := txFrom(ctx)
	if !found {
		logger.Global().Debug("postgres transaction not found when requested", zap.String("operation", "uow_GetTransaction"))
		return nil, ErrMissingPostgresTransaction
	}
	return tx, nil
}

// Conn returns the transaction attached to ctx when there is one, otherwise db.
// Reads made by a handler therefore see the handler's own uncommitted writes.
func Conn(ctx context.Context, db *sql.DB) Queryer {
	if tx, found := txFrom(ctx); found {
		return tx
	}
	return db
}

// OnCommit runs fn after the enclosing transaction commits. Outside a transaction
// (e.g. in-memory units of work) fn runs immediately.
func OnCommit(ctx context.Context, fn func(context.Context)) {
	if h, ok := ctx.Value(hooksKey{}).(*commitHooks); ok && h != nil {
		h.fns = append(h.fns, fn)
		return
	}
	fn(ctx)
}

// RunAfterCommit executes the hooks registered in ctx. Call it once, after a successful commit.
func RunAfterCommit(ctx context.Context) {
	h, ok := ctx.Value(hooksKey{}).(*commitHooks)
	if !ok || h == nil {
		return
	}
	for _, fn := range h.fns {
		fn(ctx)
	}
	h.fns = nil
}
