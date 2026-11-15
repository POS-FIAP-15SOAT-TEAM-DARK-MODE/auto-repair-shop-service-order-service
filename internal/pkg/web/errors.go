package web

import (
	"errors"
	"net/http"
	"slices"

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

// validationErrors are the input-validation sentinels: they answer 400 and their messages go to errors.
// Domains add their own with RegisterValidationErrors.
var validationErrors = []error{
	jsonv2.ErrJSONSyntax,
	jsonv2.ErrJSONType,
	jsonv2.ErrJSONUnexpectedEOF,
	jsonv2.ErrJSONEmptyBody,
	jsonv2.ErrWrongPayloadFormat,
}

// RegisterValidationErrors registers input-validation sentinels of a domain (400). A sentinel
// registered twice is kept once, so its message is never listed twice.
func RegisterValidationErrors(errs ...error) {
	for _, e := range errs {
		if !slices.Contains(validationErrors, e) {
			validationErrors = append(validationErrors, e)
		}
	}
}

// Error builds an errorResponse for the given error and decides the status code.
func Error(err error) (int, errorResponse) {
	status, body, _ := Describe(err)
	return status, body
}

// Describe is Error plus a flag telling whether the error must be logged on the server side:
// every 5xx and the generic (infrastructure) 409 and 422.
func Describe(err error) (int, errorResponse, bool) {
	status, sentinel, messages := classify(err)
	body := errorResponse{StatusCode: status}

	switch {
	case status == http.StatusBadRequest:
		body.Errors = messages
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

// classify returns the HTTP status of err, the sentinel that decided it (nil when none did, so the
// body is generic) and, for 400, the messages of the validation sentinels that err matches.
// Causes that match no known sentinel never reach the body.
func classify(err error) (int, error, []string) {
	for _, m := range extraStatuses {
		if errors.Is(err, m.err) {
			if m.status == http.StatusBadRequest {
				return m.status, nil, []string{m.err.Error()}
			}
			return m.status, m.err, nil
		}
	}
	if errors.Is(err, authPkg.ErrInvalidUserCredentials) {
		return http.StatusUnauthorized, authPkg.ErrInvalidUserCredentials, nil
	}
	if messages := validationMessages(err); len(messages) > 0 {
		return http.StatusBadRequest, nil, messages
	}
	switch {
	case isConflictError(err):
		return http.StatusConflict, nil, nil
	case isUnprocessableEntityError(err):
		return http.StatusUnprocessableEntity, nil, nil
	default:
		return http.StatusInternalServerError, nil, nil
	}
}

// validationMessages returns the messages of the validation sentinels that err matches, in
// registration order. Other causes wrapped or joined with them are never included.
func validationMessages(err error) []string {
	var messages []string
	for _, s := range validationErrors {
		if errors.Is(err, s) {
			messages = append(messages, s.Error())
		}
	}
	return messages
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

func isConflictError(err error) bool {
	return errors.Is(err, app.ErrDataConflict) || errors.Is(err, app.ErrInfraConflict)
}

func isUnprocessableEntityError(err error) bool {
	return errors.Is(err, app.ErrDataViolation)
}
