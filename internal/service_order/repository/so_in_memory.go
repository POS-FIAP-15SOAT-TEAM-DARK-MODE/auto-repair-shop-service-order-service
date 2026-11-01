package repository

import (
	"context"
	"sort"
	"sync"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/shopspring/decimal"
)

// InMemory is a thread-safe ServiceOrderRepository used by unit tests.
type InMemory struct {
	mu       sync.Mutex
	orders   map[string]domain.ServiceOrder
	works    map[string][]domain.WorkItem
	supplies map[string][]domain.SupplyItem
}

func NewInMemory() *InMemory {
	return &InMemory{
		orders:   map[string]domain.ServiceOrder{},
		works:    map[string][]domain.WorkItem{},
		supplies: map[string][]domain.SupplyItem{},
	}
}

func header(so domain.ServiceOrder) domain.ServiceOrder {
	so.Works = nil
	so.Supplies = nil
	return so
}

func (r *InMemory) Insert(_ context.Context, so *domain.ServiceOrder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orders[so.ID] = header(*so)
	return nil
}

func (r *InMemory) Update(_ context.Context, so *domain.ServiceOrder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orders[so.ID]; !ok {
		return domain.ErrServiceOrderNotFound
	}
	r.orders[so.ID] = header(*so)
	return nil
}

func (r *InMemory) FindByID(_ context.Context, id string) (domain.ServiceOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	so, ok := r.orders[id]
	if !ok {
		return domain.ServiceOrder{}, domain.ErrServiceOrderNotFound
	}
	return so, nil
}

func (r *InMemory) FindStatus(_ context.Context, id string) (domain.SERVICE_ORDER_STATUS, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	so, ok := r.orders[id]
	if !ok {
		return "", domain.ErrServiceOrderNotFound
	}
	return so.Status, nil
}

func (r *InMemory) Exists(_ context.Context, customerID, vehicleID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, so := range r.orders {
		if so.Customer.ID == customerID || so.Vehicle.ID == vehicleID {
			return true, nil
		}
	}
	return false, nil
}

func (r *InMemory) Search(_ context.Context, params *domain.ServiceOrderFilterParams) ([]domain.ServiceOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	matched := r.filter(params)
	if params.Limit > 0 && int64(len(matched)) > params.Offset {
		end := params.Offset + params.Limit
		if end > int64(len(matched)) {
			end = int64(len(matched))
		}
		matched = matched[params.Offset:end]
	} else if params.Limit > 0 {
		matched = nil
	}
	return matched, nil
}

func (r *InMemory) Count(_ context.Context, params *domain.ServiceOrderFilterParams) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.filter(params))), nil
}

// filter returns the matching orders sorted by id; must be called with the lock held.
func (r *InMemory) filter(params *domain.ServiceOrderFilterParams) []domain.ServiceOrder {
	terminal := map[string]bool{"DELIVERED": true, "COMPLETED": true, "REJECTED": true, "CANCELLED": true}
	var out []domain.ServiceOrder
	for _, so := range r.orders {
		if params.Status == "" && terminal[so.Status.String()] {
			continue
		}
		if params.Status != "" && so.Status.String() != params.Status {
			continue
		}
		if params.CustomerID != "" && so.Customer.ID != params.CustomerID {
			continue
		}
		if params.VehicleID != "" && so.Vehicle.ID != params.VehicleID {
			continue
		}
		out = append(out, so)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *InMemory) ListWorks(_ context.Context, id string) ([]domain.WorkItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.WorkItem(nil), r.works[id]...), nil
}

func (r *InMemory) AddWork(_ context.Context, id string, w domain.WorkItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.works[id] {
		if existing.ID == w.ID {
			return nil
		}
	}
	r.works[id] = append(r.works[id], w)
	return nil
}

func (r *InMemory) RemoveWork(_ context.Context, id, workID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, w := range r.works[id] {
		if w.ID == workID {
			r.works[id] = append(r.works[id][:i], r.works[id][i+1:]...)
			return nil
		}
	}
	return domain.ErrServiceOrderWorkNotFound
}

func (r *InMemory) ListSupplies(_ context.Context, id string) ([]domain.SupplyItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.SupplyItem(nil), r.supplies[id]...), nil
}

func (r *InMemory) AddSupply(_ context.Context, id string, s domain.SupplyItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.supplies[id] = append(r.supplies[id], s)
	return nil
}

func (r *InMemory) RemoveSupply(_ context.Context, id, supplyID string) (domain.SupplyItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, s := range r.supplies[id] {
		if s.ID == supplyID {
			r.supplies[id] = append(r.supplies[id][:i], r.supplies[id][i+1:]...)
			return s, nil
		}
	}
	return domain.SupplyItem{}, domain.ErrServiceOrderSupplyNotFound
}

func (r *InMemory) UpdateTotal(_ context.Context, id string, total decimal.Decimal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	so, ok := r.orders[id]
	if !ok {
		return domain.ErrServiceOrderNotFound
	}
	so.TotalAmount = total
	r.orders[id] = so
	return nil
}
