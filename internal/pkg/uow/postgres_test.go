package uow

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func TestTransactionalUoW_CommitRunsHooksAfterCommit(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	var ran bool
	err := NewTransactionalUoW(db).Execute(context.Background(), func(ctx context.Context) error {
		OnCommit(ctx, func(context.Context) { ran = true })
		assert.False(t, ran, "hook must not run before commit")
		return nil
	})
	require.NoError(t, err)
	assert.True(t, ran)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionalUoW_RollbackDropsHooks(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	var ran bool
	boom := errors.New("boom")
	err := NewTransactionalUoW(db).Execute(context.Background(), func(ctx context.Context) error {
		OnCommit(ctx, func(context.Context) { ran = true })
		return boom
	})
	require.ErrorIs(t, err, boom)
	assert.False(t, ran)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionalUoW_NestedJoinsOuterTransaction(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	err := NewTransactionalUoW(db).Execute(context.Background(), func(ctx context.Context) error {
		return NewTransactionalUoW(db).Execute(ctx, func(inner context.Context) error {
			_, err := GetTransaction(inner)
			return err
		})
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionalUoW_BeginError(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin().WillReturnError(errors.New("no conn"))
	err := NewTransactionalUoW(db).Execute(context.Background(), func(context.Context) error { return nil })
	require.Error(t, err)
}

func TestGetTransactionAndConn_WithoutTx(t *testing.T) {
	db, _ := newMockDB(t)
	_, err := GetTransaction(context.Background())
	require.ErrorIs(t, err, ErrMissingPostgresTransaction)
	assert.Equal(t, db, Conn(context.Background(), db))
}

func TestOnCommit_WithoutCollectorRunsImmediately(t *testing.T) {
	var ran bool
	OnCommit(context.Background(), func(context.Context) { ran = true })
	assert.True(t, ran)
	RunAfterCommit(context.Background()) // no-op without collector
}

func TestOnSuccessAndFailure_WithoutTx(t *testing.T) {
	require.ErrorIs(t, onSuccess(context.Background()), ErrMissingPostgresTransaction)
	require.ErrorIs(t, onFailure(context.Background(), nil), ErrMissingPostgresTransaction)
}
