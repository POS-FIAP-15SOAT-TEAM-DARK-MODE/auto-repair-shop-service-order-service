package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDB = errors.New("connection reset")

var soColumns = []string{"id", "status", "total_amount",
	"customer_id", "customer_user_id", "customer_name", "customer_email", "customer_type",
	"customer_document", "customer_company_name", "customer_phone",
	"vehicle_id", "vehicle_license_plate", "vehicle_brand", "vehicle_model", "vehicle_year",
	"quote_sent_at", "completed_at", "delivered_at"}

var supplyColumns = []string{"supply_id", "reservation_id", "name", "description", "unit_price", "version", "quantity"}

func newSOPG(t *testing.T) (domain.ServiceOrderRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewSOPostgres(db), mock
}

func soRow(id, status, companyName string, quote, completed, delivered any) *sqlmock.Rows {
	return sqlmock.NewRows(soColumns).AddRow(id, status, "221.00",
		"c1", "user-1", "João", "joao@example.com", "PF",
		"00000000000", companyName, "11999990000",
		"v1", "ABC1D23", "Fiat", "Uno", 2018,
		quote, completed, delivered)
}

func TestSOPostgresInsertAndUpdate(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSOPG(t)
	so := &domain.ServiceOrder{ID: "so-1", Status: domain.SERVICE_ORDER_STATUS_NEW, TotalAmount: decimal.Zero,
		Customer: domain.CustomerSnapshot{ID: "c1", UserID: "u1", Name: "João", Email: "j@x", Type: "PF", Document: "0", Phone: "1"},
		Vehicle:  domain.VehicleSnapshot{ID: "v1", LicensePlate: "ABC1D23", Brand: "Fiat", Model: "Uno", Year: 2018}}

	mock.ExpectExec("INSERT INTO service_order").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.Insert(ctx, so))

	mock.ExpectExec("INSERT INTO service_order").WillReturnError(errDB)
	assert.Error(t, repo.Insert(ctx, so))

	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	so.QuoteSentAt = &now
	mock.ExpectExec("UPDATE service_order").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Update(ctx, so))

	mock.ExpectExec("UPDATE service_order").WillReturnResult(sqlmock.NewResult(0, 0))
	assert.ErrorIs(t, repo.Update(ctx, so), domain.ErrServiceOrderNotFound)

	mock.ExpectExec("UPDATE service_order").WillReturnResult(sqlmock.NewErrorResult(errDB))
	assert.Error(t, repo.Update(ctx, so))

	mock.ExpectExec("UPDATE service_order").WillReturnError(errDB)
	assert.Error(t, repo.Update(ctx, so))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSOPostgresFindByIDAndStatus(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSOPG(t)
	quote := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery("FROM service_order so WHERE so.id = \\$1 FOR UPDATE").WithArgs("so-1").
		WillReturnRows(soRow("so-1", "AWAITING_APPROVAL", "Oficina LTDA", quote, nil, nil))
	so, err := repo.FindByID(ctx, "so-1")
	require.NoError(t, err)
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL, so.Status)
	assert.Equal(t, "Oficina LTDA", so.Customer.CompanyName)
	assert.Equal(t, "221.00", so.TotalAmount.StringFixed(2))
	require.NotNil(t, so.QuoteSentAt)
	assert.True(t, so.QuoteSentAt.Equal(quote))
	assert.Nil(t, so.CompletedAt)
	assert.Nil(t, so.DeliveredAt)

	mock.ExpectQuery("FOR UPDATE").WithArgs("missing").WillReturnError(sql.ErrNoRows)
	_, err = repo.FindByID(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrServiceOrderNotFound)

	mock.ExpectQuery("FOR UPDATE").WithArgs("broken").WillReturnError(errDB)
	_, err = repo.FindByID(ctx, "broken")
	assert.Error(t, err)

	mock.ExpectQuery("FOR UPDATE").WithArgs("bad").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bad"))
	_, err = repo.FindByID(ctx, "bad")
	assert.Error(t, err)

	mock.ExpectQuery("SELECT status FROM service_order WHERE id").WithArgs("so-1").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("DELIVERED"))
	st, err := repo.FindStatus(ctx, "so-1")
	require.NoError(t, err)
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_DELIVERED, st)

	mock.ExpectQuery("SELECT status FROM service_order WHERE id").WithArgs("none").WillReturnError(sql.ErrNoRows)
	_, err = repo.FindStatus(ctx, "none")
	assert.ErrorIs(t, err, domain.ErrServiceOrderNotFound)

	mock.ExpectQuery("SELECT status FROM service_order WHERE id").WithArgs("down").WillReturnError(errDB)
	_, err = repo.FindStatus(ctx, "down")
	assert.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSOPostgresCountAndSearchBuildFilters(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSOPG(t)

	t.Run("count excludes terminal statuses by default", func(t *testing.T) {
		mock.ExpectQuery("SELECT COUNT\\(so.id\\) FROM service_order so WHERE so.status != ALL").
			WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
		n, err := repo.Count(ctx, &domain.ServiceOrderFilterParams{})
		require.NoError(t, err)
		assert.Equal(t, int64(7), n)
	})
	t.Run("count with explicit filters", func(t *testing.T) {
		mock.ExpectQuery("SELECT COUNT\\(so.id\\) FROM service_order so WHERE so.status = \\$1 AND so.customer_id = \\$2 AND so.vehicle_id = \\$3").
			WithArgs("NEW", "c1", "v1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		n, err := repo.Count(ctx, &domain.ServiceOrderFilterParams{Status: "NEW", CustomerID: "c1", VehicleID: "v1"})
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
	})
	t.Run("count error", func(t *testing.T) {
		mock.ExpectQuery("SELECT COUNT").WillReturnError(errDB)
		_, err := repo.Count(ctx, &domain.ServiceOrderFilterParams{})
		assert.Error(t, err)
	})
	t.Run("search sorts by status and paginates", func(t *testing.T) {
		quote := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
		mock.ExpectQuery("FROM service_order so WHERE so.status = \\$1 ORDER BY CASE so.status .* LIMIT \\$2 OFFSET \\$3").
			WithArgs("IN_PROGRESS", int64(5), int64(10)).
			WillReturnRows(soRow("so-1", "IN_PROGRESS", "", quote, nil, nil))
		items, err := repo.Search(ctx, &domain.ServiceOrderFilterParams{Status: "IN_PROGRESS", SortBy: "status", Limit: 5, Offset: 10})
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "so-1", items[0].ID)
		assert.Empty(t, items[0].Customer.CompanyName)
	})
	t.Run("search by default orders by creation date", func(t *testing.T) {
		mock.ExpectQuery("ORDER BY so.created_at ASC").WillReturnRows(soRow("so-2", "NEW", "", nil, nil, nil))
		items, err := repo.Search(ctx, &domain.ServiceOrderFilterParams{Limit: 10})
		require.NoError(t, err)
		assert.Len(t, items, 1)
	})
	t.Run("search query error", func(t *testing.T) {
		mock.ExpectQuery("SELECT so.id").WillReturnError(errDB)
		_, err := repo.Search(ctx, &domain.ServiceOrderFilterParams{Limit: 10})
		assert.Error(t, err)
	})
	t.Run("search scan error", func(t *testing.T) {
		mock.ExpectQuery("SELECT so.id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("x"))
		_, err := repo.Search(ctx, &domain.ServiceOrderFilterParams{Limit: 10})
		assert.Error(t, err)
	})
	t.Run("search iteration error", func(t *testing.T) {
		rows := soRow("so-3", "NEW", "", nil, nil, nil).RowError(0, errDB)
		mock.ExpectQuery("SELECT so.id").WillReturnRows(rows)
		_, err := repo.Search(ctx, &domain.ServiceOrderFilterParams{Limit: 10})
		assert.Error(t, err)
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSOPostgresWorks(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSOPG(t)

	mock.ExpectQuery("FROM service_order_work").WithArgs("so-1").
		WillReturnRows(sqlmock.NewRows([]string{"work_id", "name", "description", "unit_price"}).
			AddRow("w1", "Troca de óleo", "Óleo", "150.00").
			AddRow("w2", "Alinhamento", nil, "80"))
	works, err := repo.ListWorks(ctx, "so-1")
	require.NoError(t, err)
	require.Len(t, works, 2)
	assert.Equal(t, "Óleo", works[0].Description)
	assert.Empty(t, works[1].Description, "a null description is empty")

	mock.ExpectQuery("FROM service_order_work").WithArgs("none").WillReturnRows(sqlmock.NewRows([]string{"work_id"}))
	works, err = repo.ListWorks(ctx, "none")
	require.NoError(t, err)
	assert.Empty(t, works)

	mock.ExpectQuery("FROM service_order_work").WillReturnError(errDB)
	_, err = repo.ListWorks(ctx, "x")
	assert.Error(t, err)

	mock.ExpectQuery("FROM service_order_work").WillReturnRows(sqlmock.NewRows([]string{"work_id"}).AddRow("w1"))
	_, err = repo.ListWorks(ctx, "x")
	assert.Error(t, err)

	mock.ExpectQuery("FROM service_order_work").WillReturnRows(
		sqlmock.NewRows([]string{"work_id", "name", "description", "unit_price"}).AddRow("w1", "n", nil, "1").RowError(0, errDB))
	_, err = repo.ListWorks(ctx, "x")
	assert.Error(t, err)

	mock.ExpectExec("INSERT INTO service_order_work").WithArgs(sqlmock.AnyArg(), "so-1", "w1", "Troca", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.AddWork(ctx, "so-1", domain.WorkItem{ID: "w1", Name: "Troca", UnitPrice: decimal.NewFromInt(1)}))
	mock.ExpectExec("INSERT INTO service_order_work").WillReturnError(errDB)
	assert.Error(t, repo.AddWork(ctx, "so-1", domain.WorkItem{ID: "w1"}))

	mock.ExpectExec("DELETE FROM service_order_work").WithArgs("so-1", "w1").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.RemoveWork(ctx, "so-1", "w1"))
	mock.ExpectExec("DELETE FROM service_order_work").WillReturnResult(sqlmock.NewResult(0, 0))
	assert.ErrorIs(t, repo.RemoveWork(ctx, "so-1", "w9"), domain.ErrServiceOrderWorkNotFound)
	mock.ExpectExec("DELETE FROM service_order_work").WillReturnError(errDB)
	assert.Error(t, repo.RemoveWork(ctx, "so-1", "w1"))
	mock.ExpectExec("DELETE FROM service_order_work").WillReturnResult(sqlmock.NewErrorResult(errDB))
	assert.Error(t, repo.RemoveWork(ctx, "so-1", "w1"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSOPostgresSupplies(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSOPG(t)

	mock.ExpectQuery("FROM service_order_supply").WithArgs("so-1").
		WillReturnRows(sqlmock.NewRows(supplyColumns).AddRow("s1", "r1", "Filtro", "Óleo", "35.50", 3, 2))
	supplies, err := repo.ListSupplies(ctx, "so-1")
	require.NoError(t, err)
	require.Len(t, supplies, 1)
	assert.Equal(t, "r1", supplies[0].ReservationID)
	assert.Equal(t, 2, supplies[0].Quantity)

	mock.ExpectQuery("FROM service_order_supply").WillReturnError(errDB)
	_, err = repo.ListSupplies(ctx, "x")
	assert.Error(t, err)
	mock.ExpectQuery("FROM service_order_supply").WillReturnRows(sqlmock.NewRows([]string{"supply_id"}).AddRow("s1"))
	_, err = repo.ListSupplies(ctx, "x")
	assert.Error(t, err)
	mock.ExpectQuery("FROM service_order_supply").WillReturnRows(
		sqlmock.NewRows(supplyColumns).AddRow("s1", "r1", "n", nil, "1", 1, 1).RowError(0, errDB))
	_, err = repo.ListSupplies(ctx, "x")
	assert.Error(t, err)

	mock.ExpectExec("INSERT INTO service_order_supply").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.AddSupply(ctx, "so-1", domain.SupplyItem{ID: "s1", ReservationID: "r1", Name: "Filtro", Quantity: 1, UnitPrice: decimal.NewFromInt(1)}))
	mock.ExpectExec("INSERT INTO service_order_supply").WillReturnError(errDB)
	assert.Error(t, repo.AddSupply(ctx, "so-1", domain.SupplyItem{ID: "s1"}))

	mock.ExpectQuery("DELETE FROM service_order_supply").WithArgs("so-1", "s1").
		WillReturnRows(sqlmock.NewRows(supplyColumns).AddRow("s1", "r1", "Filtro", "", "35.50", 3, 2))
	removed, err := repo.RemoveSupply(ctx, "so-1", "s1")
	require.NoError(t, err)
	assert.Equal(t, "r1", removed.ReservationID)

	mock.ExpectQuery("DELETE FROM service_order_supply").WillReturnError(sql.ErrNoRows)
	_, err = repo.RemoveSupply(ctx, "so-1", "s9")
	assert.ErrorIs(t, err, domain.ErrServiceOrderSupplyNotFound)
	mock.ExpectQuery("DELETE FROM service_order_supply").WillReturnError(errDB)
	_, err = repo.RemoveSupply(ctx, "so-1", "s1")
	assert.Error(t, err)

	mock.ExpectExec("UPDATE service_order SET total_amount").WithArgs("so-1", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.UpdateTotal(ctx, "so-1", decimal.NewFromInt(10)))
	mock.ExpectExec("UPDATE service_order SET total_amount").WillReturnResult(sqlmock.NewResult(0, 0))
	assert.ErrorIs(t, repo.UpdateTotal(ctx, "missing", decimal.Zero), domain.ErrServiceOrderNotFound)
	mock.ExpectExec("UPDATE service_order SET total_amount").WillReturnError(errDB)
	assert.Error(t, repo.UpdateTotal(ctx, "so-1", decimal.Zero))
	mock.ExpectExec("UPDATE service_order SET total_amount").WillReturnResult(sqlmock.NewErrorResult(errDB))
	assert.Error(t, repo.UpdateTotal(ctx, "so-1", decimal.Zero))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSOPostgresExists(t *testing.T) {
	ctx := context.Background()
	repo, mock := newSOPG(t)
	mock.ExpectQuery("SELECT EXISTS").WithArgs("c1", "v1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	found, err := repo.Exists(ctx, "c1", "v1")
	require.NoError(t, err)
	assert.True(t, found)

	mock.ExpectQuery("SELECT EXISTS").WillReturnError(errDB)
	_, err = repo.Exists(ctx, "c1", "v1")
	assert.Error(t, err)
}

func TestSOPostgresAggregateHelpers(t *testing.T) {
	assert.False(t, nullString("").Valid)
	assert.Equal(t, "x", nullString("x").String)
	assert.False(t, nullTime(nil).Valid)
	assert.Nil(t, timePtr(sql.NullTime{}))
	now := time.Now()
	assert.Equal(t, now, *timePtr(sql.NullTime{Time: now, Valid: true}))
}
