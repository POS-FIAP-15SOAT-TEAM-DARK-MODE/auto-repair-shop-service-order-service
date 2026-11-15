package saga

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const contractsDir = "../../testdata/contracts"

type nopNotifier struct{}

func (nopNotifier) NotifyStatusChange(context.Context, domain.StatusNotification) error { return nil }

func readFixture(t *testing.T, typ string) (messaging.Envelope, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(contractsDir, typ+".json"))
	require.NoError(t, err)
	env, err := messaging.Decode(raw)
	require.NoError(t, err)
	return env, raw
}

// Every fixture is a valid envelope whose type is its file name.
func TestContractFixturesAreValidEnvelopes(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(contractsDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, file := range files {
		typ := strings.TrimSuffix(filepath.Base(file), ".json")
		t.Run(typ, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			require.NoError(t, err)
			env, err := messaging.Decode(raw)
			require.NoError(t, err, "message_id must be a UUID and type must be set")

			assert.Equal(t, typ, env.Type, "the type must match the file name")
			_, err = uuid.Parse(env.SagaID)
			assert.NoError(t, err, "saga_id must be a UUID")
			assert.NotEmpty(t, env.ServiceOrderID)
			assert.False(t, env.OccurredAt.IsZero(), "occurred_at is required")
			assert.Equal(t, "UTC", env.OccurredAt.Location().String(), "occurred_at is UTC")
			require.NotEmpty(t, env.Payload)
			var payload map[string]any
			assert.NoError(t, json.Unmarshal(env.Payload, &payload), "payload must be a JSON object")
		})
	}
}

// The payloads produced by the saga are the ones of the fixtures (keys, types and money format).
func TestProducedPayloadsMatchContractFixtures(t *testing.T) {
	customerUserID := "3b123072-5ad5-4df8-8d3b-3c2770f11b2a"
	total := decimal.RequireFromString("221")

	so := domain.ServiceOrder{
		ID:       "01JD8Z4R3Q8M2V7K5N9XWABCDE",
		Customer: domain.CustomerSnapshot{UserID: customerUserID},
		Works: []domain.WorkItem{{
			ID: "9b2d6a0e-3c0f-4a0b-8d57-2a5f6a1c9e11", Name: "Troca de óleo",
			UnitPrice: decimal.RequireFromString("150"),
		}},
		Supplies: []domain.SupplyItem{{
			ID: "22222222-2222-4222-8222-000000000001", Name: "Filtro",
			UnitPrice: decimal.RequireFromString("35.5"), Quantity: 2,
		}},
		TotalAmount: total,
	}

	box := NewMemoryOutbox()
	changer := NewStatusChanger(nil, box, nopNotifier{})
	prev := domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS
	require.NoError(t, changer.publish(context.Background(), &so, &prev,
		domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL, "", Meta{SagaID: "0f3c9a2e-6a1d-4a53-8a6e-5d3b1f2c7e90"}))
	statusEvent := box.Sent[len(box.Sent)-1].Env

	produced := map[string]any{
		CmdGenerateBudget:            buildGenerateBudget(so),
		CmdRequestPayment:            requestPaymentPayload{Amount: money(total)},
		CmdEnqueueExecution:          enqueueExecutionPayload{Works: []executionWork{{WorkID: so.Works[0].ID, Name: so.Works[0].Name}}},
		CmdCancelBudget:              emptyPayload,
		CmdCancelExecution:           emptyPayload,
		CmdRefundPayment:             emptyPayload,
		CmdReleaseStock:              emptyPayload,
		EvtServiceOrderStatusChanged: statusEvent.Payload,
	}

	for typ, p := range produced {
		t.Run(typ, func(t *testing.T) {
			_, raw := readFixture(t, typ)
			var fixture struct {
				Payload json.RawMessage `json:"payload"`
			}
			require.NoError(t, json.Unmarshal(raw, &fixture))

			got, err := json.Marshal(p)
			require.NoError(t, err)
			assert.JSONEq(t, string(fixture.Payload), string(got))
		})
	}
}
