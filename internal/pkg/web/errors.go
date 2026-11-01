package web

import (
	"errors"
	"net/http"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	authPkg "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	jsonv2 "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/json"
)

type errorResponse struct {
	StatusCode int      `json:"code"`
	Errors     []string `json:"errors,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Error builds an errorResponse for the given error and decides the status code.
func Error(err error) (int, errorResponse) {
	status := getHTTPStatus(err)
	stackedErrors, errorMsg := buildErrorMessages(err, status)

	return status, errorResponse{
		StatusCode: status,
		Errors:     stackedErrors,
		Error:      errorMsg,
	}
}

// RegisterErrorStatus lets a domain map its own sentinel errors to an HTTP status code.
// Mappings are checked in registration order before the generic ones.
func RegisterErrorStatus(status int, errs ...error) {
	for _, e := range errs {
		extraStatuses = append(extraStatuses, statusMapping{err: e, status: status})
	}
}

type statusMapping struct {
	err    error
	status int
}

var extraStatuses []statusMapping

func getHTTPStatus(err error) int {
	for _, m := range extraStatuses {
		if errors.Is(err, m.err) {
			return m.status
		}
	}
	switch {
	case errors.Is(err, authPkg.ErrInvalidUserCredentials):
		return http.StatusUnauthorized
	case isBadRequestError(err), IsJoined(err):
		return http.StatusBadRequest
	case isConflictError(err):
		return http.StatusConflict
	case isUnprocessableEntityError(err):
		return http.StatusUnprocessableEntity
	case isInternalServerError(err):
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func IsJoined(err error) bool {
	type unwrapper interface {
		Unwrap() []error
	}

	var uw unwrapper
	return errors.As(err, &uw)
}

func buildErrorMessages(err error, status int) ([]string, string) {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest:
		return unwrapAll(err), ""
	case http.StatusNotFound:
		return nil, err.Error()
	case http.StatusConflict, http.StatusUnprocessableEntity, http.StatusBadGateway, http.StatusInternalServerError:
		root := err
		for {
			unwrapped := errors.Unwrap(root)
			if unwrapped == nil {
				break
			}
			root = unwrapped
		}
		return nil, root.Error()
	default:
		return nil, "Internal Server Error"
	}
}

func isBadRequestError(err error) bool {
	return errors.Is(err, jsonv2.ErrJSONSyntax) ||
		errors.Is(err, jsonv2.ErrJSONType) ||
		errors.Is(err, jsonv2.ErrJSONUnexpectedEOF) ||
		errors.Is(err, jsonv2.ErrJSONEmptyBody) ||
		errors.Is(err, jsonv2.ErrWrongPayloadFormat)
}

func isInternalServerError(err error) bool {
	return errors.Is(err, jsonv2.ErrJSONUnsupportedValue) ||
		errors.Is(err, jsonv2.ErrJSONUnsupportedType) ||
		errors.Is(err, jsonv2.ErrJSONInvalidUnmarshal)
}

func isConflictError(err error) bool {
	return errors.Is(err, app.ErrDataConflict) || errors.Is(err, app.ErrInfraConflict)
}

func isUnprocessableEntityError(err error) bool {
	return errors.Is(err, app.ErrDataViolation)
}

func unwrapAll(err error) []string {
	if err == nil {
		return nil
	}

	type multiUnwrap interface {
		Unwrap() []error
	}
	if m, ok := err.(multiUnwrap); ok {
		var all []string
		for _, e := range m.Unwrap() {
			all = append(all, unwrapAll(e)...)
		}
		return all
	}
	return []string{err.Error()}
}
