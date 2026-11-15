package service_order

import (
	"net/http"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/web"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/stretchr/testify/assert"
)

// Input-validation sentinels of the service must answer 400 (never 500).
func TestValidationErrorsAnswerBadRequest(t *testing.T) {
	validation := []error{
		domain.ErrInvalidServiceOrderId,
		domain.ErrInvalidWorkId,
		domain.ErrInvalidSupplyID,
		domain.ErrInvalidSupplyAmount,
		domain.ErrEmptyServicesList,
	}
	for _, err := range validation {
		t.Run(err.Error(), func(t *testing.T) {
			status, body := web.Error(err)
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Equal(t, http.StatusBadRequest, body.StatusCode)
			assert.Equal(t, []string{err.Error()}, body.Errors)
		})
	}
}
