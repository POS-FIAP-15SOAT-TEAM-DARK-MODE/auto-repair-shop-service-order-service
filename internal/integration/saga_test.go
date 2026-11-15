//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The transitions every order goes through before the budget (ServiceOrderStatusChanged).
var openTransitions = []string{
	"->NEW", "NEW->RECEIVED", "RECEIVED->IN_DIAGNOSIS", "IN_DIAGNOSIS->AWAITING_APPROVAL",
}

// (1) Happy path: send -> BudgetGenerated -> accept (IN_PROGRESS at once) -> PaymentApproved ->
// ExecutionQueued -> ExecutionCompleted -> deliver.
func TestSagaHappyPath(t *testing.T) {
	o := newOrder(t)
	o.openBudget(t)
	o.budgetGenerated(t)
	o.accept(t)
	o.requestPayment(t)

	publish(t, "billing-events", reply(t, o.saga, o.id, "PaymentApproved",
		map[string]any{"payment_id": uuid.NewString(), "amount": "221.00"}))
	enq := it.executionCmds.awaitTypes(t, o.id, "EnqueueExecution")
	require.Len(t, enq, 1)
	assert.Equal(t, o.saga, enq[0].SagaID)
	assert.JSONEq(t, `{"works":[{"work_id":"`+o.workID+`","name":"Troca de óleo"}]}`, string(enq[0].Payload))

	publish(t, "execution-events", reply(t, o.saga, o.id, "ExecutionQueued", map[string]any{"task_id": uuid.NewString()}))
	awaitSaga(t, o.saga, "EXECUTION", "RUNNING")
	publish(t, "execution-events", reply(t, o.saga, o.id, "ExecutionCompleted", map[string]any{"task_id": uuid.NewString()}))
	awaitStatus(t, o.id, "COMPLETED")
	awaitSaga(t, o.saga, "DONE", "COMPLETED")

	o.expect(t, http.MethodPut, "/deliver", staff(t, auth.ATTENDANT), http.StatusNoContent)
	awaitStatus(t, o.id, "DELIVERED")

	awaitTransitions(t, o.id, append(openTransitions,
		"AWAITING_APPROVAL->IN_PROGRESS",
		"IN_PROGRESS->COMPLETED",
		"COMPLETED->DELIVERED",
	)...)
	it.billingCmds.awaitTypes(t, o.id, "GenerateBudget", "RequestPayment")
	it.catalogCmds.awaitTypes(t, o.id)
}

// (2) PaymentFailed: order REJECTED with PAYMENT_REJECTED; CancelBudget and ReleaseStock.
func TestSagaPaymentFailedRejects(t *testing.T) {
	o := newOrder(t)
	o.openBudget(t)
	o.budgetGenerated(t)
	o.accept(t)
	o.requestPayment(t)

	publish(t, "billing-events", reply(t, o.saga, o.id, "PaymentFailed",
		map[string]any{"payment_id": uuid.NewString(), "amount": "221.00", "reason": "PAYMENT_REJECTED"}))
	awaitStatus(t, o.id, "REJECTED")
	awaitSaga(t, o.saga, "PAYMENT", "COMPENSATED")

	awaitTransitions(t, o.id, append(openTransitions,
		"AWAITING_APPROVAL->IN_PROGRESS",
		"IN_PROGRESS->REJECTED:PAYMENT_REJECTED",
	)...)
	it.billingCmds.awaitTypes(t, o.id, "GenerateBudget", "RequestPayment", "CancelBudget")
	it.catalogCmds.awaitTypes(t, o.id, "ReleaseStock")
	it.executionCmds.awaitTypes(t, o.id)
}

// (3) ExecutionFailed: order CANCELLED with EXECUTION_FAILED; RefundPayment, CancelBudget and ReleaseStock.
func TestSagaExecutionFailedCancels(t *testing.T) {
	o := newOrder(t)
	o.openBudget(t)
	o.budgetGenerated(t)
	o.accept(t)
	o.requestPayment(t)

	publish(t, "billing-events", reply(t, o.saga, o.id, "PaymentApproved",
		map[string]any{"payment_id": uuid.NewString(), "amount": "221.00"}))
	it.executionCmds.awaitTypes(t, o.id, "EnqueueExecution")
	publish(t, "execution-events", reply(t, o.saga, o.id, "ExecutionQueued", map[string]any{"task_id": uuid.NewString()}))
	awaitSaga(t, o.saga, "EXECUTION", "RUNNING")

	publish(t, "execution-events", reply(t, o.saga, o.id, "ExecutionFailed",
		map[string]any{"task_id": uuid.NewString(), "reason": "ALL_WORKS_CANCELLED"}))
	awaitStatus(t, o.id, "CANCELLED")
	awaitSaga(t, o.saga, "EXECUTION", "COMPENSATED")

	awaitTransitions(t, o.id, append(openTransitions,
		"AWAITING_APPROVAL->IN_PROGRESS",
		"IN_PROGRESS->CANCELLED:EXECUTION_FAILED",
	)...)
	it.billingCmds.awaitTypes(t, o.id, "GenerateBudget", "RequestPayment", "RefundPayment", "CancelBudget")
	it.catalogCmds.awaitTypes(t, o.id, "ReleaseStock")
	it.executionCmds.awaitTypes(t, o.id, "EnqueueExecution")
}

// (4) Customer reject: order REJECTED with BUDGET_REJECTED; CancelBudget and ReleaseStock.
func TestSagaCustomerRejectCompensates(t *testing.T) {
	o := newOrder(t)
	o.openBudget(t)
	o.budgetGenerated(t)

	o.expect(t, http.MethodPut, "/reject", token(t, o.userID, auth.CUSTOMER), http.StatusNoContent)
	awaitStatus(t, o.id, "REJECTED")
	awaitSaga(t, o.saga, "AWAITING_CUSTOMER", "COMPENSATED")

	awaitTransitions(t, o.id, append(openTransitions, "AWAITING_APPROVAL->REJECTED:BUDGET_REJECTED")...)
	it.billingCmds.awaitTypes(t, o.id, "GenerateBudget", "CancelBudget")
	it.catalogCmds.awaitTypes(t, o.id, "ReleaseStock")
	it.executionCmds.awaitTypes(t, o.id)
}

// (5) The same reply published twice has a single effect: one transition, one next command.
func TestSagaDuplicateReplyHasSingleEffect(t *testing.T) {
	o := newOrder(t)
	o.openBudget(t)

	budget := reply(t, o.saga, o.id, "BudgetGenerated", map[string]any{
		"budget_id": uuid.NewString(), "total_amount": "221.00",
	})
	publish(t, "billing-events", budget)
	publish(t, "billing-events", budget)
	awaitStatus(t, o.id, "AWAITING_APPROVAL")
	awaitTransitions(t, o.id, openTransitions...) // exactly one AWAITING_APPROVAL transition

	o.accept(t)
	o.requestPayment(t)
	payment := reply(t, o.saga, o.id, "PaymentApproved", map[string]any{"payment_id": uuid.NewString(), "amount": "221.00"})
	publish(t, "billing-events", payment)
	publish(t, "billing-events", payment)
	it.executionCmds.awaitTypes(t, o.id, "EnqueueExecution")
	awaitSaga(t, o.saga, "EXECUTION_QUEUE", "RUNNING")
	assert.Equal(t, "IN_PROGRESS", statusOf(t, o.id))
}

// (6) Cancel in IN_PROGRESS while the saga waits for the payment: CANCELLED (USER_CANCELLED);
// RefundPayment, CancelBudget and ReleaseStock. No CancelExecution: execution was never enqueued.
func TestSagaCancelDuringPaymentCompensates(t *testing.T) {
	o := newOrder(t)
	o.openBudget(t)
	o.budgetGenerated(t)
	o.accept(t)
	o.requestPayment(t)
	awaitSaga(t, o.saga, "PAYMENT", "RUNNING")

	o.expect(t, http.MethodPut, "/cancel", staff(t, auth.ATTENDANT), http.StatusNoContent)
	awaitStatus(t, o.id, "CANCELLED")
	awaitSaga(t, o.saga, "PAYMENT", "COMPENSATED")

	awaitTransitions(t, o.id, append(openTransitions,
		"AWAITING_APPROVAL->IN_PROGRESS",
		"IN_PROGRESS->CANCELLED:USER_CANCELLED",
	)...)
	it.billingCmds.awaitTypes(t, o.id, "GenerateBudget", "RequestPayment", "RefundPayment", "CancelBudget")
	it.catalogCmds.awaitTypes(t, o.id, "ReleaseStock")
	it.executionCmds.awaitTypes(t, o.id)
}
