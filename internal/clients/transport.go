// Package clients holds the REST clients used by the Service Order to call Customer and Catalog
// on the internal /internal/* routes.
package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"go.uber.org/zap"
)

const (
	internalTokenHeader = "X-Internal-Token"
	requestIDHeader     = "X-Request-Id"
	DefaultTimeout      = 3 * time.Second
)

// Mapping tells the transport which domain error a 404 or 409 response becomes for a given call.
type Mapping struct {
	NotFound error
	Conflict error
}

// Transport performs internal REST calls with the shared token and a 3 s timeout. Only GET
// requests are retried (once); writes are never retried, the caller decides.
type Transport struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewTransport(baseURL, token string, timeout time.Duration) *Transport {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Transport{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}
}

// Do sends one request. A non-nil out decodes the 2xx JSON body. Status mapping:
// 404 -> m.NotFound, 409 -> m.Conflict, 422 -> ErrUpstreamValidation,
// 5xx and transport failures -> ErrUpstreamUnavailable.
func (t *Transport) Do(ctx context.Context, method, path string, body, out any, m Mapping) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
	}

	attempts := 1
	if method == http.MethodGet {
		attempts = 2
	}

	var lastErr = domain.ErrUpstreamUnavailable
	for attempt := 0; attempt < attempts; attempt++ {
		status, data, err := t.send(ctx, method, path, raw)
		if err != nil {
			logger.Of(ctx).Warn("internal call failed",
				zap.String("method", method), zap.String("path", path), zap.Error(err))
			continue
		}
		if status >= 500 {
			logger.Of(ctx).Warn("internal call returned 5xx",
				zap.String("method", method), zap.String("path", path), zap.Int("status", status))
			continue
		}
		return mapStatus(status, data, out, m)
	}
	return lastErr
}

func (t *Transport) send(ctx context.Context, method, path string, raw []byte) (int, []byte, error) {
	var body io.Reader
	if raw != nil {
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set(internalTokenHeader, t.token)
	if raw != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id := logger.RequestID(ctx); id != "" {
		req.Header.Set(requestIDHeader, id)
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, data, nil
}

func mapStatus(status int, data []byte, out any, m Mapping) error {
	switch {
	case status >= 200 && status < 300:
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return errors.Join(domain.ErrUpstreamUnavailable, err)
			}
		}
		return nil
	case status == http.StatusNotFound:
		if m.NotFound != nil {
			return m.NotFound
		}
		return domain.ErrUpstreamValidation
	case status == http.StatusConflict:
		if m.Conflict != nil {
			return m.Conflict
		}
		return domain.ErrUpstreamValidation
	case status == http.StatusUnprocessableEntity:
		return domain.ErrUpstreamValidation
	default:
		return domain.ErrUpstreamUnavailable
	}
}
