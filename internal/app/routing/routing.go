package routing

import (
	"net/http"

	api "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/docs"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

// SetupRouter builds the gin engine with the common middlewares, ops endpoints and the
// service routes registered by the given hook.
func SetupRouter(m *container.Middlewares, register func(v1 *gin.RouterGroup)) *gin.Engine {
	router := gin.New()

	for name, mid := range *m {
		logger.Global().Info("Starting new middleware", zap.String("name", name))
		router.Use(mid)
	}

	mountSwaggerUI(router)

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := router.Group("/v1")
	if register != nil {
		register(v1)
	}
	return router
}

func mountSwaggerUI(router *gin.Engine) {
	router.GET("/swagger.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", api.SwaggerBytes)
	})

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger.yaml")))
}
