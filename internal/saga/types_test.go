package saga

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/uow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func specTypes(specs []commandSpec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.typ)
	}
	return out
}

func TestCancelCommandsFollowTheFlags(t *testing.T) {
	cases := []struct {
		name     string
		inst     Instance
		supplies bool
		want     []string
	}{
		{"nothing requested, no supplies", Instance{}, false, []string{}},
		{"nothing requested, supplies", Instance{}, true, []string{CmdReleaseStock}},
		{"budget only", Instance{BudgetRequested: true}, false, []string{CmdCancelBudget}},
		{"payment and budget", Instance{BudgetRequested: true, PaymentRequested: true}, false,
			[]string{CmdRefundPayment, CmdCancelBudget}},
		{"full saga with supplies", Instance{BudgetRequested: true, PaymentRequested: true, ExecutionEnqueued: true}, true,
			[]string{CmdCancelExecution, CmdRefundPayment, CmdCancelBudget, CmdReleaseStock}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := tc.inst
			assert.Equal(t, tc.want, specTypes(cancelCommands(&inst, tc.supplies)))
		})
	}
}

func TestSendCommandFailsForUnmarshalablePayload(t *testing.T) {
	box := NewMemoryOutbox()
	err := sendCommand(context.Background(), box, "saga", soID, "", specCancelBudget, make(chan int))
	assert.Error(t, err)
	assert.Empty(t, box.Sent)
}

func TestPGOutboxStoresInsideTheAttachedTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	ctx := uow.WithTx(context.Background(), tx)

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO outbox")).WillReturnResult(sqlmock.NewResult(1, 1))
	env := mustEnv(t, CmdCancelBudget, "saga-1", map[string]string{})
	require.NoError(t, PGOutbox{}.Enqueue(ctx, TopicSagaCommands, TargetBilling, env))
	require.NoError(t, mock.ExpectationsWereMet())
}
