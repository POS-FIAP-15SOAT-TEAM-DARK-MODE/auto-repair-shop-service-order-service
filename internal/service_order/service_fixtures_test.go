package service_order

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients/catalog"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/saga"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/adapters"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/repository"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const (
	customerID   = "c1"
	customerUser = "user-c1"
	vehicleID    = "v1"
	soID         = "01JD8Z4R3Q8M2V7K5N9XWABCDE"
)

// notifierSpy records the status notifications sent after commit.
type notifierSpy struct {
	calls []domain.StatusNotification
	err   error
}

func (n *notifierSpy) NotifyStatusChange(_ context.Context, msg domain.StatusNotification) error {
	n.calls = append(n.calls, msg)
	return n.err
}

// fakeCustomers is the Customer REST gateway.
type fakeCustomers struct {
	customers map[string]domain.CustomerSnapshot
	vehicles  map[string]domain.VehicleSnapshot
	owners    map[string]string
	err       error
}

func (f *fakeCustomers) GetCustomer(_ context.Context, id string) (domain.CustomerSnapshot, error) {
	if f.err != nil {
		return domain.CustomerSnapshot{}, f.err
	}
	c, ok := f.customers[id]
	if !ok {
		return domain.CustomerSnapshot{}, domain.ErrCustomerNotFound
	}
	return c, nil
}

func (f *fakeCustomers) GetVehicle(_ context.Context, id string) (domain.VehicleSnapshot, string, error) {
	if f.err != nil {
		return domain.VehicleSnapshot{}, "", f.err
	}
	v, ok := f.vehicles[id]
	if !ok {
		return domain.VehicleSnapshot{}, "", domain.ErrVehicleNotFound
	}
	return v, f.owners[id], nil
}

// fakeCatalog is the Catalog REST gateway: works, stock and reservations.
type fakeCatalog struct {
	works       map[string]domain.WorkItem
	inactive    map[string]bool
	supplies    map[string]domain.SupplyItem
	stock       map[string]int
	reservation map[string]reservedRow
	seq         int
	workErr     error
	reserveErr  map[string]error
	releaseErr  error
	released    []string
}

type reservedRow struct {
	supplyID string
	qty      int
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{
		works:       map[string]domain.WorkItem{},
		inactive:    map[string]bool{},
		supplies:    map[string]domain.SupplyItem{},
		stock:       map[string]int{},
		reservation: map[string]reservedRow{},
		reserveErr:  map[string]error{},
	}
}

func (c *fakeCatalog) GetWork(_ context.Context, id string) (domain.WorkItem, bool, error) {
	if c.workErr != nil {
		return domain.WorkItem{}, false, c.workErr
	}
	w, ok := c.works[id]
	if !ok {
		return domain.WorkItem{}, false, domain.ErrWorkNotFound
	}
	return w, !c.inactive[id], nil
}

func (c *fakeCatalog) Reserve(_ context.Context, _, supplyID string, quantity int) (domain.SupplyItem, error) {
	if err := c.reserveErr[supplyID]; err != nil {
		return domain.SupplyItem{}, err
	}
	tmpl, ok := c.supplies[supplyID]
	if !ok {
		return domain.SupplyItem{}, domain.ErrSupplyNotFound
	}
	if c.stock[supplyID] < quantity {
		return domain.SupplyItem{}, domain.ErrSupplyOutOfStock
	}
	c.stock[supplyID] -= quantity
	c.seq++
	rid := fmt.Sprintf("res-%d", c.seq)
	c.reservation[rid] = reservedRow{supplyID: supplyID, qty: quantity}
	tmpl.ReservationID = rid
	tmpl.Quantity = quantity
	return tmpl, nil
}

func (c *fakeCatalog) ReleaseReservation(_ context.Context, rid string) error {
	c.released = append(c.released, rid)
	if c.releaseErr != nil {
		return c.releaseErr
	}
	row, ok := c.reservation[rid]
	if !ok {
		return catalog.ErrReservationNotFound
	}
	c.stock[row.supplyID] += row.qty
	delete(c.reservation, rid)
	return nil
}

// stubRepo wraps the in-memory order repository and fails or overrides the chosen operations.
type stubRepo struct {
	domain.ServiceOrderRepository
	statusOverride  *domain.SERVICE_ORDER_STATUS // FindStatus answers this
	headerOverride  *domain.SERVICE_ORDER_STATUS // FindByID answers a header in this status
	insertErr       error
	addWorkErr      error
	addSupplyErr    error
	removeSupplyErr error
	listWorksErr    error
	listSuppliesErr error
	findStatusErr   error
	findErr         error
	countErr        error
	searchErr       error
	existsErr       error
}

func (r *stubRepo) Insert(ctx context.Context, so *domain.ServiceOrder) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	return r.ServiceOrderRepository.Insert(ctx, so)
}

func (r *stubRepo) FindByID(ctx context.Context, id string) (domain.ServiceOrder, error) {
	if r.findErr != nil {
		return domain.ServiceOrder{}, r.findErr
	}
	so, err := r.ServiceOrderRepository.FindByID(ctx, id)
	if err == nil && r.headerOverride != nil {
		so.Status = *r.headerOverride
	}
	return so, err
}

func (r *stubRepo) FindStatus(ctx context.Context, id string) (domain.SERVICE_ORDER_STATUS, error) {
	if r.findStatusErr != nil {
		return "", r.findStatusErr
	}
	if r.statusOverride != nil {
		return *r.statusOverride, nil
	}
	return r.ServiceOrderRepository.FindStatus(ctx, id)
}

func (r *stubRepo) Exists(ctx context.Context, c, v string) (bool, error) {
	if r.existsErr != nil {
		return false, r.existsErr
	}
	return r.ServiceOrderRepository.Exists(ctx, c, v)
}

func (r *stubRepo) Count(ctx context.Context, p *domain.ServiceOrderFilterParams) (int64, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return r.ServiceOrderRepository.Count(ctx, p)
}

func (r *stubRepo) Search(ctx context.Context, p *domain.ServiceOrderFilterParams) ([]domain.ServiceOrder, error) {
	if r.searchErr != nil {
		return nil, r.searchErr
	}
	return r.ServiceOrderRepository.Search(ctx, p)
}

func (r *stubRepo) ListWorks(ctx context.Context, id string) ([]domain.WorkItem, error) {
	if r.listWorksErr != nil {
		return nil, r.listWorksErr
	}
	return r.ServiceOrderRepository.ListWorks(ctx, id)
}

func (r *stubRepo) AddWork(ctx context.Context, id string, w domain.WorkItem) error {
	if r.addWorkErr != nil {
		return r.addWorkErr
	}
	return r.ServiceOrderRepository.AddWork(ctx, id, w)
}

func (r *stubRepo) ListSupplies(ctx context.Context, id string) ([]domain.SupplyItem, error) {
	if r.listSuppliesErr != nil {
		return nil, r.listSuppliesErr
	}
	return r.ServiceOrderRepository.ListSupplies(ctx, id)
}

func (r *stubRepo) AddSupply(ctx context.Context, id string, s domain.SupplyItem) error {
	if r.addSupplyErr != nil {
		return r.addSupplyErr
	}
	return r.ServiceOrderRepository.AddSupply(ctx, id, s)
}

func (r *stubRepo) RemoveSupply(ctx context.Context, id, supplyID string) (domain.SupplyItem, error) {
	if r.removeSupplyErr != nil {
		return domain.SupplyItem{}, r.removeSupplyErr
	}
	return r.ServiceOrderRepository.RemoveSupply(ctx, id, supplyID)
}

// fixture is a Service wired with in-memory stores and the REST fakes.
type fixture struct {
	svc       *Service
	sos       *repository.InMemory
	repo      *stubRepo
	sagas     *saga.MemoryRepository
	box       *saga.MemoryOutbox
	notifier  *notifierSpy
	customers *fakeCustomers
	catalog   *fakeCatalog
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	sos := repository.NewInMemory()
	f := &fixture{
		sos:       sos,
		repo:      &stubRepo{ServiceOrderRepository: sos},
		sagas:     saga.NewMemoryRepository(),
		box:       saga.NewMemoryOutbox(),
		notifier:  &notifierSpy{},
		customers: &fakeCustomers{customers: map[string]domain.CustomerSnapshot{}, vehicles: map[string]domain.VehicleSnapshot{}, owners: map[string]string{}},
		catalog:   newFakeCatalog(),
	}
	f.customers.customers[customerID] = domain.CustomerSnapshot{ID: customerID, UserID: customerUser, Name: "João", Email: "joao@example.com",
		Type: "PF", Document: "00000000000", Phone: "11999990000"}
	f.customers.vehicles[vehicleID] = domain.VehicleSnapshot{ID: vehicleID, LicensePlate: "ABC1D23", Brand: "Fiat", Model: "Uno", Year: 2018}
	f.customers.owners[vehicleID] = customerID

	f.catalog.works["w1"] = domain.WorkItem{ID: "w1", Name: "Troca de óleo", Description: "Óleo e filtro", UnitPrice: decimal.RequireFromString("150.00")}
	f.catalog.works["w2"] = domain.WorkItem{ID: "w2", Name: "Alinhamento", Description: "Geometria", UnitPrice: decimal.RequireFromString("80")}
	f.catalog.works["w-off"] = domain.WorkItem{ID: "w-off", Name: "Antigo", UnitPrice: decimal.RequireFromString("10")}
	f.catalog.inactive["w-off"] = true
	f.catalog.supplies["s1"] = domain.SupplyItem{ID: "s1", Name: "Filtro", Description: "Filtro de óleo", UnitPrice: decimal.RequireFromString("35.50"), Version: 3}
	f.catalog.supplies["s2"] = domain.SupplyItem{ID: "s2", Name: "Pastilha", UnitPrice: decimal.RequireFromString("120"), Version: 1}
	f.catalog.stock["s1"] = 5
	f.catalog.stock["s2"] = 1

	orch := saga.NewOrchestrator(uow.NewInMemoryUoW(), f.repo, f.sagas, f.box, f.notifier,
		saga.Config{StepTimeout: 2 * time.Minute, PaymentTimeout: 24 * time.Hour})
	f.svc = NewService(uow.NewInMemoryUoW(), f.repo, f.customers, f.catalog, orch)
	return f
}

// seed stores an order in the given status with the given works and supplies (already reserved).
func (f *fixture) seed(t *testing.T, status domain.SERVICE_ORDER_STATUS, works []domain.WorkItem, supplies []domain.SupplyItem) {
	t.Helper()
	ctx := context.Background()
	so := domain.ServiceOrder{
		ID:       soID,
		Status:   status,
		Customer: f.customers.customers[customerID],
		Vehicle:  f.customers.vehicles[vehicleID],
	}
	require.NoError(t, f.sos.Insert(ctx, &so))
	for _, w := range works {
		require.NoError(t, f.sos.AddWork(ctx, soID, w))
	}
	for _, s := range supplies {
		require.NoError(t, f.sos.AddSupply(ctx, soID, s))
	}
	require.NoError(t, f.sos.UpdateTotal(ctx, soID, domain.ComputeTotal(works, supplies)))
}

func (f *fixture) status(t *testing.T) domain.SERVICE_ORDER_STATUS {
	t.Helper()
	st, err := f.sos.FindStatus(context.Background(), soID)
	require.NoError(t, err)
	return st
}

func (f *fixture) total(t *testing.T) string {
	t.Helper()
	so, err := f.sos.FindByID(context.Background(), soID)
	require.NoError(t, err)
	return so.TotalAmount.StringFixed(2)
}

func (f *fixture) lastEventTypes() []string {
	return f.box.Types()
}

func statusPtr(s domain.SERVICE_ORDER_STATUS) *domain.SERVICE_ORDER_STATUS { return &s }

func supplyLine(id string, qty int) adapters.AddSupplyItem {
	return adapters.AddSupplyItem{ID: id, Amount: qty}
}

var errBoom = errors.New("upstream exploded")
