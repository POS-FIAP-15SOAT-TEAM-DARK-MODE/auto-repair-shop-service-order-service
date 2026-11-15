package domain

import (
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/shopspring/decimal"
)

type SERVICE_ORDER_STATUS string

const (
	SERVICE_ORDER_STATUS_NEW               SERVICE_ORDER_STATUS = "NEW"
	SERVICE_ORDER_STATUS_RECEIVED          SERVICE_ORDER_STATUS = "RECEIVED"
	SERVICE_ORDER_STATUS_IN_DIAGNOSIS      SERVICE_ORDER_STATUS = "IN_DIAGNOSIS"
	SERVICE_ORDER_STATUS_AWAITING_APPROVAL SERVICE_ORDER_STATUS = "AWAITING_APPROVAL"
	SERVICE_ORDER_STATUS_IN_PROGRESS       SERVICE_ORDER_STATUS = "IN_PROGRESS"
	SERVICE_ORDER_STATUS_COMPLETED         SERVICE_ORDER_STATUS = "COMPLETED"
	SERVICE_ORDER_STATUS_DELIVERED         SERVICE_ORDER_STATUS = "DELIVERED"
	SERVICE_ORDER_STATUS_REJECTED          SERVICE_ORDER_STATUS = "REJECTED"
	SERVICE_ORDER_STATUS_CANCELLED         SERVICE_ORDER_STATUS = "CANCELLED"
)

// Reasons carried by ServiceOrderStatusChanged.reason (contracts/messaging.md).
const (
	ReasonBudgetRejected  = "BUDGET_REJECTED"
	ReasonPaymentRejected = "PAYMENT_REJECTED"
	ReasonUserCancelled   = "USER_CANCELLED"
	ReasonExecutionFailed = "EXECUTION_FAILED"
	ReasonSagaTimeout     = "SAGA_TIMEOUT"
)

// allowedTransitions is the status graph of the service order. IN_PROGRESS -> REJECTED and
// IN_PROGRESS -> CANCELLED are reachable only through the saga (see internal/saga).
var allowedTransitions = map[SERVICE_ORDER_STATUS][]SERVICE_ORDER_STATUS{
	SERVICE_ORDER_STATUS_NEW:               {SERVICE_ORDER_STATUS_RECEIVED, SERVICE_ORDER_STATUS_CANCELLED},
	SERVICE_ORDER_STATUS_RECEIVED:          {SERVICE_ORDER_STATUS_IN_DIAGNOSIS, SERVICE_ORDER_STATUS_CANCELLED},
	SERVICE_ORDER_STATUS_IN_DIAGNOSIS:      {SERVICE_ORDER_STATUS_AWAITING_APPROVAL, SERVICE_ORDER_STATUS_CANCELLED},
	SERVICE_ORDER_STATUS_AWAITING_APPROVAL: {SERVICE_ORDER_STATUS_IN_PROGRESS, SERVICE_ORDER_STATUS_REJECTED, SERVICE_ORDER_STATUS_CANCELLED},
	SERVICE_ORDER_STATUS_IN_PROGRESS:       {SERVICE_ORDER_STATUS_COMPLETED, SERVICE_ORDER_STATUS_REJECTED, SERVICE_ORDER_STATUS_CANCELLED},
	SERVICE_ORDER_STATUS_COMPLETED:         {SERVICE_ORDER_STATUS_DELIVERED},
}

func (s SERVICE_ORDER_STATUS) String() string {
	return string(s)
}

// IsCancelable reports whether a user may cancel an order in this status:
// any non-terminal status before COMPLETED.
func (s SERVICE_ORDER_STATUS) IsCancelable() bool {
	return s != SERVICE_ORDER_STATUS_REJECTED &&
		s != SERVICE_ORDER_STATUS_COMPLETED &&
		s != SERVICE_ORDER_STATUS_DELIVERED &&
		s != SERVICE_ORDER_STATUS_CANCELLED
}

// CanTransitionTo reports whether the status graph has an edge from s to next.
func (s SERVICE_ORDER_STATUS) CanTransitionTo(next SERVICE_ORDER_STATUS) bool {
	for _, t := range allowedTransitions[s] {
		if t == next {
			return true
		}
	}
	return false
}

func StringToServiceOrderStatus(val string) SERVICE_ORDER_STATUS {
	return SERVICE_ORDER_STATUS(val)
}

// CustomerSnapshot is the customer data copied into the order (no cross-service queries later).
type CustomerSnapshot struct {
	ID          string
	UserID      string
	Name        string
	Email       string
	Type        string
	Document    string
	CompanyName string
	Phone       string
}

// VehicleSnapshot is the vehicle data copied into the order.
type VehicleSnapshot struct {
	ID           string
	LicensePlate string
	Brand        string
	Model        string
	Year         int
}

// WorkItem is a work linked to the order. ID is the work id in the Catalog.
type WorkItem struct {
	ID          string
	Name        string
	Description string
	UnitPrice   decimal.Decimal
}

// SupplyItem is a supply linked to the order, with the reservation made in the Catalog.
type SupplyItem struct {
	ID            string
	ReservationID string
	Name          string
	Description   string
	UnitPrice     decimal.Decimal
	Version       int
	Quantity      int
}

type ServiceOrder struct {
	ID          string
	Status      SERVICE_ORDER_STATUS
	Customer    CustomerSnapshot
	Vehicle     VehicleSnapshot
	Works       []WorkItem
	Supplies    []SupplyItem
	TotalAmount decimal.Decimal
	QuoteSentAt *time.Time
	CompletedAt *time.Time
	DeliveredAt *time.Time
}

type ServiceOrderFilterParams struct {
	Page       int64
	PageSize   int64
	Limit      int64
	Offset     int64
	Status     string
	CustomerID string
	VehicleID  string
	SortBy     string
	SortOrder  string
}

// NewServiceOrder creates an order in NEW with a fresh ULID.
func NewServiceOrder(customer CustomerSnapshot, vehicle VehicleSnapshot) *ServiceOrder {
	return &ServiceOrder{
		ID:          ulid.Make().String(),
		Status:      SERVICE_ORDER_STATUS_NEW,
		Customer:    customer,
		Vehicle:     vehicle,
		TotalAmount: decimal.Zero,
	}
}

// Transition moves the order to next and returns the previous status. Illegal edges fail with
// ErrServiceOrderInvalidTransition.
func (so *ServiceOrder) Transition(next SERVICE_ORDER_STATUS) (SERVICE_ORDER_STATUS, error) {
	prev := so.Status
	if !prev.CanTransitionTo(next) {
		return prev, ErrServiceOrderInvalidTransition
	}
	so.Status = next
	return prev, nil
}

// RecalculateTotal sets TotalAmount = sum(work.unit_price) + sum(supply.unit_price * quantity).
func (so *ServiceOrder) RecalculateTotal() {
	so.TotalAmount = ComputeTotal(so.Works, so.Supplies)
}

// ComputeTotal applies the pricing rule of the order to the given items.
func ComputeTotal(works []WorkItem, supplies []SupplyItem) decimal.Decimal {
	total := decimal.Zero
	for _, w := range works {
		total = total.Add(w.UnitPrice)
	}
	for _, s := range supplies {
		total = total.Add(s.UnitPrice.Mul(decimal.NewFromInt(int64(s.Quantity))))
	}
	return total
}

// WorkIDs returns the work ids linked to the order.
func (so *ServiceOrder) WorkIDs() []string {
	ids := make([]string, 0, len(so.Works))
	for _, w := range so.Works {
		ids = append(ids, w.ID)
	}
	return ids
}
