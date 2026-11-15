package saga

import (
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
)

var (
	specGenerateBudget   = commandSpec{CmdGenerateBudget, TargetBilling}
	specRequestPayment   = commandSpec{CmdRequestPayment, TargetBilling}
	specEnqueueExecution = commandSpec{CmdEnqueueExecution, TargetExecution}
)

// Payloads of the commands that carry data (contracts/messaging.md). Money is a decimal string.
type budgetWork struct {
	WorkID    string `json:"work_id"`
	Name      string `json:"name"`
	UnitPrice string `json:"unit_price"`
}

type budgetSupply struct {
	SupplyID  string `json:"supply_id"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice string `json:"unit_price"`
}

type generateBudgetPayload struct {
	CustomerUserID string         `json:"customer_user_id"`
	Works          []budgetWork   `json:"works"`
	Supplies       []budgetSupply `json:"supplies"`
}

type requestPaymentPayload struct {
	Amount string `json:"amount"`
}

type executionWork struct {
	WorkID string `json:"work_id"`
	Name   string `json:"name"`
}

type enqueueExecutionPayload struct {
	Works []executionWork `json:"works"`
}

// buildGenerateBudget snapshots the works and supplies of the order for Billing.
func buildGenerateBudget(so domain.ServiceOrder) generateBudgetPayload {
	p := generateBudgetPayload{CustomerUserID: so.Customer.UserID}
	for _, w := range so.Works {
		p.Works = append(p.Works, budgetWork{WorkID: w.ID, Name: w.Name, UnitPrice: money(w.UnitPrice)})
	}
	for _, s := range so.Supplies {
		p.Supplies = append(p.Supplies, budgetSupply{SupplyID: s.ID, Name: s.Name, Quantity: s.Quantity, UnitPrice: money(s.UnitPrice)})
	}
	return p
}
