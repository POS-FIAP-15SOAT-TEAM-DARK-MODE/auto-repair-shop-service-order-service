package routing

import (
	"context"
	"net/http"

	api "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/docs"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/web"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

// SetupRouter builds the gin engine with the common middlewares, the ops endpoints and the
// service routes mounted by register.
func SetupRouter(m *container.Middlewares, register func(router *gin.Engine)) *gin.Engine {
	router := gin.New()

	for name, mid := range *m {
		logger.Global().Info("middleware.registered", zap.String("name", name))
		router.Use(mid)
	}

	mountSwaggerUI(router)

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	if register != nil {
		register(router)
	}
	return router
}

// GinHandler adapts a service call that returns a body. The route params are passed in the context.
func GinHandler[T any](cb func(context.Context, *http.Request) (T, error), customStatus ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		for _, v := range c.Params {
			ctx = context.WithValue(ctx, v.Key, v.Value) //nolint:staticcheck
		}

		resp, err := cb(ctx, c.Request)
		if err != nil {
			writeError(ctx, c, err)
			return
		}

		if len(customStatus) == 0 {
			c.JSON(http.StatusOK, resp)
			return
		}
		c.JSON(customStatus[0], resp)
	}
}

// GinOnlyErrorHandler adapts a service call without body; the default success status is 204.
func GinOnlyErrorHandler(cb func(context.Context, *http.Request) error, customStatus ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		for _, v := range c.Params {
			ctx = context.WithValue(ctx, v.Key, v.Value) //nolint:staticcheck
		}

		if err := cb(ctx, c.Request); err != nil {
			writeError(ctx, c, err)
			return
		}

		if len(customStatus) == 0 {
			c.Status(http.StatusNoContent)
			c.Writer.WriteHeaderNow()
			return
		}
		c.Status(customStatus[0])
		c.Writer.WriteHeaderNow()
	}
}

// writeError answers err with the public body and logs the full cause (with request_id) when the
// error is server-side (5xx, or an infrastructure 409/422).
func writeError(ctx context.Context, c *gin.Context, err error) {
	status, body, logIt := web.Describe(err)
	if logIt {
		logger.Of(ctx).Error(err, zap.Int("status", status))
	}
	c.JSON(status, body)
}

func mountSwaggerUI(router *gin.Engine) {
	router.GET("/swagger.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", api.SwaggerBytes)
	})

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger.yaml")))
}
