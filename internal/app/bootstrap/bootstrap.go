package bootstrap

import (
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/env"
)

// Servers returns the HTTP API. Relay and consumer workers are added with the saga.
func Servers() []app.Server {
	router := routing.SetupRouter(middlewaresContainer(), nil)
	return []app.Server{app.NewHTTPServer("http-api", ":"+env.GetString("PORT", "8086"), router)}
}

func middlewaresContainer() *container.Middlewares {
	return &container.Middlewares{
		"Logger":       middleware.Logger(),
		"Recovery":     middleware.Recovery(),
		"Metrics":      middleware.Metrics(),
		"ErrorHandler": middleware.ErrorHandler(),
	}
}
