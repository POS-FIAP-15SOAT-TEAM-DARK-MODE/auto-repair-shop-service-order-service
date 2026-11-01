package web

import (
	"errors"
	"net/http"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	authPkg "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	jsonv2 "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/json"
	"github.com/stretchr/testify/assert"
)

var errCustom = errors.New("custom sentinel")

func TestErrorMapsDomainAndGenericErrorsToStatus(t *testing.T) {
	RegisterErrorStatus(http.StatusTeapot, errCustom)
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"registered sentinel", errCustom, http.StatusTeapot},
		{"invalid credentials", authPkg.ErrInvalidUserCredentials, http.StatusUnauthorized},
		{"bad json syntax", jsonv2.ErrJSONSyntax, http.StatusBadRequest},
		{"bad json type", jsonv2.ErrJSONType, http.StatusBadRequest},
		{"empty body", jsonv2.ErrJSONEmptyBody, http.StatusBadRequest},
		{"wrong payload format", jsonv2.ErrWrongPayloadFormat, http.StatusBadRequest},
		{"unsupported json value is a server error", jsonv2.ErrJSONUnsupportedValue, http.StatusInternalServerError},
		{"conflict", app.ErrDataConflict, http.StatusConflict},
		{"infra conflict", app.ErrInfraConflict, http.StatusConflict},
		{"data violation", app.ErrDataViolation, http.StatusUnprocessableEntity},
		{"joined errors are a bad request", errors.Join(jsonv2.ErrJSONType, errors.New("field")), http.StatusBadRequest},
		{"unknown error", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, body := Error(c.err)
			assert.Equal(t, c.status, status)
			assert.Equal(t, status, body.StatusCode)
		})
	}
}

func TestErrorBodyDependsOnStatus(t *testing.T) {
	_, notFound := Error(errors.New("customer not found"))
	assert.Equal(t, "customer not found", notFound.Error)
	assert.Nil(t, notFound.Errors)

	bad := errors.Join(errors.New("field a"), errors.New("field b"))
	status, badReq := Error(bad)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, []string{"field a", "field b"}, badReq.Errors)
	assert.Empty(t, badReq.Error)

	_, conflict := Error(app.ErrDataConflict)
	assert.Equal(t, app.ErrDataConflict.Error(), conflict.Error, "conflict reports the root message")
}

func TestUnauthorizedAndForbiddenListTheMessages(t *testing.T) {
	status, body := Error(authPkg.ErrInvalidUserCredentials)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, []string{authPkg.ErrInvalidUserCredentials.Error()}, body.Errors)
}

func TestUnwrapAllAndIsJoined(t *testing.T) {
	assert.Nil(t, unwrapAll(nil))
	assert.Equal(t, []string{"a"}, unwrapAll(errors.New("a")))
	assert.Equal(t, []string{"x", "y"}, unwrapAll(errors.Join(errors.New("x"), errors.New("y"))))
	assert.True(t, IsJoined(errors.Join(errors.New("x"))))
	assert.False(t, IsJoined(errors.New("plain")))
}

func TestRegisterErrorStatusKeepsRegistrationOrder(t *testing.T) {
	first := errors.New("first")
	RegisterErrorStatus(http.StatusAccepted, first)
	RegisterErrorStatus(http.StatusNoContent, first)
	status, _ := Error(first)
	assert.Equal(t, http.StatusAccepted, status, "the first registration wins")
}
