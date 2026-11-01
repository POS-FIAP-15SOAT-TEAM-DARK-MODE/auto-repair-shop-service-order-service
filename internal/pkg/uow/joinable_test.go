package uow

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransactionalUoW_NestedExecuteJoinsOuterTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Only one transaction is opened and committed: the inner unit of work joins it.
	mock.ExpectBegin()
	mock.ExpectCommit()

	var innerRan bool
	err = NewTransactionalUoW(db).Execute(context.Background(), func(ctx context.Context) error {
		return NewTransactionalUoW(db).Execute(ctx, func(inner context.Context) error {
			_, err := GetTransaction(inner)
			innerRan = err == nil
			return err
		})
	})

	require.NoError(t, err)
	assert.True(t, innerRan)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionalUoW_NestedErrorRollsBackOuter(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectRollback()

	boom := errors.New("boom")
	err = NewTransactionalUoW(db).Execute(context.Background(), func(ctx context.Context) error {
		return NewTransactionalUoW(db).Execute(ctx, func(context.Context) error { return boom })
	})

	assert.ErrorIs(t, err, boom)
	assert.NoError(t, mock.ExpectationsWereMet())
}
