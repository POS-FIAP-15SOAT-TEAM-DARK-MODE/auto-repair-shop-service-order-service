package messaging

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ErrInvalidEnvelope is returned when a message body is not a valid envelope.
var ErrInvalidEnvelope = errors.New("invalid message envelope")

// Envelope is the JSON body of every message (see contracts/messaging.md).
type Envelope struct {
	MessageID      string          `json:"message_id"`
	SagaID         string          `json:"saga_id"`
	Type           string          `json:"type"`
	OccurredAt     time.Time       `json:"occurred_at"`
	ServiceOrderID string          `json:"service_order_id"`
	RequestID      string          `json:"request_id"`
	Payload        json.RawMessage `json:"payload"`
}

// LogFields returns the fields that identify the message in the logs: message_id, and saga_id
// and service_order_id when the envelope carries them.
func (e Envelope) LogFields() []zap.Field {
	fields := []zap.Field{zap.String("message_id", e.MessageID)}
	if e.SagaID != "" {
		fields = append(fields, zap.String("saga_id", e.SagaID))
	}
	if e.ServiceOrderID != "" {
		fields = append(fields, zap.String("service_order_id", e.ServiceOrderID))
	}
	return fields
}

// NewEnvelope builds an envelope with a new UUID v4 message_id and occurred_at in UTC.
func NewEnvelope(typ, sagaID, serviceOrderID, requestID string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		MessageID:      uuid.NewString(),
		SagaID:         sagaID,
		Type:           typ,
		OccurredAt:     time.Now().UTC(),
		ServiceOrderID: serviceOrderID,
		RequestID:      requestID,
		Payload:        raw,
	}, nil
}

// Decode parses and validates a message body.
func Decode(body []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Envelope{}, errors.Join(ErrInvalidEnvelope, err)
	}
	if _, err := uuid.Parse(env.MessageID); err != nil {
		return Envelope{}, errors.Join(ErrInvalidEnvelope, err)
	}
	if env.Type == "" {
		return Envelope{}, ErrInvalidEnvelope
	}
	return env, nil
}
