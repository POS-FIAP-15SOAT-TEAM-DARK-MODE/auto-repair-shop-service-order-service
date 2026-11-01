// Package customer is the REST client of the Customer service used by the Service Order.
package customer

import (
	"context"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
)

type customerDTO struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	Type        string  `json:"type"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	Document    string  `json:"document"`
	CompanyName *string `json:"company_name"`
	Phone       string  `json:"phone"`
}

type vehicleDTO struct {
	ID           string `json:"id"`
	CustomerID   string `json:"customer_id"`
	LicensePlate string `json:"license_plate"`
	Brand        string `json:"brand"`
	Model        string `json:"model"`
	Year         int    `json:"year"`
}

// Client implements the customer and vehicle lookups of the Service Order.
type Client struct {
	t *clients.Transport
}

func New(baseURL, internalToken string, timeout time.Duration) *Client {
	return &Client{t: clients.NewTransport(baseURL, internalToken, timeout)}
}

// GetCustomer returns the customer snapshot. 404 -> ErrCustomerNotFound.
func (c *Client) GetCustomer(ctx context.Context, id string) (domain.CustomerSnapshot, error) {
	var dto customerDTO
	err := c.t.Do(ctx, "GET", "/internal/customers/"+id, nil, &dto,
		clients.Mapping{NotFound: domain.ErrCustomerNotFound})
	if err != nil {
		return domain.CustomerSnapshot{}, err
	}
	snap := domain.CustomerSnapshot{
		ID: dto.ID, UserID: dto.UserID, Type: dto.Type, Name: dto.Name,
		Email: dto.Email, Document: dto.Document, Phone: dto.Phone,
	}
	if dto.CompanyName != nil {
		snap.CompanyName = *dto.CompanyName
	}
	return snap, nil
}

// GetVehicle returns the vehicle snapshot and the id of its owner. 404 -> ErrVehicleNotFound.
func (c *Client) GetVehicle(ctx context.Context, id string) (domain.VehicleSnapshot, string, error) {
	var dto vehicleDTO
	err := c.t.Do(ctx, "GET", "/internal/vehicles/"+id, nil, &dto,
		clients.Mapping{NotFound: domain.ErrVehicleNotFound})
	if err != nil {
		return domain.VehicleSnapshot{}, "", err
	}
	return domain.VehicleSnapshot{
		ID: dto.ID, LicensePlate: dto.LicensePlate, Brand: dto.Brand, Model: dto.Model, Year: dto.Year,
	}, dto.CustomerID, nil
}
