package catalog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, "tok", 500*time.Millisecond)
}

func TestGetWorkReturnsActiveFlag(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/works/w1", r.URL.Path)
		assert.Equal(t, "tok", r.Header.Get("X-Internal-Token"))
		_, _ = w.Write([]byte(`{"id":"w1","name":"Troca","description":"Óleo","unit_price":"150.00","active":true}`))
	})
	w, active, err := c.GetWork(context.Background(), "w1")
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, "w1", w.ID)
	assert.Equal(t, "Óleo", w.Description)
	assert.Equal(t, "150", w.UnitPrice.String())
}

func TestGetWorkInactiveAndErrors(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"w1","unit_price":"1","active":false}`))
	})
	_, active, err := c.GetWork(context.Background(), "w1")
	require.NoError(t, err)
	assert.False(t, active)

	c = serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	_, active, err = c.GetWork(context.Background(), "w9")
	assert.ErrorIs(t, err, domain.ErrWorkNotFound)
	assert.False(t, active)

	c = serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"w1","unit_price":"not-a-number","active":true}`))
	})
	_, _, err = c.GetWork(context.Background(), "w1")
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable)
}

func TestReserveSendsRequestAndMapsReservation(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/reservations", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, map[string]any{"service_order_id": "so-1", "supply_id": "s1", "quantity": float64(2)}, got)
		_, _ = w.Write([]byte(`{"reservation_id":"r-9","supply_id":"s1","name":"Filtro","description":"d","unit_price":"35.50","version":3}`))
	})
	item, err := c.Reserve(context.Background(), "so-1", "s1", 2)
	require.NoError(t, err)
	assert.Equal(t, "r-9", item.ReservationID)
	assert.Equal(t, "s1", item.ID)
	assert.Equal(t, 2, item.Quantity)
	assert.Equal(t, 3, item.Version)
	assert.Equal(t, "35.5", item.UnitPrice.String())
}

func TestReserveErrorsAreNotRetried(t *testing.T) {
	calls := 0
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := c.Reserve(context.Background(), "so-1", "s1", 1)
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable)
	assert.Equal(t, 1, calls, "POST is never retried")

	cases := map[int]error{
		http.StatusConflict:            domain.ErrSupplyOutOfStock,
		http.StatusNotFound:            domain.ErrSupplyNotFound,
		http.StatusUnprocessableEntity: domain.ErrUpstreamValidation,
	}
	for status, want := range cases {
		c := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		_, err := c.Reserve(context.Background(), "so-1", "s1", 1)
		assert.ErrorIs(t, err, want, "status %d", status)
	}
}

func TestReserveWithBadPriceIsUpstreamUnavailable(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"reservation_id":"r","unit_price":"x"}`))
	})
	_, err := c.Reserve(context.Background(), "so-1", "s1", 1)
	assert.ErrorIs(t, err, domain.ErrUpstreamUnavailable)
}

func TestReleaseReservation(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/internal/reservations/r-9", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})
	require.NoError(t, c.ReleaseReservation(context.Background(), "r-9"))

	c = serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	assert.ErrorIs(t, c.ReleaseReservation(context.Background(), "gone"), ErrReservationNotFound)

	c = serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	assert.ErrorIs(t, c.ReleaseReservation(context.Background(), "r"), domain.ErrUpstreamUnavailable)
}
