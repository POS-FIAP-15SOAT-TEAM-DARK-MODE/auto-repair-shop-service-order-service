package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	authPkg "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	jsonv2 "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errCustom       = errors.New("custom sentinel")
	errNotFound     = errors.New("thing not found")
	errDomainBad    = errors.New("domain rule broke")
	errDomainNotFnd = errors.New("domain missing")
)

// sensitive simulates a driver/infrastructure message that must never reach a response body.
const sensitive = `pq: duplicate key value violates unique constraint "uq_users_cpf" (value 123.456.789-00)`

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
		{"joined validation errors are a bad request", errors.Join(jsonv2.ErrJSONType, errors.New("field")), http.StatusBadRequest},
		{"joined with an unknown cause is a server error", errors.Join(errors.New("field"), errors.New("boom")), http.StatusInternalServerError},
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

// Infrastructure errors must never leak their cause into the body, whatever the status.
func TestInfrastructureErrorsDoNotLeakCause(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{"unique violation (409)", app.ErrDataConflict, http.StatusConflict, "conflict"},
		{"infra conflict (409)", app.ErrInfraConflict, http.StatusConflict, "conflict"},
		{"data violation (422)", app.ErrDataViolation, http.StatusUnprocessableEntity, "invalid data"},
		{"wrapped data violation (422)", fmt.Errorf("save: %w", app.ErrDataViolation), http.StatusUnprocessableEntity, "invalid data"},
		{"unknown driver error (500)", errors.New(sensitive), http.StatusInternalServerError, "internal server error"},
		{"wrapped driver error (500)", fmt.Errorf("save: %w", errors.New(sensitive)), http.StatusInternalServerError, "internal server error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, body := Error(c.err)
			require.Equal(t, c.wantStatus, status)

			raw, err := json.Marshal(body)
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "uq_users_cpf")
			assert.NotContains(t, string(raw), "123.456.789-00")
			assert.NotContains(t, string(raw), "pq:")
			assert.Equal(t, status, body.StatusCode)
			if c.wantError != "" {
				assert.Equal(t, c.wantError, body.Error)
				assert.Nil(t, body.Errors)
			}
		})
	}
}

// Generic upstream-unavailable (502) answers the fixed domain message, even when joined with the cause.
func TestUpstreamUnavailableAnswersFixedMessage(t *testing.T) {
	RegisterErrorStatus(http.StatusBadGateway, errUpstream)
	status, body := Error(errors.Join(errUpstream, errors.New(sensitive)))
	require.Equal(t, http.StatusBadGateway, status)
	assert.Equal(t, errUpstream.Error(), body.Error)
	assert.NotContains(t, body.Error, "uq_users_cpf")
}

var errUpstream = errors.New("upstream unavailable")

// Domain errors keep their own message, and the cause wrapped around the sentinel is not exposed.
func TestDomainErrorsKeepTheirMessage(t *testing.T) {
	RegisterErrorStatus(http.StatusNotFound, errNotFound)
	RegisterErrorStatus(http.StatusConflict, errDomainBad)
	RegisterErrorStatus(http.StatusUnprocessableEntity, errDomainNotFnd)

	for _, tc := range []struct {
		err    error
		status int
	}{
		{errNotFound, http.StatusNotFound},
		{errDomainBad, http.StatusConflict},
		{errDomainNotFnd, http.StatusUnprocessableEntity},
	} {
		status, body := Error(fmt.Errorf("context: %w", errors.Join(tc.err, errors.New(sensitive))))
		require.Equal(t, tc.status, status)
		assert.Equal(t, tc.err.Error(), body.Error)
		assert.Nil(t, body.Errors)
	}
}

var (
	errFieldName = errors.New("field name is required")
	errFieldSize = errors.New("field size is invalid")
)

func TestBadRequestListsOnlyTheMatchedValidationMessages(t *testing.T) {
	RegisterValidationErrors(errFieldName, errFieldSize)

	status, body := Error(errors.Join(errFieldName, errFieldSize))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, []string{errFieldName.Error(), errFieldSize.Error()}, body.Errors)
	assert.Empty(t, body.Error)

	status, body = Error(fmt.Errorf("body: %w", errFieldName))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, []string{errFieldName.Error()}, body.Errors, "the wrapping context is not sent")
}

// A joined validation error never carries the driver text of the cause it is joined with.
func TestJoinedDriverErrorIsNotValidation(t *testing.T) {
	RegisterValidationErrors(errFieldName)
	driver := errors.New(sensitive)

	status, body := Error(errors.Join(driver, errors.New("boom")))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, messageInternalError, body.Error)
	assert.Nil(t, body.Errors)

	status, body = Error(errors.Join(errFieldName, driver))
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, []string{errFieldName.Error()}, body.Errors)
	assert.NotContains(t, fmt.Sprint(body), "uq_users_cpf")
}

func TestUnauthorizedAndForbiddenUseSingleMessage(t *testing.T) {
	status, body := Error(authPkg.ErrInvalidUserCredentials)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, authPkg.ErrInvalidUserCredentials.Error(), body.Error)
	assert.Nil(t, body.Errors)
}

func TestDescribeFlagsServerSideErrors(t *testing.T) {
	RegisterErrorStatus(http.StatusNotFound, errNotFound)
	cases := []struct {
		name string
		err  error
		log  bool
	}{
		{"domain not found is not logged", errNotFound, false},
		{"validation is not logged", jsonv2.ErrJSONSyntax, false},
		{"credentials are not logged", authPkg.ErrInvalidUserCredentials, false},
		{"infra conflict is logged", app.ErrDataConflict, true},
		{"infra violation is logged", app.ErrDataViolation, true},
		{"unknown error is logged", errors.New("boom"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, logIt := Describe(c.err)
			assert.Equal(t, c.log, logIt)
		})
	}
}

func TestRegisterErrorStatusKeepsRegistrationOrder(t *testing.T) {
	first := errors.New("first")
	RegisterErrorStatus(http.StatusAccepted, first)
	RegisterErrorStatus(http.StatusNoContent, first)
	status, _ := Error(first)
	assert.Equal(t, http.StatusAccepted, status, "the first registration wins")
}
