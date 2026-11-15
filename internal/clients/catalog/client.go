// Package catalog is the REST client of the Catalog service used by the Service Order.
package catalog

import (
	"context"
	"errors"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/shopspring/decimal"
)

// ErrReservationNotFound means the reservation id never existed in the Catalog.
var ErrReservationNotFound = errors.New("reservation not found")

type workDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UnitPrice   string `json:"unit_price"`
	Active      bool   `json:"active"`
}

type reservationDTO struct {
	ReservationID string `json:"reservation_id"`
	SupplyID      string `json:"supply_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	UnitPrice     string `json:"unit_price"`
	Version       int    `json:"version"`
}

type reserveRequest struct {
	ServiceOrderID string `json:"service_order_id"`
	SupplyID       string `json:"supply_id"`
	Quantity       int    `json:"quantity"`
}

// Client implements the work lookup and the stock reservations of the Service Order.
type Client struct {
	t *clients.Transport
}

func New(baseURL, internalToken string, timeout time.Duration) *Client {
	return &Client{t: clients.NewTransport(baseURL, internalToken, timeout)}
}

// GetWork returns the work and whether it is active. 404 -> ErrWorkNotFound.
func (c *Client) GetWork(ctx context.Context, id string) (domain.WorkItem, bool, error) {
	var dto workDTO
	err := c.t.Do(ctx, "GET", "/internal/works/"+id, nil, &dto,
		clients.Mapping{NotFound: domain.ErrWorkNotFound})
	if err != nil {
		return domain.WorkItem{}, false, err
	}
	price, err := decimal.NewFromString(dto.UnitPrice)
	if err != nil {
		return domain.WorkItem{}, false, errors.Join(domain.ErrUpstreamUnavailable, err)
	}
	return domain.WorkItem{ID: dto.ID, Name: dto.Name, Description: dto.Description, UnitPrice: price}, dto.Active, nil
}

// Reserve takes quantity units of the supply for the order. 409 -> ErrSupplyOutOfStock,
// 404 -> ErrSupplyNotFound, 422 -> ErrUpstreamValidation. Never retried.
func (c *Client) Reserve(ctx context.Context, serviceOrderID, supplyID string, quantity int) (domain.SupplyItem, error) {
	var dto reservationDTO
	err := c.t.Do(ctx, "POST", "/internal/reservations",
		reserveRequest{ServiceOrderID: serviceOrderID, SupplyID: supplyID, Quantity: quantity}, &dto,
		clients.Mapping{NotFound: domain.ErrSupplyNotFound, Conflict: domain.ErrSupplyOutOfStock})
	if err != nil {
		return domain.SupplyItem{}, err
	}
	price, err := decimal.NewFromString(dto.UnitPrice)
	if err != nil {
		return domain.SupplyItem{}, errors.Join(domain.ErrUpstreamUnavailable, err)
	}
	return domain.SupplyItem{
		ID: dto.SupplyID, ReservationID: dto.ReservationID, Name: dto.Name, Description: dto.Description,
		UnitPrice: price, Version: dto.Version, Quantity: quantity,
	}, nil
}

// ReleaseReservation gives the reserved stock back. A reservation that does not exist returns
// ErrReservationNotFound, which callers treat as already released.
func (c *Client) ReleaseReservation(ctx context.Context, reservationID string) error {
	return c.t.Do(ctx, "DELETE", "/internal/reservations/"+reservationID, nil, nil,
		clients.Mapping{NotFound: ErrReservationNotFound})
}
