//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// customerStub answers GET /internal/customers/{id} and GET /internal/vehicles/{id} as the
// Customer service does (contracts/internal-rest.md).
type customerStub struct {
	mu        sync.Mutex
	customers map[string]customerRow
	vehicles  map[string]vehicleRow
}

type customerRow struct {
	ID       string  `json:"id"`
	UserID   string  `json:"user_id"`
	Type     string  `json:"type"`
	Name     string  `json:"name"`
	Email    string  `json:"email"`
	Document string  `json:"document"`
	Company  *string `json:"company_name"`
	Phone    string  `json:"phone"`
}

type vehicleRow struct {
	ID           string `json:"id"`
	CustomerID   string `json:"customer_id"`
	LicensePlate string `json:"license_plate"`
	Brand        string `json:"brand"`
	Model        string `json:"model"`
	Year         int    `json:"year"`
}

func newCustomerStub() *customerStub {
	return &customerStub{customers: map[string]customerRow{}, vehicles: map[string]vehicleRow{}}
}

func (c *customerStub) addCustomer(row customerRow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.customers[row.ID] = row
}

func (c *customerStub) addVehicle(row vehicleRow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vehicles[row.ID] = row
}

func (c *customerStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/customers/", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/internal/customers/")
		c.mu.Lock()
		row, ok := c.customers[id]
		c.mu.Unlock()
		writeStub(w, ok, row)
	})
	mux.HandleFunc("/internal/vehicles/", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/internal/vehicles/")
		c.mu.Lock()
		row, ok := c.vehicles[id]
		c.mu.Unlock()
		writeStub(w, ok, row)
	})
	return mux
}

// catalogStub answers the work lookup and the stock reservations of the Catalog service.
type catalogStub struct {
	mu           sync.Mutex
	works        map[string]workRow
	supplies     map[string]supplyRow
	reservations map[string]string // reservation id -> supply id
}

type workRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UnitPrice   string `json:"unit_price"`
	Active      bool   `json:"active"`
}

type supplyRow struct {
	ID          string `json:"supply_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UnitPrice   string `json:"unit_price"`
	Version     int    `json:"version"`
}

func newCatalogStub() *catalogStub {
	return &catalogStub{works: map[string]workRow{}, supplies: map[string]supplyRow{}, reservations: map[string]string{}}
}

func (c *catalogStub) addWork(row workRow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.works[row.ID] = row
}

func (c *catalogStub) addSupply(row supplyRow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.supplies[row.ID] = row
}

func (c *catalogStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/works/", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/internal/works/")
		c.mu.Lock()
		row, ok := c.works[id]
		c.mu.Unlock()
		writeStub(w, ok, row)
	})
	mux.HandleFunc("/internal/reservations", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) || r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SupplyID string `json:"supply_id"`
			Quantity int    `json:"quantity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Quantity <= 0 {
			writeStub(w, false, nil)
			return
		}
		c.mu.Lock()
		supply, ok := c.supplies[req.SupplyID]
		resID := uuid.NewString()
		if ok {
			c.reservations[resID] = req.SupplyID
		}
		c.mu.Unlock()
		if !ok {
			writeStub(w, false, nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"reservation_id": resID, "supply_id": supply.ID, "name": supply.Name,
			"description": supply.Description, "unit_price": supply.UnitPrice, "version": supply.Version,
		})
	})
	mux.HandleFunc("/internal/reservations/", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) || r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/internal/reservations/")
		c.mu.Lock()
		_, ok := c.reservations[id]
		delete(c.reservations, id)
		c.mu.Unlock()
		if !ok {
			writeStub(w, false, nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// authorized checks the internal token as the real services do.
func authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Internal-Token") != internalToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func writeStub(w http.ResponseWriter, found bool, body any) {
	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"errors":["not found"]}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
