//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// order is one service order created by a scenario, with its own customer, vehicle, work and supply.
type order struct {
	id         string
	customerID string
	userID     string // the customer's user: the only one allowed to accept or reject
	vehicleID  string
	workID     string
	supplyID   string
	saga       string // saga_id taken from the GenerateBudget command
}

// token signs a JWT with the same key the application uses.
func token(t *testing.T, userID string, role auth.Role) string {
	t.Helper()
	tok, err := auth.GenerateToken(userID, []auth.Role{role}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	return tok
}

func staff(t *testing.T, role auth.Role) string {
	return token(t, uuid.NewString(), role)
}

// newOrder registers the data in the stubs and creates a NEW order with one work and one supply.
func newOrder(t *testing.T) *order {
	t.Helper()
	o := &order{
		customerID: uuid.NewString(), userID: uuid.NewString(), vehicleID: uuid.NewString(),
		workID: uuid.NewString(), supplyID: uuid.NewString(),
	}
	it.customers.addCustomer(customerRow{
		ID: o.customerID, UserID: o.userID, Type: "INDIVIDUAL", Name: "João Silva",
		Email: "joao.silva@example.com", Document: "52998224725", Phone: "11999990001",
	})
	it.customers.addVehicle(vehicleRow{
		ID: o.vehicleID, CustomerID: o.customerID, LicensePlate: "ABC1D23", Brand: "Fiat", Model: "Uno", Year: 2018,
	})
	it.catalog.addWork(workRow{
		ID: o.workID, Name: "Troca de óleo", Description: "Troca de óleo do motor com filtro incluído",
		UnitPrice: "150.00", Active: true,
	})
	it.catalog.addSupply(supplyRow{
		ID: o.supplyID, Name: "Filtro", Description: "Filtro de óleo", UnitPrice: "35.50", Version: 3,
	})

	body := map[string]any{
		"client":   o.customerID,
		"vehicle":  o.vehicleID,
		"works":    []string{o.workID},
		"supplies": []map[string]any{{"id": o.supplyID, "amount": 2}},
	}
	code, raw := call(t, http.MethodPost, "/v1/service-order", staff(t, auth.ATTENDANT), body)
	require.Equal(t, http.StatusCreated, code, string(raw))
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	require.Equal(t, "NEW", created.Status)
	o.id = created.ID
	return o
}

// expect performs a request on the order and checks the status code.
func (o *order) expect(t *testing.T, method, action, tok string, want int) {
	t.Helper()
	code, raw := call(t, method, "/v1/service-order/"+o.id+action, tok, nil)
	require.Equal(t, want, code, "%s %s: %s", method, action, string(raw))
}

// openBudget moves the order to IN_DIAGNOSIS with a work and sends it to Billing. It returns after
// the GenerateBudget command is read, and checks its payload.
func (o *order) openBudget(t *testing.T) {
	t.Helper()
	o.expect(t, http.MethodPut, "/received", staff(t, auth.ATTENDANT), http.StatusNoContent)
	o.expect(t, http.MethodPut, "/start-diagnosis", staff(t, auth.MECHANIC), http.StatusNoContent)
	o.expect(t, http.MethodPut, "/send", staff(t, auth.MECHANIC), http.StatusNoContent)

	// Send keeps the order in IN_DIAGNOSIS until the budget comes back.
	assert.Equal(t, "IN_DIAGNOSIS", statusOf(t, o.id))

	it.billingCmds.awaitTypes(t, o.id, "GenerateBudget")
	gen := it.billingCmds.awaitMessage(t, o.id, "GenerateBudget")
	o.saga = gen.SagaID
	var p struct {
		CustomerUserID string `json:"customer_user_id"`
		Works          []struct {
			WorkID    string `json:"work_id"`
			UnitPrice string `json:"unit_price"`
		} `json:"works"`
		Supplies []struct {
			SupplyID  string `json:"supply_id"`
			Quantity  int    `json:"quantity"`
			UnitPrice string `json:"unit_price"`
		} `json:"supplies"`
	}
	require.NoError(t, json.Unmarshal(gen.Payload, &p))
	assert.Equal(t, o.userID, p.CustomerUserID)
	require.Len(t, p.Works, 1)
	assert.Equal(t, o.workID, p.Works[0].WorkID)
	assert.Equal(t, "150.00", p.Works[0].UnitPrice)
	require.Len(t, p.Supplies, 1)
	assert.Equal(t, o.supplyID, p.Supplies[0].SupplyID)
	assert.Equal(t, 2, p.Supplies[0].Quantity)
	assert.Equal(t, "35.50", p.Supplies[0].UnitPrice)
}

// budgetGenerated sends the budget reply of Billing (total 221.00 = 150.00 + 2 x 35.50).
func (o *order) budgetGenerated(t *testing.T) messaging.Envelope {
	t.Helper()
	env := reply(t, o.saga, o.id, "BudgetGenerated", map[string]any{
		"budget_id": uuid.NewString(), "total_amount": "221.00",
	})
	publish(t, "billing-events", env)
	awaitStatus(t, o.id, "AWAITING_APPROVAL")
	return env
}

// accept lets the customer approve the budget and checks the immediate IN_PROGRESS.
func (o *order) accept(t *testing.T) {
	t.Helper()
	o.expect(t, http.MethodPut, "/accept", token(t, o.userID, auth.CUSTOMER), http.StatusNoContent)
	// No reply of Billing has been published yet: the order is already IN_PROGRESS.
	assert.Equal(t, "IN_PROGRESS", statusOf(t, o.id))
}

// requestPayment checks the RequestPayment command sent by accept.
func (o *order) requestPayment(t *testing.T) {
	t.Helper()
	it.billingCmds.awaitTypes(t, o.id, "GenerateBudget", "RequestPayment")
	cmd := it.billingCmds.awaitMessage(t, o.id, "RequestPayment")
	assert.Equal(t, o.saga, cmd.SagaID)
	var p struct {
		Amount string `json:"amount"`
	}
	require.NoError(t, json.Unmarshal(cmd.Payload, &p))
	assert.Equal(t, "221.00", p.Amount)
}
