package service_order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients/catalog"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/saga"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/adapters"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// CustomerGateway is the REST view of Customer used to snapshot the order.
type CustomerGateway interface {
	GetCustomer(ctx context.Context, id string) (domain.CustomerSnapshot, error)
	GetVehicle(ctx context.Context, id string) (domain.VehicleSnapshot, string, error)
}

// CatalogGateway is the REST view of Catalog: works lookup and stock reservations.
type CatalogGateway interface {
	GetWork(ctx context.Context, id string) (domain.WorkItem, bool, error)
	Reserve(ctx context.Context, serviceOrderID, supplyID string, quantity int) (domain.SupplyItem, error)
	ReleaseReservation(ctx context.Context, reservationID string) error
}

// Service implements the local flows of the order (SPEC 3.1 and the table of the F6 task).
// Saga operations are delegated to the orchestrator.
type Service struct {
	uow       uow.Executor
	repo      domain.ServiceOrderRepository
	customers CustomerGateway
	catalog   CatalogGateway
	saga      *saga.Orchestrator
}

func NewService(u uow.Executor, repo domain.ServiceOrderRepository, customers CustomerGateway, cat CatalogGateway, orch *saga.Orchestrator) *Service {
	return &Service{uow: u, repo: repo, customers: customers, catalog: cat, saga: orch}
}

func (s *Service) logValidationError(ctx context.Context, operation string, err error) {
	logger.Of(ctx).Warn("service_order.validation_failed",
		zap.String("operation", operation),
		zap.String("entity", "service_order"),
		zap.Error(err),
	)
}

// Create snapshots the customer, vehicle and works, reserves the supplies and stores the order as
// NEW in one transaction. Reservations are released when the order cannot be stored.
func (s *Service) Create(ctx context.Context, req adapters.CreateSORequest) (adapters.SOResponse, error) {
	customer, err := s.customers.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		return adapters.SOResponse{}, err
	}
	vehicle, ownerID, err := s.customers.GetVehicle(ctx, req.VehicleID)
	if err != nil {
		return adapters.SOResponse{}, err
	}
	if ownerID != customer.ID {
		return adapters.SOResponse{}, domain.ErrInvalidCustomerProperty
	}

	works, err := s.resolveWorks(ctx, req.WorkIDs)
	if err != nil {
		return adapters.SOResponse{}, err
	}
	if err := validateSupplies(req.Supplies); err != nil {
		s.logValidationError(ctx, "create_service_order", err)
		return adapters.SOResponse{}, err
	}

	so := domain.NewServiceOrder(customer, vehicle)
	so.Works = works
	reserved, err := s.reserveAll(ctx, so.ID, req.Supplies)
	if err != nil {
		return adapters.SOResponse{}, err
	}
	so.Supplies = reserved
	so.RecalculateTotal()

	err = s.uow.Execute(ctx, func(ctx context.Context) error {
		if err := s.repo.Insert(ctx, so); err != nil {
			return err
		}
		for _, w := range so.Works {
			if err := s.repo.AddWork(ctx, so.ID, w); err != nil {
				return err
			}
		}
		for _, sup := range so.Supplies {
			if err := s.repo.AddSupply(ctx, so.ID, sup); err != nil {
				return err
			}
		}
		return s.saga.Status().Created(ctx, so, saga.Meta{RequestID: logger.RequestID(ctx)})
	})
	if err != nil {
		s.releaseAll(ctx, reserved)
		return adapters.SOResponse{}, err
	}

	logger.Of(ctx).Info("service_order.created",
		zap.String("operation", "create_service_order"),
		zap.String("entity", "service_order"),
		zap.String("service_order_id", so.ID),
	)
	return adapters.SOResponse{ID: so.ID, Status: so.Status.String()}, nil
}

// resolveWorks loads the works by id; each one must exist and be active.
func (s *Service) resolveWorks(ctx context.Context, rawIDs []string) ([]domain.WorkItem, error) {
	seen := map[string]bool{}
	var works []domain.WorkItem
	for _, raw := range rawIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, domain.ErrInvalidWorkId
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		w, active, err := s.catalog.GetWork(ctx, id)
		if err != nil {
			return nil, err
		}
		if !active {
			return nil, domain.ErrWorkNotFound
		}
		works = append(works, w)
	}
	return works, nil
}

func validateSupplies(items []adapters.AddSupplyItem) error {
	for _, sup := range items {
		if strings.TrimSpace(sup.ID) == "" {
			return domain.ErrInvalidSupplyID
		}
		if sup.Amount <= 0 {
			return domain.ErrInvalidSupplyAmount
		}
	}
	return nil
}

// reserveAll reserves each supply; on the first failure the reservations already made are released.
func (s *Service) reserveAll(ctx context.Context, soID string, items []adapters.AddSupplyItem) ([]domain.SupplyItem, error) {
	var reserved []domain.SupplyItem
	for _, sup := range items {
		item, err := s.catalog.Reserve(ctx, soID, strings.TrimSpace(sup.ID), sup.Amount)
		if err != nil {
			s.releaseAll(ctx, reserved)
			return nil, err
		}
		reserved = append(reserved, item)
	}
	return reserved, nil
}

// releaseAll gives the stock back, best effort. A missing reservation counts as released.
func (s *Service) releaseAll(ctx context.Context, items []domain.SupplyItem) {
	for _, item := range items {
		if err := s.catalog.ReleaseReservation(ctx, item.ReservationID); err != nil && !errors.Is(err, catalog.ErrReservationNotFound) {
			logger.Of(ctx).Warn("service_order.release_reservation_failed",
				zap.String("reservation_id", item.ReservationID), zap.Error(err))
		}
	}
}

func (s *Service) List(ctx context.Context, params adapters.SOFilterParams) (adapters.PaginatedSOResponse, error) {
	domainParams := &domain.ServiceOrderFilterParams{
		Page:       params.Page,
		PageSize:   params.PageSize,
		Limit:      params.PageSize,
		Offset:     (params.Page - 1) * params.PageSize,
		Status:     params.Status,
		CustomerID: params.CustomerID,
		VehicleID:  params.VehicleID,
		SortBy:     params.SortBy,
		SortOrder:  params.SortOrder,
	}

	var total int64
	var items []domain.ServiceOrder

	eg := errgroup.Group{}
	eg.Go(func() error {
		t, err := s.repo.Count(ctx, domainParams)
		total = t
		return err
	})
	eg.Go(func() error {
		list, err := s.repo.Search(ctx, domainParams)
		items = list
		return err
	})
	if err := eg.Wait(); err != nil {
		return adapters.PaginatedSOResponse{}, fmt.Errorf("fail to list service orders: %w", err)
	}

	listItems := make([]adapters.SOListItemResponse, 0, len(items))
	for _, so := range items {
		listItems = append(listItems, mapSOToListItem(so))
	}
	return adapters.PaginatedSOResponse{
		Items:      listItems,
		TotalItems: total,
		TotalPages: int64(math.Ceil(float64(total) / float64(params.PageSize))),
		Page:       params.Page,
		PageSize:   params.PageSize,
	}, nil
}

func (s *Service) GetWorks(ctx context.Context, soID string) (adapters.ListResponse[adapters.WorkItemResponse], error) {
	if _, err := s.repo.FindStatus(ctx, soID); err != nil {
		return adapters.ListResponse[adapters.WorkItemResponse]{}, err
	}
	works, err := s.repo.ListWorks(ctx, soID)
	if err != nil {
		return adapters.ListResponse[adapters.WorkItemResponse]{}, err
	}
	items := mapWorksToResponse(works)
	return pageOf(items), nil
}

// AddWorks links works to a NEW order and recalculates the total.
func (s *Service) AddWorks(ctx context.Context, soID string, workIDs []string) error {
	if len(workIDs) == 0 {
		s.logValidationError(ctx, "add_works_to_service_order", domain.ErrEmptyServicesList)
		return domain.ErrEmptyServicesList
	}
	status, err := s.repo.FindStatus(ctx, soID)
	if err != nil {
		return err
	}
	if status != domain.SERVICE_ORDER_STATUS_NEW {
		return domain.ErrServiceOrderNotNew
	}
	works, err := s.resolveWorks(ctx, workIDs)
	if err != nil {
		return err
	}

	return s.uow.Execute(ctx, func(ctx context.Context) error {
		so, err := s.repo.FindByID(ctx, soID)
		if err != nil {
			return err
		}
		if so.Status != domain.SERVICE_ORDER_STATUS_NEW {
			return domain.ErrServiceOrderNotNew
		}
		for _, w := range works {
			if err := s.repo.AddWork(ctx, soID, w); err != nil {
				return err
			}
		}
		return s.recalculate(ctx, soID)
	})
}

// RemoveWork unlinks a work from a NEW order and recalculates the total.
func (s *Service) RemoveWork(ctx context.Context, soID, workID string) error {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return domain.ErrInvalidWorkId
	}
	return s.uow.Execute(ctx, func(ctx context.Context) error {
		so, err := s.repo.FindByID(ctx, soID)
		if err != nil {
			return err
		}
		if so.Status != domain.SERVICE_ORDER_STATUS_NEW {
			return domain.ErrServiceOrderNotNew
		}
		if err := s.repo.RemoveWork(ctx, soID, workID); err != nil {
			return err
		}
		return s.recalculate(ctx, soID)
	})
}

func (s *Service) GetSupplies(ctx context.Context, soID string) (adapters.ListResponse[adapters.SupplyItemResponse], error) {
	if _, err := s.repo.FindStatus(ctx, soID); err != nil {
		return adapters.ListResponse[adapters.SupplyItemResponse]{}, err
	}
	supplies, err := s.repo.ListSupplies(ctx, soID)
	if err != nil {
		return adapters.ListResponse[adapters.SupplyItemResponse]{}, err
	}
	return pageOf(mapSuppliesToResponse(supplies)), nil
}

// AddSupplies reserves the stock of each supply and links it to an IN_DIAGNOSIS order.
func (s *Service) AddSupplies(ctx context.Context, soID string, items []adapters.AddSupplyItem) error {
	if len(items) == 0 {
		s.logValidationError(ctx, "add_supplies_to_service_order", domain.ErrEmptyServicesList)
		return domain.ErrEmptyServicesList
	}
	if err := validateSupplies(items); err != nil {
		s.logValidationError(ctx, "add_supplies_to_service_order", err)
		return err
	}
	status, err := s.repo.FindStatus(ctx, soID)
	if err != nil {
		return err
	}
	if status != domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS {
		return domain.ErrServiceOrderNotInDiagnosis
	}

	reserved, err := s.reserveAll(ctx, soID, items)
	if err != nil {
		return err
	}
	err = s.uow.Execute(ctx, func(ctx context.Context) error {
		so, err := s.repo.FindByID(ctx, soID)
		if err != nil {
			return err
		}
		if so.Status != domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS {
			return domain.ErrServiceOrderNotInDiagnosis
		}
		for _, sup := range reserved {
			if err := s.repo.AddSupply(ctx, soID, sup); err != nil {
				return err
			}
		}
		return s.recalculate(ctx, soID)
	})
	if err != nil {
		s.releaseAll(ctx, reserved)
		return err
	}
	return nil
}

// RemoveSupply releases the reservation of the supply in the Catalog and unlinks it from an
// IN_DIAGNOSIS order. A failure to release leaves the order unchanged.
func (s *Service) RemoveSupply(ctx context.Context, soID, supplyID string) error {
	supplyID = strings.TrimSpace(supplyID)
	if supplyID == "" {
		return domain.ErrInvalidSupplyID
	}
	return s.uow.Execute(ctx, func(ctx context.Context) error {
		so, err := s.repo.FindByID(ctx, soID)
		if err != nil {
			return err
		}
		if so.Status != domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS {
			return domain.ErrServiceOrderNotInDiagnosis
		}
		supplies, err := s.repo.ListSupplies(ctx, soID)
		if err != nil {
			return err
		}
		var link *domain.SupplyItem
		for i := range supplies {
			if supplies[i].ID == supplyID {
				link = &supplies[i]
			}
		}
		if link == nil {
			return domain.ErrServiceOrderSupplyNotFound
		}
		if err := s.catalog.ReleaseReservation(ctx, link.ReservationID); err != nil && !errors.Is(err, catalog.ErrReservationNotFound) {
			return err
		}
		if _, err := s.repo.RemoveSupply(ctx, soID, supplyID); err != nil {
			return err
		}
		return s.recalculate(ctx, soID)
	})
}

// recalculate sets the total from the current works and supplies, in the same transaction.
func (s *Service) recalculate(ctx context.Context, soID string) error {
	so, err := domain.LoadAggregate(ctx, s.repo, soID)
	if err != nil {
		return err
	}
	so.RecalculateTotal()
	return s.repo.UpdateTotal(ctx, soID, so.TotalAmount)
}

// Receive moves a NEW order to RECEIVED.
func (s *Service) Receive(ctx context.Context, soID string) error {
	return s.localTransition(ctx, soID, domain.SERVICE_ORDER_STATUS_NEW, domain.ErrServiceOrderNotNew,
		domain.SERVICE_ORDER_STATUS_RECEIVED)
}

// StartDiagnosis moves a RECEIVED order to IN_DIAGNOSIS.
func (s *Service) StartDiagnosis(ctx context.Context, soID string) error {
	return s.localTransition(ctx, soID, domain.SERVICE_ORDER_STATUS_RECEIVED, domain.ErrServiceOrderNotInReceived,
		domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS)
}

// Deliver moves a COMPLETED order to DELIVERED.
func (s *Service) Deliver(ctx context.Context, soID string) error {
	return s.localTransition(ctx, soID, domain.SERVICE_ORDER_STATUS_COMPLETED, domain.ErrServiceOrderNotCompleted,
		domain.SERVICE_ORDER_STATUS_DELIVERED)
}

func (s *Service) localTransition(ctx context.Context, soID string, from domain.SERVICE_ORDER_STATUS, notFrom error, to domain.SERVICE_ORDER_STATUS) error {
	return s.uow.Execute(ctx, func(ctx context.Context) error {
		so, err := s.repo.FindByID(ctx, soID)
		if err != nil {
			return err
		}
		if so.Status != from {
			return notFrom
		}
		return s.saga.Status().Change(ctx, &so, to, "", saga.Meta{RequestID: logger.RequestID(ctx)})
	})
}

// Send starts the approval saga (SPEC 3.2 step 1).
func (s *Service) Send(ctx context.Context, soID string) error {
	return s.saga.Send(ctx, soID, logger.RequestID(ctx))
}

// Accept lets the customer approve the budget (JWT user id must own the order).
func (s *Service) Accept(ctx context.Context, soID, userID string) error {
	return s.saga.Accept(ctx, soID, userID, logger.RequestID(ctx))
}

// Reject lets the customer refuse the budget.
func (s *Service) Reject(ctx context.Context, soID, userID string) error {
	return s.saga.Reject(ctx, soID, userID, logger.RequestID(ctx))
}

// Cancel cancels the order and compensates the saga, if any.
func (s *Service) Cancel(ctx context.Context, soID string) error {
	return s.saga.Cancel(ctx, soID, logger.RequestID(ctx))
}

// ExpireDeadlines is run by the deadline worker.
func (s *Service) ExpireDeadlines(ctx context.Context) (int, error) {
	return s.saga.ExpireDeadlines(ctx, 50)
}

// GetFullByID returns the order with its works and supplies, from the local database only.
func (s *Service) GetFullByID(ctx context.Context, soID string) (adapters.SODetailResponse, error) {
	so, err := domain.LoadAggregate(ctx, s.repo, soID)
	if err != nil {
		return adapters.SODetailResponse{}, err
	}
	return mapSOToDetailResponse(so), nil
}

func (s *Service) GetStatus(ctx context.Context, soID string) (adapters.SOResponse, error) {
	status, err := s.repo.FindStatus(ctx, soID)
	if err != nil {
		return adapters.SOResponse{}, err
	}
	return adapters.SOResponse{ID: soID, Status: status.String()}, nil
}

// Exists reports whether the customer or the vehicle has any order (internal check).
func (s *Service) Exists(ctx context.Context, customerID, vehicleID string) (bool, error) {
	return s.repo.Exists(ctx, customerID, vehicleID)
}

func pageOf[T any](items []T) adapters.ListResponse[T] {
	n := int64(len(items))
	return adapters.ListResponse[T]{Items: items, TotalItems: n, TotalPages: 1, PageSize: n, Page: 1}
}
