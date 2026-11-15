package saga

import (
	"context"
	"database/sql"
	"errors"
	"time"

	pgPkg "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/db/postgres"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
)

const sagaColumns = `id, service_order_id, step, status, budget_requested, payment_requested,
	payment_approved, execution_enqueued, deadline, created_at, updated_at`

const (
	insertSagaQuery = `INSERT INTO saga_instance (id, service_order_id, step, status, budget_requested,
		payment_requested, payment_approved, execution_enqueued, deadline)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	updateSagaQuery = `UPDATE saga_instance
		SET step = $2, status = $3, budget_requested = $4, payment_requested = $5,
		    payment_approved = $6, execution_enqueued = $7, deadline = $8, updated_at = NOW()
		WHERE id = $1`

	findSagaByIDQuery      = `SELECT ` + sagaColumns + ` FROM saga_instance WHERE id = $1 FOR UPDATE`
	findRunningSagaByOrder = `SELECT ` + sagaColumns + ` FROM saga_instance WHERE service_order_id = $1 AND status = 'RUNNING'`
	findExpiredSagasQuery  = `SELECT ` + sagaColumns + ` FROM saga_instance
		WHERE status = 'RUNNING' AND deadline IS NOT NULL AND deadline < $1
		ORDER BY deadline ASC LIMIT $2`
)

type pgRepository struct {
	db *sql.DB
}

// NewPostgresRepository stores saga instances in saga_instance.
func NewPostgresRepository(db *sql.DB) Repository {
	return &pgRepository{db: db}
}

func (r *pgRepository) conn(ctx context.Context) uow.Queryer {
	return uow.Conn(ctx, r.db)
}

func (r *pgRepository) Insert(ctx context.Context, inst *Instance) error {
	_, err := r.conn(ctx).ExecContext(ctx, insertSagaQuery, inst.ID, inst.ServiceOrderID, string(inst.Step),
		string(inst.Status), inst.BudgetRequested, inst.PaymentRequested, inst.PaymentApproved,
		inst.ExecutionEnqueued, nullTime(inst.Deadline))
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	return nil
}

func (r *pgRepository) Update(ctx context.Context, inst *Instance) error {
	_, err := r.conn(ctx).ExecContext(ctx, updateSagaQuery, inst.ID, string(inst.Step), string(inst.Status),
		inst.BudgetRequested, inst.PaymentRequested, inst.PaymentApproved, inst.ExecutionEnqueued,
		nullTime(inst.Deadline))
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	return nil
}

func (r *pgRepository) FindByID(ctx context.Context, id string) (*Instance, error) {
	inst, err := scanInstance(r.conn(ctx).QueryRowContext(ctx, findSagaByIDQuery, id))
	return optional(ctx, inst, err)
}

func (r *pgRepository) FindRunningBySO(ctx context.Context, serviceOrderID string) (*Instance, error) {
	inst, err := scanInstance(r.conn(ctx).QueryRowContext(ctx, findRunningSagaByOrder, serviceOrderID))
	return optional(ctx, inst, err)
}

func (r *pgRepository) FindExpired(ctx context.Context, now time.Time, limit int) ([]Instance, error) {
	rows, err := r.conn(ctx).QueryContext(ctx, findExpiredSagasQuery, now, limit)
	if err != nil {
		return nil, pgPkg.Error(ctx, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Instance
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, pgPkg.Error(ctx, err)
		}
		out = append(out, *inst)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInstance(s rowScanner) (*Instance, error) {
	var inst Instance
	var step, status string
	var deadline sql.NullTime
	err := s.Scan(&inst.ID, &inst.ServiceOrderID, &step, &status, &inst.BudgetRequested, &inst.PaymentRequested,
		&inst.PaymentApproved, &inst.ExecutionEnqueued, &deadline, &inst.CreatedAt, &inst.UpdatedAt)
	if err != nil {
		return nil, err
	}
	inst.Step = Step(step)
	inst.Status = Status(status)
	if deadline.Valid {
		t := deadline.Time
		inst.Deadline = &t
	}
	return &inst, nil
}

// optional maps "no rows" to a nil instance.
func optional(ctx context.Context, inst *Instance, err error) (*Instance, error) {
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, pgPkg.Error(ctx, err)
	}
	return inst, nil
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
