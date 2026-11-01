package saga

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sagaRowColumns = []string{"id", "service_order_id", "step", "status", "budget_requested", "payment_requested",
	"payment_approved", "execution_enqueued", "deadline", "created_at", "updated_at"}

func newSagaPG(t *testing.T) (Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewPostgresRepository(db), mock
}

func sampleInstance() *Instance {
	deadline := time.Date(2026, 11, 1, 12, 2, 0, 0, time.UTC)
	return &Instance{ID: "saga-1", ServiceOrderID: soID, Step: StepPayment, Status: StatusRunning,
		BudgetRequested: true, PaymentRequested: true, PaymentApproved: false, ExecutionEnqueued: false,
		Deadline: &deadline}
}

func TestSagaPostgresInsertAndUpdate(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSagaPG(t)
	inst := sampleInstance()

	mock.ExpectExec("INSERT INTO saga_instance").
		WithArgs("saga-1", soID, "PAYMENT", "RUNNING", true, true, false, false, inst.Deadline).
		WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.Insert(ctx, inst))

	mock.ExpectExec("INSERT INTO saga_instance").WillReturnError(errors.New("boom"))
	assert.Error(t, repo.Insert(ctx, inst))

	inst.Step = StepDone
	inst.Status = StatusCompleted
	inst.Deadline = nil
	mock.ExpectExec("UPDATE saga_instance").
		WithArgs("saga-1", "DONE", "COMPLETED", true, true, false, false, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Update(ctx, inst))

	mock.ExpectExec("UPDATE saga_instance").WillReturnError(errors.New("boom"))
	assert.Error(t, repo.Update(ctx, inst))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSagaPostgresFindByID(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSagaPG(t)
	created := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 11, 1, 12, 2, 0, 0, time.UTC)

	t.Run("found with deadline", func(t *testing.T) {
		mock.ExpectQuery("SELECT .* FROM saga_instance WHERE id = \\$1 FOR UPDATE").WithArgs("saga-1").
			WillReturnRows(sqlmock.NewRows(sagaRowColumns).
				AddRow("saga-1", soID, "BUDGET", "RUNNING", true, false, false, false, deadline, created, created))
		inst, err := repo.FindByID(ctx, "saga-1")
		require.NoError(t, err)
		require.NotNil(t, inst)
		assert.Equal(t, StepBudget, inst.Step)
		assert.Equal(t, StatusRunning, inst.Status)
		require.NotNil(t, inst.Deadline)
		assert.True(t, inst.Deadline.Equal(deadline))
	})
	t.Run("found without deadline", func(t *testing.T) {
		mock.ExpectQuery("SELECT .* FROM saga_instance WHERE id = \\$1 FOR UPDATE").WithArgs("saga-2").
			WillReturnRows(sqlmock.NewRows(sagaRowColumns).
				AddRow("saga-2", soID, "AWAITING_CUSTOMER", "RUNNING", true, false, false, false, nil, created, created))
		inst, err := repo.FindByID(ctx, "saga-2")
		require.NoError(t, err)
		require.NotNil(t, inst)
		assert.Nil(t, inst.Deadline)
	})
	t.Run("missing returns nil", func(t *testing.T) {
		mock.ExpectQuery("SELECT .* FROM saga_instance WHERE id").WithArgs("nope").
			WillReturnError(sql.ErrNoRows)
		inst, err := repo.FindByID(ctx, "nope")
		require.NoError(t, err)
		assert.Nil(t, inst)
	})
	t.Run("query error is returned", func(t *testing.T) {
		mock.ExpectQuery("SELECT .* FROM saga_instance WHERE id").WithArgs("x").WillReturnError(errors.New("conn reset"))
		_, err := repo.FindByID(ctx, "x")
		assert.Error(t, err)
	})
	t.Run("scan error is returned", func(t *testing.T) {
		mock.ExpectQuery("SELECT .* FROM saga_instance WHERE id").WithArgs("bad").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bad"))
		_, err := repo.FindByID(ctx, "bad")
		assert.Error(t, err)
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSagaPostgresFindRunningBySO(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSagaPG(t)
	created := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT .* FROM saga_instance WHERE service_order_id = \\$1 AND status = 'RUNNING'").
		WithArgs(soID).WillReturnRows(sqlmock.NewRows(sagaRowColumns).
		AddRow("saga-1", soID, "PAYMENT", "RUNNING", true, true, false, false, nil, created, created))
	inst, err := repo.FindRunningBySO(ctx, soID)
	require.NoError(t, err)
	require.NotNil(t, inst)
	assert.Equal(t, "saga-1", inst.ID)

	mock.ExpectQuery("SELECT .* FROM saga_instance WHERE service_order_id").WithArgs("none").WillReturnError(sql.ErrNoRows)
	inst, err = repo.FindRunningBySO(ctx, "none")
	require.NoError(t, err)
	assert.Nil(t, inst)

	mock.ExpectQuery("SELECT .* FROM saga_instance WHERE service_order_id").WithArgs("down").WillReturnError(errors.New("down"))
	_, err = repo.FindRunningBySO(ctx, "down")
	assert.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSagaPostgresFindExpired(t *testing.T) {
	ctx := context.Background()
	created := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 11, 1, 12, 2, 0, 0, time.UTC)
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, time.UTC)

	t.Run("returns the expired rows", func(t *testing.T) {
		repo, mock := newSagaPG(t)
		mock.ExpectQuery("WHERE status = 'RUNNING' AND deadline IS NOT NULL AND deadline < \\$1").
			WithArgs(now, 10).WillReturnRows(sqlmock.NewRows(sagaRowColumns).
			AddRow("saga-1", soID, "BUDGET", "RUNNING", true, false, false, false, deadline, created, created).
			AddRow("saga-2", "so-2", "PAYMENT", "RUNNING", true, true, false, false, deadline, created, created))
		out, err := repo.FindExpired(ctx, now, 10)
		require.NoError(t, err)
		require.Len(t, out, 2)
		assert.Equal(t, "saga-2", out[1].ID)
		assert.Equal(t, StepPayment, out[1].Step)
	})
	t.Run("query error", func(t *testing.T) {
		repo, mock := newSagaPG(t)
		mock.ExpectQuery("deadline < \\$1").WillReturnError(errors.New("down"))
		_, err := repo.FindExpired(ctx, now, 10)
		assert.Error(t, err)
	})
	t.Run("scan error", func(t *testing.T) {
		repo, mock := newSagaPG(t)
		mock.ExpectQuery("deadline < \\$1").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("x"))
		_, err := repo.FindExpired(ctx, now, 10)
		assert.Error(t, err)
	})
	t.Run("row iteration error", func(t *testing.T) {
		repo, mock := newSagaPG(t)
		rows := sqlmock.NewRows(sagaRowColumns).
			AddRow("saga-1", soID, "BUDGET", "RUNNING", true, false, false, false, deadline, created, created).
			RowError(0, errors.New("broken cursor"))
		mock.ExpectQuery("deadline < \\$1").WillReturnRows(rows)
		_, err := repo.FindExpired(ctx, now, 10)
		assert.Error(t, err)
	})
}

func TestNullTimeHelper(t *testing.T) {
	assert.False(t, nullTime(nil).Valid)
	ts := time.Now()
	n := nullTime(&ts)
	assert.True(t, n.Valid)
	assert.True(t, n.Time.Equal(ts))
}
