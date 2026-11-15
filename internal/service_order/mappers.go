package service_order

import (
	"fmt"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/adapters"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
)

// workStatusActive is the only status a work can have in an order (only active works are accepted).
const workStatusActive = "ACTIVE"

func mapWorksToResponse(works []domain.WorkItem) []adapters.WorkItemResponse {
	if len(works) == 0 {
		return nil
	}
	items := make([]adapters.WorkItemResponse, 0, len(works))
	for _, w := range works {
		items = append(items, adapters.WorkItemResponse{
			ID:          w.ID,
			Name:        w.Name,
			Description: w.Description,
			Price:       w.UnitPrice.String(),
			Status:      workStatusActive,
		})
	}
	return items
}

func mapSuppliesToResponse(supplies []domain.SupplyItem) []adapters.SupplyItemResponse {
	if len(supplies) == 0 {
		return nil
	}
	items := make([]adapters.SupplyItemResponse, 0, len(supplies))
	for _, s := range supplies {
		items = append(items, adapters.SupplyItemResponse{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Price:       s.UnitPrice.String(),
			Version:     s.Version,
			Amount:      s.Quantity,
		})
	}
	return items
}

func mapVehicle(v domain.VehicleSnapshot) adapters.VehicleResponse {
	return adapters.VehicleResponse{
		ID:           v.ID,
		BrandModel:   fmt.Sprintf("%s - %s", v.Brand, v.Model),
		Year:         v.Year,
		LicensePlate: v.LicensePlate,
	}
}

func mapCustomer(c domain.CustomerSnapshot) adapters.CustomerResponse {
	return adapters.CustomerResponse{
		ID:          c.ID,
		Name:        c.Name,
		Email:       c.Email,
		Type:        c.Type,
		Document:    c.Document,
		CompanyName: c.CompanyName,
		Phone:       c.Phone,
	}
}

func mapSOToListItem(so domain.ServiceOrder) adapters.SOListItemResponse {
	return adapters.SOListItemResponse{
		ID:         so.ID,
		Status:     so.Status.String(),
		TotalValue: so.TotalAmount.InexactFloat64(),
		Customer:   mapCustomer(so.Customer),
		Vehicle:    mapVehicle(so.Vehicle),
	}
}

func mapSOToDetailResponse(so domain.ServiceOrder) adapters.SODetailResponse {
	return adapters.SODetailResponse{
		ID:         so.ID,
		Status:     so.Status.String(),
		TotalValue: so.TotalAmount.InexactFloat64(),
		Customer:   mapCustomer(so.Customer),
		Vehicle:    mapVehicle(so.Vehicle),
		Works:      mapWorksToResponse(so.Works),
		Supplies:   mapSuppliesToResponse(so.Supplies),
	}
}
