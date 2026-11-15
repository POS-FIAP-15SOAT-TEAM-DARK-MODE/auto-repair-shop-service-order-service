package service_order

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	pkgAuth "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/json"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/adapters"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"go.uber.org/zap"
)

const (
	soIDParam      = "id"
	serviceIDParam = "serviceId"
	supplyIDParam  = "supplyId"
)

// Controller maps HTTP requests to the service. It holds no business rules.
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller {
	return &Controller{svc: svc}
}

func idFrom(ctx context.Context, key string) string {
	v, _ := ctx.Value(key).(string)
	return strings.TrimSpace(v)
}

func (c *Controller) Create(ctx context.Context, req *http.Request) (adapters.SOResponse, error) {
	var dto adapters.CreateSORequest
	if err := json.ParseJsonBodyToStruct(req.Body, &dto); err != nil {
		logger.Of(ctx).Debug("failed to bind service order creation payload", zap.Error(err))
		return adapters.SOResponse{}, err
	}
	return c.svc.Create(ctx, dto)
}

func (c *Controller) List(ctx context.Context, req *http.Request) (adapters.PaginatedSOResponse, error) {
	q := req.URL.Query()
	var page, pageSize int64 = 1, 10
	if v, err := strconv.Atoi(q.Get("page")); err == nil && v > 0 {
		page = int64(v)
	}
	if v, err := strconv.Atoi(q.Get("pageSize")); err == nil && v > 0 {
		pageSize = int64(v)
	}
	return c.svc.List(ctx, adapters.SOFilterParams{
		Page:       page,
		PageSize:   pageSize,
		Status:     q.Get("status"),
		CustomerID: q.Get("customerId"),
		VehicleID:  q.Get("vehicleId"),
		SortBy:     q.Get("sort_by"),
		SortOrder:  q.Get("sort_order"),
	})
}

func (c *Controller) GetWorks(ctx context.Context, _ *http.Request) (adapters.ListResponse[adapters.WorkItemResponse], error) {
	id := idFrom(ctx, soIDParam)
	if id == "" {
		return adapters.ListResponse[adapters.WorkItemResponse]{}, domain.ErrInvalidServiceOrderId
	}
	return c.svc.GetWorks(ctx, id)
}

func (c *Controller) AddWork(ctx context.Context, req *http.Request) error {
	id := idFrom(ctx, soIDParam)
	if id == "" {
		return domain.ErrInvalidServiceOrderId
	}
	var dto adapters.AddWorksRequest
	if err := json.ParseJsonBodyToStruct(req.Body, &dto); err != nil {
		return err
	}
	return c.svc.AddWorks(ctx, id, dto.WorkIDs)
}

func (c *Controller) DeleteWork(ctx context.Context, _ *http.Request) error {
	id := idFrom(ctx, soIDParam)
	workID := idFrom(ctx, serviceIDParam)
	if id == "" {
		return domain.ErrInvalidServiceOrderId
	}
	return c.svc.RemoveWork(ctx, id, workID)
}

func (c *Controller) GetSupplies(ctx context.Context, _ *http.Request) (adapters.ListResponse[adapters.SupplyItemResponse], error) {
	id := idFrom(ctx, soIDParam)
	if id == "" {
		return adapters.ListResponse[adapters.SupplyItemResponse]{}, domain.ErrInvalidServiceOrderId
	}
	return c.svc.GetSupplies(ctx, id)
}

func (c *Controller) AddSupplies(ctx context.Context, req *http.Request) error {
	id := idFrom(ctx, soIDParam)
	if id == "" {
		return domain.ErrInvalidServiceOrderId
	}
	var dto adapters.AddSuppliesRequest
	if err := json.ParseJsonBodyToStruct(req.Body, &dto); err != nil {
		return err
	}
	return c.svc.AddSupplies(ctx, id, dto.Supplies)
}

func (c *Controller) DeleteSupply(ctx context.Context, _ *http.Request) error {
	id := idFrom(ctx, soIDParam)
	if id == "" {
		return domain.ErrInvalidServiceOrderId
	}
	return c.svc.RemoveSupply(ctx, id, idFrom(ctx, supplyIDParam))
}

func (c *Controller) Receive(ctx context.Context, _ *http.Request) error {
	return c.svc.Receive(ctx, idFrom(ctx, soIDParam))
}

func (c *Controller) SendToDiagnosis(ctx context.Context, _ *http.Request) error {
	return c.svc.StartDiagnosis(ctx, idFrom(ctx, soIDParam))
}

func (c *Controller) SendToCustomerApproval(ctx context.Context, _ *http.Request) error {
	return c.svc.Send(ctx, idFrom(ctx, soIDParam))
}

func (c *Controller) Accept(ctx context.Context, req *http.Request) error {
	claims, err := pkgAuth.GetClaimsFromHeader(req)
	if err != nil {
		return err
	}
	return c.svc.Accept(ctx, idFrom(ctx, soIDParam), claims.UserId)
}

func (c *Controller) Reject(ctx context.Context, req *http.Request) error {
	claims, err := pkgAuth.GetClaimsFromHeader(req)
	if err != nil {
		return err
	}
	return c.svc.Reject(ctx, idFrom(ctx, soIDParam), claims.UserId)
}

func (c *Controller) Cancel(ctx context.Context, _ *http.Request) error {
	return c.svc.Cancel(ctx, idFrom(ctx, soIDParam))
}

func (c *Controller) Deliver(ctx context.Context, _ *http.Request) error {
	return c.svc.Deliver(ctx, idFrom(ctx, soIDParam))
}

func (c *Controller) GetFullByID(ctx context.Context, _ *http.Request) (adapters.SODetailResponse, error) {
	id := idFrom(ctx, soIDParam)
	if id == "" {
		return adapters.SODetailResponse{}, domain.ErrInvalidServiceOrderId
	}
	return c.svc.GetFullByID(ctx, id)
}

func (c *Controller) GetStatus(ctx context.Context, _ *http.Request) (adapters.SOResponse, error) {
	return c.svc.GetStatus(ctx, idFrom(ctx, soIDParam))
}

type existsResponse struct {
	Exists bool `json:"exists"`
}

// Exists answers the internal check used by Customer and Vehicle before a delete.
// Exactly one of customer_id or vehicle_id must be given (422 otherwise).
func (c *Controller) Exists(ctx context.Context, req *http.Request) (existsResponse, error) {
	customerID := strings.TrimSpace(req.URL.Query().Get("customer_id"))
	vehicleID := strings.TrimSpace(req.URL.Query().Get("vehicle_id"))
	if (customerID == "") == (vehicleID == "") {
		return existsResponse{}, app.ErrDataViolation
	}
	found, err := c.svc.Exists(ctx, customerID, vehicleID)
	if err != nil {
		return existsResponse{}, err
	}
	return existsResponse{Exists: found}, nil
}
