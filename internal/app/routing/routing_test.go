package routing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/container"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestGinHandlerWritesBodyWithDefaultAndCustomStatus(t *testing.T) {
	r := gin.New()
	r.GET("/ok", GinHandler(func(context.Context, *http.Request) (map[string]string, error) {
		return map[string]string{"id": "1"}, nil
	}))
	r.POST("/created", GinHandler(func(context.Context, *http.Request) (map[string]string, error) {
		return map[string]string{"id": "2"}, nil
	}, http.StatusCreated))

	rec := do(r, http.MethodGet, "/ok")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"1"}`, rec.Body.String())

	rec = do(r, http.MethodPost, "/created")
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"id":"2"}`, rec.Body.String())
}

func TestGinHandlerMapsErrorsAndPassesRouteParams(t *testing.T) {
	r := gin.New()
	r.GET("/orders/:id", GinHandler(func(ctx context.Context, _ *http.Request) (string, error) {
		id, _ := ctx.Value("id").(string)
		if id == "missing" {
			return "", app.ErrDataConflict
		}
		return id, nil
	}))

	rec := do(r, http.MethodGet, "/orders/42")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `"42"`, rec.Body.String(), "route params are available in the context")

	rec = do(r, http.MethodGet, "/orders/missing")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":409`)
}

func TestGinOnlyErrorHandlerStatuses(t *testing.T) {
	r := gin.New()
	r.PUT("/a", GinOnlyErrorHandler(func(context.Context, *http.Request) error { return nil }))
	r.PUT("/b", GinOnlyErrorHandler(func(context.Context, *http.Request) error { return nil }, http.StatusAccepted))
	r.PUT("/c", GinOnlyErrorHandler(func(context.Context, *http.Request) error { return errors.New("boom") }))
	r.PUT("/d/:id", GinOnlyErrorHandler(func(ctx context.Context, _ *http.Request) error {
		if ctx.Value("id") != "7" {
			return errors.New("wrong param")
		}
		return nil
	}))

	assert.Equal(t, http.StatusNoContent, do(r, http.MethodPut, "/a").Code)
	assert.Equal(t, http.StatusAccepted, do(r, http.MethodPut, "/b").Code)
	assert.Equal(t, http.StatusInternalServerError, do(r, http.MethodPut, "/c").Code)
	assert.Equal(t, http.StatusNoContent, do(r, http.MethodPut, "/d/7").Code)
}

func TestSetupRouterMountsOpsEndpointsAndMiddlewares(t *testing.T) {
	called := false
	mids := container.Middlewares{"marker": func(c *gin.Context) {
		c.Header("X-Marker", "yes")
		c.Next()
	}}
	router := SetupRouter(&mids, func(r *gin.Engine) {
		r.GET("/custom", func(c *gin.Context) {
			called = true
			c.Status(http.StatusOK)
		})
	})
	require.NotNil(t, router)

	rec := do(router, http.MethodGet, "/ping")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"message":"pong"}`, rec.Body.String())
	assert.Equal(t, "yes", rec.Header().Get("X-Marker"))

	assert.Equal(t, http.StatusOK, do(router, http.MethodGet, "/metrics").Code)
	assert.Equal(t, http.StatusOK, do(router, http.MethodGet, "/swagger.yaml").Code)
	assert.Equal(t, http.StatusOK, do(router, http.MethodGet, "/custom").Code)
	assert.True(t, called)

	// register may be nil
	assert.Equal(t, http.StatusOK, do(SetupRouter(&mids, nil), http.MethodGet, "/ping").Code)
}

func do(r http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}
