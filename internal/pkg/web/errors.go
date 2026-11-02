package web

import (
	"errors"
	"net/http"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/app"
	authPkg "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	jsonv2 "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/json"
)

// Generic messages for the errors that do not come from a registered domain sentinel.
// The cause stays in the server log; the body never carries it.
const (
	messageConflict      = "conflict"
	messageInvalidData   = "invalid data"
	messageInternalError = "internal server error"
)

type errorResponse struct {
	StatusCode int      `json:"code"`
	Errors     []string `json:"errors,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Error builds an errorResponse for the given error and decides the status code.
func Error(err error) (int, errorResponse) {
	status, body, _ := Describe(err)
	return status, body
}

// Describe is Error plus a flag telling whether the error must be logged on the server side:
// every 5xx and the generic (infrastructure) 409 and 422.
func Describe(err error) (int, errorResponse, bool) {
	status, sentinel := classify(err)
	body := errorResponse{StatusCode: status}

	switch {
	case status == http.StatusBadRequest:
		body.Errors = unwrapAll(err)
	case sentinel != nil:
		body.Error = sentinel.Error()
	default:
		body.Error = genericMessage(status)
	}

	logIt := status >= http.StatusInternalServerError ||
		(sentinel == nil && (status == http.StatusConflict || status == http.StatusUnprocessableEntity))
	return status, body, logIt
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

// classify returns the HTTP status of err and the sentinel that decided it (nil when no
// domain sentinel matched).
func classify(err error) (int, error) {
	for _, m := range extraStatuses {
		if errors.Is(err, m.err) {
			return m.status, m.err
		}
	}
	switch {
	case errors.Is(err, authPkg.ErrInvalidUserCredentials):
		return http.StatusUnauthorized, authPkg.ErrInvalidUserCredentials
	case isBadRequestError(err), IsJoined(err):
		return http.StatusBadRequest, nil
	case isConflictError(err):
		return http.StatusConflict, nil
	case isUnprocessableEntityError(err):
		return http.StatusUnprocessableEntity, nil
	default:
		return http.StatusInternalServerError, nil
	}
}

func genericMessage(status int) string {
	switch status {
	case http.StatusConflict:
		return messageConflict
	case http.StatusUnprocessableEntity:
		return messageInvalidData
	default:
		return messageInternalError
	}
}

func IsJoined(err error) bool {
	type unwrapper interface {
		Unwrap() []error
	}

	var uw unwrapper
	return errors.As(err, &uw)
}

func isBadRequestError(err error) bool {
	return errors.Is(err, jsonv2.ErrJSONSyntax) ||
		errors.Is(err, jsonv2.ErrJSONType) ||
		errors.Is(err, jsonv2.ErrJSONUnexpectedEOF) ||
		errors.Is(err, jsonv2.ErrJSONEmptyBody) ||
		errors.Is(err, jsonv2.ErrWrongPayloadFormat)
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
