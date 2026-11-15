package clients_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	catalogClient "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients/catalog"
	customerClient "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients/customer"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const token = "internal-token"

func server(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCustomerClient(t *testing.T) {
	ctx := context.Background()
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/customers/c1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1", "user_id": "u1", "type": "COMPANY",
				"name": "Oficina", "email": "x@y.z", "document": "12345678000199", "company_name": "Oficina LTDA", "phone": "11"})
		case "/internal/customers/c2":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "c2", "user_id": "u2", "type": "INDIVIDUAL",
				"name": "João", "email": "j@y.z", "document": "52998224725", "company_name": nil, "phone": "11"})
		case "/internal/vehicles/v1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "v1", "customer_id": "c1", "license_plate": "ABC1D23",
				"brand": "Fiat", "model": "Uno", "year": 2018})
		case "/internal/vehicles/bad":
			w.WriteHeader(http.StatusUnprocessableEntity)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c := customerClient.New(srv.URL, token, time.Second)

	cust, err := c.GetCustomer(ctx, "c1")
	require.NoError(t, err)
	assert.Equal(t, "u1", cust.UserID)
	assert.Equal(t, "Oficina LTDA", cust.CompanyName)

	cust2, err := c.GetCustomer(ctx, "c2")
	require.NoError(t, err)
	assert.Empty(t, cust2.CompanyName)

	_, err = c.GetCustomer(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrCustomerNotFound)

	veh, owner, err := c.GetVehicle(ctx, "v1")
	require.NoError(t, err)
	assert.Equal(t, "c1", owner)
	assert.Equal(t, "ABC1D23", veh.LicensePlate)

	_, _, err = c.GetVehicle(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrVehicleNotFound)

	_, _, err = c.GetVehicle(ctx, "bad")
	assert.ErrorIs(t, err, domain.ErrUpstreamValidation)
}

func TestCustomerClientRejectsBadToken(t *testing.T) {
	srv := server(t, func(http.ResponseWriter, *http.Request) {})
	c := customerClient.New(srv.URL, "wrong", time.Second)
	_, err := c.GetCustomer(context.Background(), "c1")
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable, "a 401 means misconfigured internal token")
}

func TestCatalogClientWork(t *testing.T) {
	ctx := context.Background()
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/works/w1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "w1", "name": "Troca", "description": "d", "unit_price": "150.00", "active": true})
		case "/internal/works/bad-price":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "w2", "unit_price": "abc"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c := catalogClient.New(srv.URL, token, time.Second)

	w, active, err := c.GetWork(ctx, "w1")
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, "150.00", w.UnitPrice.StringFixed(2))

	_, _, err = c.GetWork(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrWorkNotFound)

	_, _, err = c.GetWork(ctx, "bad-price")
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable)
}

func TestCatalogReserveStatusMapping(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		status int
		body   any
		want   error
	}{
		"created":      {http.StatusCreated, map[string]any{"reservation_id": "r1", "supply_id": "s1", "name": "Filtro", "unit_price": "35.50", "version": 3}, nil},
		"out of stock": {http.StatusConflict, nil, domain.ErrSupplyOutOfStock},
		"no supply":    {http.StatusNotFound, nil, domain.ErrSupplyNotFound},
		"invalid":      {http.StatusUnprocessableEntity, nil, domain.ErrUpstreamValidation},
		"server error": {http.StatusInternalServerError, nil, domain.ErrUpstreamUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := server(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				w.WriteHeader(tc.status)
				if tc.body != nil {
					_ = json.NewEncoder(w).Encode(tc.body)
				}
			})
			c := catalogClient.New(srv.URL, token, time.Second)
			item, err := c.Reserve(ctx, "so1", "s1", 2)
			if tc.want == nil {
				require.NoError(t, err)
				assert.Equal(t, "r1", item.ReservationID)
				assert.Equal(t, 2, item.Quantity)
				assert.Equal(t, "35.50", item.UnitPrice.StringFixed(2))
				return
			}
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestCatalogGetRetriesOnceAndPostDoesNot(t *testing.T) {
	ctx := context.Background()
	var gets, posts atomic.Int32
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if gets.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "w1", "unit_price": "1", "active": true})
			return
		}
		posts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	c := catalogClient.New(srv.URL, token, time.Second)

	_, _, err := c.GetWork(ctx, "w1")
	require.NoError(t, err, "GET is retried once")
	assert.Equal(t, int32(2), gets.Load())

	_, err = c.Reserve(ctx, "so1", "s1", 1)
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable)
	assert.Equal(t, int32(1), posts.Load(), "POST is never retried")
}

func TestCatalogTimeoutIsUpstreamUnavailable(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	})
	c := catalogClient.New(srv.URL, token, 20*time.Millisecond)
	err := c.ReleaseReservation(context.Background(), "r1")
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable)
}

func TestCatalogReleaseReservation(t *testing.T) {
	ctx := context.Background()
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/reservations/r1":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c := catalogClient.New(srv.URL, token, time.Second)
	assert.NoError(t, c.ReleaseReservation(ctx, "r1"))
	assert.ErrorIs(t, c.ReleaseReservation(ctx, "gone"), catalogClient.ErrReservationNotFound)
}
