package service_order

import (
	"net/http"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/middleware"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes mounts the public order routes under /v1 and the internal existence check.
func RegisterRoutes(router *gin.Engine, c *Controller, internalToken string) {
	v1 := router.Group("/v1")
	attendant := auth.AttendantRoles
	mechanic := auth.MechanicRoles
	customer := auth.CustomerRoles
	attendantMechanic := auth.AttendantAndMechanicRoles

	v1.POST("/service-order", middleware.Auth(attendant...), routing.GinHandler(c.Create, http.StatusCreated))
	v1.POST("/service-order/:id/works", middleware.Auth(attendant...), routing.GinOnlyErrorHandler(c.AddWork))
	v1.DELETE("/service-order/:id/works/:serviceId", middleware.Auth(attendant...), routing.GinOnlyErrorHandler(c.DeleteWork))
	v1.PUT("/service-order/:id/received", middleware.Auth(attendant...), routing.GinOnlyErrorHandler(c.Receive))
	v1.PUT("/service-order/:id/start-diagnosis", middleware.Auth(mechanic...), routing.GinOnlyErrorHandler(c.SendToDiagnosis))
	v1.POST("/service-order/:id/supplies", middleware.Auth(mechanic...), routing.GinOnlyErrorHandler(c.AddSupplies))
	v1.DELETE("/service-order/:id/supplies/:supplyId", middleware.Auth(mechanic...), routing.GinOnlyErrorHandler(c.DeleteSupply))
	v1.PUT("/service-order/:id/send", middleware.Auth(mechanic...), routing.GinOnlyErrorHandler(c.SendToCustomerApproval))
	v1.PUT("/service-order/:id/accept", middleware.Auth(customer...), routing.GinOnlyErrorHandler(c.Accept))
	v1.PUT("/service-order/:id/reject", middleware.Auth(customer...), routing.GinOnlyErrorHandler(c.Reject))
	v1.PUT("/service-order/:id/cancel", middleware.Auth(attendantMechanic...), routing.GinOnlyErrorHandler(c.Cancel, http.StatusNoContent))
	v1.PUT("/service-order/:id/deliver", middleware.Auth(attendant...), routing.GinOnlyErrorHandler(c.Deliver))

	v1.GET("/service-order", middleware.Auth(attendantMechanic...), routing.GinHandler(c.List))
	v1.GET("/service-order/:id", middleware.Auth(attendantMechanic...), routing.GinHandler(c.GetFullByID))
	v1.GET("/service-order/:id/works", middleware.Auth(attendantMechanic...), routing.GinHandler(c.GetWorks))
	v1.GET("/service-order/:id/supplies", middleware.Auth(attendantMechanic...), routing.GinHandler(c.GetSupplies))
	v1.GET("/service-order/:id/status", routing.GinHandler(c.GetStatus))

	router.GET("/internal/service-orders/exists", middleware.InternalAuth(internalToken), routing.GinHandler(c.Exists))
}
