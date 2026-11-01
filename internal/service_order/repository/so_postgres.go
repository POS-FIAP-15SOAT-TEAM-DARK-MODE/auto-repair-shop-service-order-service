package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	dbPkg "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/db"
	pgPkg "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/db/postgres"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type soPostgres struct {
	db *sql.DB
}

// NewSOPostgres returns the PostgreSQL repository. Reads and writes run in the transaction
// attached to the context when there is one (see uow.Conn).
func NewSOPostgres(db *sql.DB) domain.ServiceOrderRepository {
	return &soPostgres{db: db}
}

func (r *soPostgres) conn(ctx context.Context) uow.Queryer {
	return uow.Conn(ctx, r.db)
}

func (r *soPostgres) Insert(ctx context.Context, so *domain.ServiceOrder) error {
	_, err := r.conn(ctx).ExecContext(ctx, insertServiceOrderQuery,
		so.ID, so.Status.String(), so.TotalAmount,
		so.Customer.ID, so.Customer.UserID, so.Customer.Name, so.Customer.Email, so.Customer.Type,
		so.Customer.Document, nullString(so.Customer.CompanyName), so.Customer.Phone,
		so.Vehicle.ID, so.Vehicle.LicensePlate, so.Vehicle.Brand, so.Vehicle.Model, so.Vehicle.Year,
	)
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	return nil
}

func (r *soPostgres) Update(ctx context.Context, so *domain.ServiceOrder) error {
	res, err := r.conn(ctx).ExecContext(ctx, updateServiceOrderQuery,
		so.ID, so.Status.String(), so.TotalAmount,
		nullTime(so.QuoteSentAt), nullTime(so.CompletedAt), nullTime(so.DeliveredAt),
	)
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	return requireAffected(ctx, res, domain.ErrServiceOrderNotFound)
}

func (r *soPostgres) FindByID(ctx context.Context, id string) (domain.ServiceOrder, error) {
	row := r.conn(ctx).QueryRowContext(ctx, serviceOrderFindByIDQuery, id)
	so, err := scanServiceOrder(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ServiceOrder{}, domain.ErrServiceOrderNotFound
		}
		return domain.ServiceOrder{}, pgPkg.Error(ctx, err)
	}
	return so, nil
}

func (r *soPostgres) FindStatus(ctx context.Context, id string) (domain.SERVICE_ORDER_STATUS, error) {
	var status string
	if err := r.db.QueryRowContext(ctx, serviceOrderStatusQuery, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrServiceOrderNotFound
		}
		return "", pgPkg.Error(ctx, err)
	}
	return domain.StringToServiceOrderStatus(status), nil
}

func (r *soPostgres) Count(ctx context.Context, params *domain.ServiceOrderFilterParams) (int64, error) {
	qb := dbPkg.QueryBuilder(countServiceOrderQuery)
	if params.Status == "" {
		qb.AddNotIn("so.status", terminalStatuses)
	} else {
		qb.Add("so.status =", params.Status)
	}
	qb.Add("so.customer_id =", params.CustomerID)
	qb.Add("so.vehicle_id =", params.VehicleID)
	query, args := qb.Build()

	var total int64
	if err := r.conn(ctx).QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, pgPkg.Error(ctx, err)
	}
	return total, nil
}

func (r *soPostgres) Search(ctx context.Context, params *domain.ServiceOrderFilterParams) ([]domain.ServiceOrder, error) {
	qb := dbPkg.QueryBuilder(searchServiceOrderQuery)
	if params.Status == "" {
		qb.AddNotIn("so.status", terminalStatuses)
	} else {
		qb.Add("so.status =", params.Status)
	}
	qb.Add("so.customer_id =", params.CustomerID)
	qb.Add("so.vehicle_id =", params.VehicleID)
	if params.SortBy == "status" {
		qb.OrderByRaw(statusSortCaseExpr)
	} else {
		qb.OrderBy("so.created_at", dbPkg.ASC)
	}
	qb.AddPagination(params.Limit, params.Offset)
	query, args := qb.Build()

	rows, err := r.conn(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, pgPkg.Error(ctx, err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domain.ServiceOrder, 0, params.Limit)
	for rows.Next() {
		so, err := scanServiceOrder(rows)
		if err != nil {
			return nil, pgPkg.Error(ctx, err)
		}
		items = append(items, so)
	}
	if err = rows.Err(); err != nil {
		return nil, pgPkg.Error(ctx, err)
	}
	return items, nil
}

func (r *soPostgres) ListWorks(ctx context.Context, serviceOrderID string) ([]domain.WorkItem, error) {
	rows, err := r.conn(ctx).QueryContext(ctx, listWorksQuery, serviceOrderID)
	if err != nil {
		return nil, pgPkg.Error(ctx, err)
	}
	defer func() { _ = rows.Close() }()

	var works []domain.WorkItem
	for rows.Next() {
		var w domain.WorkItem
		var desc sql.NullString
		if err = rows.Scan(&w.ID, &w.Name, &desc, &w.UnitPrice); err != nil {
			return nil, pgPkg.Error(ctx, err)
		}
		w.Description = desc.String
		works = append(works, w)
	}
	if err = rows.Err(); err != nil {
		return nil, pgPkg.Error(ctx, err)
	}
	return works, nil
}

func (r *soPostgres) AddWork(ctx context.Context, serviceOrderID string, w domain.WorkItem) error {
	_, err := r.conn(ctx).ExecContext(ctx, insertWorkQuery,
		uuid.NewString(), serviceOrderID, w.ID, w.Name, nullString(w.Description), w.UnitPrice)
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	return nil
}

func (r *soPostgres) RemoveWork(ctx context.Context, serviceOrderID, workID string) error {
	res, err := r.conn(ctx).ExecContext(ctx, deleteWorkQuery, serviceOrderID, workID)
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	return requireAffected(ctx, res, domain.ErrServiceOrderWorkNotFound)
}

func (r *soPostgres) ListSupplies(ctx context.Context, serviceOrderID string) ([]domain.SupplyItem, error) {
	rows, err := r.conn(ctx).QueryContext(ctx, listSuppliesQuery, serviceOrderID)
	if err != nil {
		return nil, pgPkg.Error(ctx, err)
	}
	defer func() { _ = rows.Close() }()

	var supplies []domain.SupplyItem
	for rows.Next() {
		s, err := scanSupply(rows)
		if err != nil {
			return nil, pgPkg.Error(ctx, err)
		}
		supplies = append(supplies, s)
	}
	if err = rows.Err(); err != nil {
		return nil, pgPkg.Error(ctx, err)
	}
	return supplies, nil
}

func (r *soPostgres) AddSupply(ctx context.Context, serviceOrderID string, s domain.SupplyItem) error {
	_, err := r.conn(ctx).ExecContext(ctx, insertSupplyQuery,
		uuid.NewString(), serviceOrderID, s.ID, s.ReservationID, s.Name, nullString(s.Description),
		s.Version, s.Quantity, s.UnitPrice)
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	return nil
}

func (r *soPostgres) RemoveSupply(ctx context.Context, serviceOrderID, supplyID string) (domain.SupplyItem, error) {
	row := r.conn(ctx).QueryRowContext(ctx, deleteSupplyQuery, serviceOrderID, supplyID)
	s, err := scanSupply(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SupplyItem{}, domain.ErrServiceOrderSupplyNotFound
		}
		return domain.SupplyItem{}, pgPkg.Error(ctx, err)
	}
	return s, nil
}

func (r *soPostgres) UpdateTotal(ctx context.Context, serviceOrderID string, total decimal.Decimal) error {
	res, err := r.conn(ctx).ExecContext(ctx, updateTotalQuery, serviceOrderID, total)
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	return requireAffected(ctx, res, domain.ErrServiceOrderNotFound)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanServiceOrder(s scanner) (domain.ServiceOrder, error) {
	var so domain.ServiceOrder
	var status string
	var companyName sql.NullString
	var quote, completed, delivered sql.NullTime
	if err := s.Scan(
		&so.ID, &status, &so.TotalAmount,
		&so.Customer.ID, &so.Customer.UserID, &so.Customer.Name, &so.Customer.Email, &so.Customer.Type,
		&so.Customer.Document, &companyName, &so.Customer.Phone,
		&so.Vehicle.ID, &so.Vehicle.LicensePlate, &so.Vehicle.Brand, &so.Vehicle.Model, &so.Vehicle.Year,
		&quote, &completed, &delivered,
	); err != nil {
		return domain.ServiceOrder{}, err
	}
	so.Status = domain.StringToServiceOrderStatus(status)
	so.Customer.CompanyName = companyName.String
	so.QuoteSentAt = timePtr(quote)
	so.CompletedAt = timePtr(completed)
	so.DeliveredAt = timePtr(delivered)
	return so, nil
}

func scanSupply(s scanner) (domain.SupplyItem, error) {
	var item domain.SupplyItem
	var desc sql.NullString
	if err := s.Scan(&item.ID, &item.ReservationID, &item.Name, &desc, &item.UnitPrice, &item.Version, &item.Quantity); err != nil {
		return domain.SupplyItem{}, err
	}
	item.Description = desc.String
	return item, nil
}

func requireAffected(ctx context.Context, res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return pgPkg.Error(ctx, err)
	}
	if n == 0 {
		return notFound
	}
	return nil
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func timePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

func (r *soPostgres) Exists(ctx context.Context, customerID, vehicleID string) (bool, error) {
	var exists bool
	if err := r.conn(ctx).QueryRowContext(ctx, existsServiceOrderQuery, customerID, vehicleID).Scan(&exists); err != nil {
		return false, pgPkg.Error(ctx, err)
	}
	return exists, nil
}
