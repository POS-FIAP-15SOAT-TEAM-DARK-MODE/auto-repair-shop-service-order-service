package service_order

import (
	"net/http"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/web"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
)

// HTTP status of the service order errors (same as the monolith, plus the saga and upstream ones).
func init() {
	web.RegisterErrorStatus(http.StatusNotFound,
		domain.ErrCustomerNotFound, domain.ErrVehicleNotFound, domain.ErrWorkNotFound,
		domain.ErrSupplyNotFound, domain.ErrServiceOrderNotFound,
		domain.ErrServiceOrderWorkNotFound, domain.ErrServiceOrderSupplyNotFound)
	web.RegisterErrorStatus(http.StatusForbidden, domain.ErrInvalidCustomerProperty)
	web.RegisterValidationErrors(
		domain.ErrInvalidServiceOrderId, domain.ErrInvalidWorkId, domain.ErrInvalidSupplyID,
		domain.ErrInvalidSupplyAmount, domain.ErrEmptyServicesList)
	web.RegisterErrorStatus(http.StatusConflict,
		domain.ErrServiceOrderNotNew, domain.ErrServiceOrderNotInReceived, domain.ErrServiceOrderNotInDiagnosis,
		domain.ErrServiceOrderNotAwaitingApproval, domain.ErrServiceOrderNotCancelable,
		domain.ErrServiceOrderNotCompleted, domain.ErrServiceOrderInvalidTransition,
		domain.ErrSagaAlreadyRunning, domain.ErrSagaNotAwaitingCustomer)
	web.RegisterErrorStatus(http.StatusUnprocessableEntity, domain.ErrSupplyOutOfStock, domain.ErrUpstreamValidation)
	web.RegisterErrorStatus(http.StatusBadGateway, domain.ErrUpstreamUnavailable)
}
