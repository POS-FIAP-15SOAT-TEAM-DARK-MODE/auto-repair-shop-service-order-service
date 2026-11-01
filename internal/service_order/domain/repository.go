package domain

import (
	"context"

	"github.com/shopspring/decimal"
)

// ServiceOrderRepository persists the order aggregate (header, works and supplies).
type ServiceOrderRepository interface {
	// Insert creates the header row.
	Insert(ctx context.Context, so *ServiceOrder) error
	// Update writes status, total and timestamps of an existing header.
	Update(ctx context.Context, so *ServiceOrder) error
	// FindByID loads the header with the lock needed for a state change; it does not load items.
	FindByID(ctx context.Context, id string) (ServiceOrder, error)
	// FindStatus returns the current status without locking.
	FindStatus(ctx context.Context, id string) (SERVICE_ORDER_STATUS, error)
	// Exists reports whether any order (in any status) belongs to the customer or the vehicle.
	Exists(ctx context.Context, customerID, vehicleID string) (bool, error)
	Search(ctx context.Context, params *ServiceOrderFilterParams) ([]ServiceOrder, error)
	Count(ctx context.Context, params *ServiceOrderFilterParams) (int64, error)
	ListWorks(ctx context.Context, serviceOrderID string) ([]WorkItem, error)
	AddWork(ctx context.Context, serviceOrderID string, w WorkItem) error
	RemoveWork(ctx context.Context, serviceOrderID, workID string) error
	ListSupplies(ctx context.Context, serviceOrderID string) ([]SupplyItem, error)
	AddSupply(ctx context.Context, serviceOrderID string, s SupplyItem) error
	// RemoveSupply deletes the link and returns the removed item (reservation id).
	RemoveSupply(ctx context.Context, serviceOrderID, supplyID string) (SupplyItem, error)
	UpdateTotal(ctx context.Context, serviceOrderID string, total decimal.Decimal) error
}

// LoadAggregate reads the order header together with its works and supplies.
// The header is read with the lock used for state changes (see FindByID).
func LoadAggregate(ctx context.Context, repo ServiceOrderRepository, id string) (ServiceOrder, error) {
	so, err := repo.FindByID(ctx, id)
	if err != nil {
		return ServiceOrder{}, err
	}
	if so.Works, err = repo.ListWorks(ctx, id); err != nil {
		return ServiceOrder{}, err
	}
	if so.Supplies, err = repo.ListSupplies(ctx, id); err != nil {
		return ServiceOrder{}, err
	}
	return so, nil
}
