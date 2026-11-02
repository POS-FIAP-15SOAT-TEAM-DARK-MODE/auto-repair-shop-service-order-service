package domain

import "errors"

// Errors of the upstream services, mapped to HTTP status by the REST clients and the controller.
var (
	ErrCustomerNotFound        = errors.New("customer not found")
	ErrVehicleNotFound         = errors.New("vehicle not found")
	ErrInvalidCustomerProperty = errors.New("customer does not own this resource")
	ErrWorkNotFound            = errors.New("work not found")
	ErrInvalidWorkId           = errors.New("invalid work id")
	ErrSupplyNotFound          = errors.New("supply not found")
	ErrSupplyOutOfStock        = errors.New("supply out of stock")
	ErrInvalidSupplyID         = errors.New("invalid supply id")
	ErrInvalidSupplyAmount     = errors.New("supply amount must be greater than zero")
	ErrUpstreamValidation      = errors.New("upstream rejected the request as invalid")
	ErrUpstreamUnavailable     = errors.New("upstream unavailable")
)

// Service order errors.
var (
	ErrInvalidServiceOrderId           = errors.New("invalid service order id")
	ErrServiceOrderNotFound            = errors.New("service order not found")
	ErrServiceOrderNotNew              = errors.New("service order isn't at NEW state")
	ErrServiceOrderNotInReceived       = errors.New("service order isn't in RECEIVED state")
	ErrServiceOrderNotInDiagnosis      = errors.New("service order isn't at IN_DIAGNOSIS state")
	ErrServiceOrderNotAwaitingApproval = errors.New("service order isn't at AWAITING_APPROVAL state")
	ErrServiceOrderNotCancelable       = errors.New("service order state isn't cancelable")
	ErrServiceOrderNotCompleted        = errors.New("service order isn't at COMPLETED state")
	ErrServiceOrderInvalidTransition   = errors.New("invalid service order status transition")
	ErrServiceOrderWorkNotFound        = errors.New("work is not linked to this service order")
	ErrServiceOrderSupplyNotFound      = errors.New("supply is not linked to this service order")
	ErrEmptyServicesList               = errors.New("services list cannot be empty")
)

// Saga errors.
var (
	ErrSagaAlreadyRunning      = errors.New("service order already has a running approval saga")
	ErrSagaNotAwaitingCustomer = errors.New("service order approval is not awaiting the customer")
)
