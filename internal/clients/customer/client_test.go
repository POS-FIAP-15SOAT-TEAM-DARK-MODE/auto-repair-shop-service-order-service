package customer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, h http.HandlerFunc) (*Client, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "tok", 500*time.Millisecond), &calls
}

func TestGetCustomerMapsTheSnapshot(t *testing.T) {
	c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/customers/c1", r.URL.Path)
		assert.Equal(t, "tok", r.Header.Get("X-Internal-Token"))
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1", "user_id": "u1", "type": "PJ", "name": "Oficina",
			"email": "o@x.com", "document": "00000000000000", "company_name": "Oficina LTDA", "phone": "11"})
	})
	snap, err := c.GetCustomer(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, domain.CustomerSnapshot{ID: "c1", UserID: "u1", Type: "PJ", Name: "Oficina", Email: "o@x.com",
		Document: "00000000000000", CompanyName: "Oficina LTDA", Phone: "11"}, snap)
}

func TestGetCustomerWithoutCompanyName(t *testing.T) {
	c, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","company_name":null}`))
	})
	snap, err := c.GetCustomer(context.Background(), "c1")
	require.NoError(t, err)
	assert.Empty(t, snap.CompanyName)
}

func TestGetCustomerErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		want   error
	}{
		"not found":     {http.StatusNotFound, domain.ErrCustomerNotFound},
		"unprocessable": {http.StatusUnprocessableEntity, domain.ErrUpstreamValidation},
		"server error":  {http.StatusInternalServerError, domain.ErrUpstreamUnavailable},
		"unauthorized":  {http.StatusUnauthorized, domain.ErrUpstreamUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
			_, err := c.GetCustomer(context.Background(), "c1")
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestGetCustomerRetriesGetOnceOnServerError(t *testing.T) {
	calls := 0
	c, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"id":"c1"}`))
	})
	snap, err := c.GetCustomer(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, "c1", snap.ID)
	assert.Equal(t, 2, calls)
}

func TestGetCustomerTimeoutIsUpstreamUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "tok", 50*time.Millisecond)
	_, err := c.GetCustomer(context.Background(), "c1")
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable)
}

func TestGetVehicleReturnsOwner(t *testing.T) {
	c, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/vehicles/v1", r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"v1","customer_id":"c1","license_plate":"ABC1D23","brand":"Fiat","model":"Uno","year":2018}`))
	})
	snap, owner, err := c.GetVehicle(context.Background(), "v1")
	require.NoError(t, err)
	assert.Equal(t, "c1", owner)
	assert.Equal(t, domain.VehicleSnapshot{ID: "v1", LicensePlate: "ABC1D23", Brand: "Fiat", Model: "Uno", Year: 2018}, snap)
}

func TestGetVehicleErrors(t *testing.T) {
	c, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	_, owner, err := c.GetVehicle(context.Background(), "v9")
	assert.ErrorIs(t, err, domain.ErrVehicleNotFound)
	assert.Empty(t, owner)

	c, _ = serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	_, _, err = c.GetVehicle(context.Background(), "v1")
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable)
}
