package service_order

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/clients/catalog"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/saga"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/adapters"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/service_order/domain"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ctx = context.Background()

func createReq(works []string, supplies ...adapters.AddSupplyItem) adapters.CreateSORequest {
	return adapters.CreateSORequest{CustomerID: customerID, VehicleID: vehicleID, WorkIDs: works, Supplies: supplies}
}

// ---- Create (T4 row 1) ----

func TestCreateStoresNewOrderWithSnapshotsAndReservations(t *testing.T) {
	f := newFixture(t)
	resp, err := f.svc.Create(ctx, createReq([]string{"w1", " w2 ", "w1"}, supplyLine("s1", 2)))
	require.NoError(t, err)
	assert.Equal(t, "NEW", resp.Status)
	require.NotEmpty(t, resp.ID)

	so, err := domain.LoadAggregate(ctx, f.sos, resp.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_NEW, so.Status)
	assert.Equal(t, "João", so.Customer.Name, "customer is snapshotted")
	assert.Equal(t, "Fiat", so.Vehicle.Brand)
	assert.Equal(t, []string{"w1", "w2"}, so.WorkIDs(), "duplicated and padded work ids are resolved once")
	require.Len(t, so.Supplies, 1)
	assert.NotEmpty(t, so.Supplies[0].ReservationID)
	assert.Equal(t, 2, so.Supplies[0].Quantity)
	assert.Equal(t, 3, f.catalog.stock["s1"], "stock is reserved")
	// 150 + 80 + 2 * 35.50
	assert.Equal(t, "301.00", so.TotalAmount.StringFixed(2))
	assert.Equal(t, []string{"ServiceOrderStatusChanged"}, f.lastEventTypes())
	var ev struct {
		Previous *string `json:"previous"`
		New      string  `json:"new"`
	}
	require.NoError(t, json.Unmarshal(f.box.Sent[0].Env.Payload, &ev))
	assert.Nil(t, ev.Previous, "creation event has no previous status")
	assert.Equal(t, "NEW", ev.New)
}

func TestCreateFailsForUnknownCustomerOrVehicle(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(ctx, adapters.CreateSORequest{CustomerID: "nope", VehicleID: vehicleID})
	assert.ErrorIs(t, err, domain.ErrCustomerNotFound)

	_, err = f.svc.Create(ctx, adapters.CreateSORequest{CustomerID: customerID, VehicleID: "nope"})
	assert.ErrorIs(t, err, domain.ErrVehicleNotFound)
	assert.Empty(t, f.catalog.released)
}

func TestCreateRejectsVehicleOfAnotherCustomer(t *testing.T) {
	f := newFixture(t)
	f.customers.owners[vehicleID] = "someone-else"
	_, err := f.svc.Create(ctx, createReq([]string{"w1"}))
	assert.ErrorIs(t, err, domain.ErrInvalidCustomerProperty)
}

func TestCreateRejectsUnknownOrInactiveWork(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(ctx, createReq([]string{"ghost"}))
	assert.ErrorIs(t, err, domain.ErrWorkNotFound)

	_, err = f.svc.Create(ctx, createReq([]string{"w-off"}))
	assert.ErrorIs(t, err, domain.ErrWorkNotFound, "inactive works are not accepted")

	_, err = f.svc.Create(ctx, createReq([]string{"  "}))
	assert.ErrorIs(t, err, domain.ErrInvalidWorkId)
}

func TestCreateSurfacesCatalogWorkErrors(t *testing.T) {
	f := newFixture(t)
	f.catalog.workErr = errBoom
	_, err := f.svc.Create(ctx, createReq([]string{"w1"}))
	assert.ErrorIs(t, err, errBoom)
}

func TestCreateSurfacesCustomerServiceErrors(t *testing.T) {
	f := newFixture(t)
	f.customers.err = errBoom
	_, err := f.svc.Create(ctx, createReq([]string{"w1"}))
	assert.ErrorIs(t, err, errBoom)
}

func TestCreateValidatesSupplies(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(ctx, createReq([]string{"w1"}, supplyLine(" ", 1)))
	assert.ErrorIs(t, err, domain.ErrInvalidSupplyID)
	_, err = f.svc.Create(ctx, createReq([]string{"w1"}, supplyLine("s1", 0)))
	assert.ErrorIs(t, err, domain.ErrInvalidSupplyAmount)
	assert.Equal(t, 5, f.catalog.stock["s1"], "nothing is reserved when validation fails")
}

func TestCreateWithoutStockReleasesEarlierReservations(t *testing.T) {
	f := newFixture(t)
	// s1 is reserved first, s2 has only one unit: asking for 2 fails on the second supply
	_, err := f.svc.Create(ctx, createReq([]string{"w1"}, supplyLine("s1", 2), supplyLine("s2", 2)))
	assert.ErrorIs(t, err, domain.ErrSupplyOutOfStock)
	assert.Equal(t, 5, f.catalog.stock["s1"], "the first reservation is given back")
	assert.Equal(t, 1, f.catalog.stock["s2"])
	assert.Empty(t, f.lastEventTypes())
}

func TestCreateSupplyNotFoundInCatalog(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(ctx, createReq([]string{"w1"}, supplyLine("ghost", 1)))
	assert.ErrorIs(t, err, domain.ErrSupplyNotFound)
}

func TestCreateReleasesReservationsWhenStoringFails(t *testing.T) {
	f := newFixture(t)
	f.repo.insertErr = errBoom
	_, err := f.svc.Create(ctx, createReq([]string{"w1"}, supplyLine("s1", 2)))
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, 5, f.catalog.stock["s1"], "reservations are released after a failed write")
	assert.Len(t, f.catalog.released, 1)
	assert.Empty(t, f.lastEventTypes(), "no status event for an order that was not stored")
}

func TestCreateKeepsErrorWhenReleaseAlsoFails(t *testing.T) {
	f := newFixture(t)
	f.repo.addSupplyErr = errBoom
	f.catalog.releaseErr = errBoom
	_, err := f.svc.Create(ctx, createReq([]string{"w1"}, supplyLine("s1", 2)))
	assert.ErrorIs(t, err, errBoom, "the original failure is returned even if the release fails")
}

// ---- Works (T4 row 2) ----

func TestAddWorksOnlyWhileNewAndRecalculates(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
	require.NoError(t, f.svc.AddWorks(ctx, soID, []string{"w1", "w2"}))
	assert.Equal(t, "230.00", f.total(t))

	works, err := f.svc.GetWorks(ctx, soID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), works.TotalItems)
}

func TestAddWorksRejections(t *testing.T) {
	ctx := context.Background()
	t.Run("empty list", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, nil), domain.ErrEmptyServicesList)
	})
	t.Run("order not found", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, []string{"w1"}), domain.ErrServiceOrderNotFound)
	})
	t.Run("not NEW", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil, nil)
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, []string{"w1"}), domain.ErrServiceOrderNotNew)
	})
	t.Run("inactive work", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, []string{"w-off"}), domain.ErrWorkNotFound)
	})
	t.Run("status changed before the write", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
		f.repo.headerOverride = statusPtr(domain.SERVICE_ORDER_STATUS_RECEIVED)
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, []string{"w1"}), domain.ErrServiceOrderNotNew)
	})
	t.Run("lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.repo.findStatusErr = errBoom
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, []string{"w1"}), errBoom)
	})
	t.Run("catalog fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
		f.catalog.workErr = errBoom
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, []string{"w1"}), errBoom)
	})
	t.Run("store fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
		f.repo.addWorkErr = errBoom
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, []string{"w1"}), errBoom)
	})
	t.Run("in-transaction lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
		f.repo.findErr = errBoom
		assert.ErrorIs(t, f.svc.AddWorks(ctx, soID, []string{"w1"}), errBoom)
	})
}

func TestRemoveWorkOnlyWhileNewAndRecalculates(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, []domain.WorkItem{f.catalog.works["w1"], f.catalog.works["w2"]}, nil)
	require.NoError(t, f.svc.RemoveWork(ctx, soID, "w1"))
	assert.Equal(t, "80.00", f.total(t))
}

func TestRemoveWorkRejections(t *testing.T) {
	ctx := context.Background()
	t.Run("blank id", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.RemoveWork(ctx, soID, "  "), domain.ErrInvalidWorkId)
	})
	t.Run("order not found", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.RemoveWork(ctx, soID, "w1"), domain.ErrServiceOrderNotFound)
	})
	t.Run("not NEW", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]}, nil)
		assert.ErrorIs(t, f.svc.RemoveWork(ctx, soID, "w1"), domain.ErrServiceOrderNotNew)
	})
	t.Run("work not linked", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, []domain.WorkItem{f.catalog.works["w1"]}, nil)
		assert.ErrorIs(t, f.svc.RemoveWork(ctx, soID, "w2"), domain.ErrServiceOrderWorkNotFound)
	})
	t.Run("store fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, []domain.WorkItem{f.catalog.works["w1"]}, nil)
		f.repo.findErr = errBoom
		assert.ErrorIs(t, f.svc.RemoveWork(ctx, soID, "w1"), errBoom)
	})
}

func TestGetWorksAndSuppliesReadOnlyLocalData(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]},
		[]domain.SupplyItem{{ID: "s1", ReservationID: "r", Name: "Filtro", UnitPrice: decimal.RequireFromString("35.50"), Version: 3, Quantity: 2}})

	works, err := f.svc.GetWorks(ctx, soID)
	require.NoError(t, err)
	require.Len(t, works.Items, 1)
	assert.Equal(t, "ACTIVE", works.Items[0].Status)
	assert.Equal(t, int64(1), works.TotalPages)

	supplies, err := f.svc.GetSupplies(ctx, soID)
	require.NoError(t, err)
	require.Len(t, supplies.Items, 1)
	assert.Equal(t, 2, supplies.Items[0].Amount)
	assert.Equal(t, 3, supplies.Items[0].Version)

	_, err = f.svc.GetWorks(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrServiceOrderNotFound)
	_, err = f.svc.GetSupplies(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrServiceOrderNotFound)

	f.repo.listWorksErr = errBoom
	_, err = f.svc.GetWorks(ctx, soID)
	assert.ErrorIs(t, err, errBoom)
	f.repo.listSuppliesErr = errBoom
	_, err = f.svc.GetSupplies(ctx, soID)
	assert.ErrorIs(t, err, errBoom)
}

func TestGetWorksOfEmptyOrderIsAnEmptyPage(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
	works, err := f.svc.GetWorks(ctx, soID)
	require.NoError(t, err)
	assert.Empty(t, works.Items)
	assert.Equal(t, int64(0), works.TotalItems)
}

// ---- Supplies (T4 row 3) ----

func TestAddSuppliesReservesAndLinksInDiagnosis(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]}, nil)
	require.NoError(t, f.svc.AddSupplies(ctx, soID, []adapters.AddSupplyItem{supplyLine("s1", 2)}))

	assert.Equal(t, 3, f.catalog.stock["s1"])
	assert.Equal(t, "221.00", f.total(t))
	supplies, err := f.svc.GetSupplies(ctx, soID)
	require.NoError(t, err)
	require.Len(t, supplies.Items, 1)
	assert.Equal(t, "Filtro", supplies.Items[0].Name)
}

func TestAddSuppliesRejections(t *testing.T) {
	ctx := context.Background()
	t.Run("empty", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.AddSupplies(ctx, soID, nil), domain.ErrEmptyServicesList)
	})
	t.Run("invalid item", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.AddSupplies(ctx, soID, []adapters.AddSupplyItem{supplyLine("s1", -1)}), domain.ErrInvalidSupplyAmount)
	})
	t.Run("order not found", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.AddSupplies(ctx, soID, []adapters.AddSupplyItem{supplyLine("s1", 1)}), domain.ErrServiceOrderNotFound)
	})
	t.Run("not in diagnosis", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
		assert.ErrorIs(t, f.svc.AddSupplies(ctx, soID, []adapters.AddSupplyItem{supplyLine("s1", 1)}), domain.ErrServiceOrderNotInDiagnosis)
	})
	t.Run("out of stock", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil, nil)
		assert.ErrorIs(t, f.svc.AddSupplies(ctx, soID, []adapters.AddSupplyItem{supplyLine("s2", 5)}), domain.ErrSupplyOutOfStock)
	})
	t.Run("status changed before the write releases the reservations", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil, nil)
		f.repo.headerOverride = statusPtr(domain.SERVICE_ORDER_STATUS_RECEIVED)
		assert.ErrorIs(t, f.svc.AddSupplies(ctx, soID, []adapters.AddSupplyItem{supplyLine("s1", 1)}), domain.ErrServiceOrderNotInDiagnosis)
		assert.Equal(t, 5, f.catalog.stock["s1"], "reservation released")
	})
	t.Run("link fails releases the reservations", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil, nil)
		f.repo.addSupplyErr = errBoom
		assert.ErrorIs(t, f.svc.AddSupplies(ctx, soID, []adapters.AddSupplyItem{supplyLine("s1", 1)}), errBoom)
		assert.Equal(t, 5, f.catalog.stock["s1"])
	})
	t.Run("lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.repo.findStatusErr = errBoom
		assert.ErrorIs(t, f.svc.AddSupplies(ctx, soID, []adapters.AddSupplyItem{supplyLine("s1", 1)}), errBoom)
	})
}

func TestRemoveSupplyReleasesReservationAndRecalculates(t *testing.T) {
	f := newFixture(t)
	f.catalog.stock["s1"] = 3
	f.catalog.reservation["res-x"] = reservedRow{supplyID: "s1", qty: 2}
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]},
		[]domain.SupplyItem{{ID: "s1", ReservationID: "res-x", UnitPrice: decimal.RequireFromString("35.50"), Quantity: 2}})
	require.NoError(t, f.svc.RemoveSupply(ctx, soID, "s1"))

	assert.Equal(t, []string{"res-x"}, f.catalog.released)
	assert.Equal(t, 5, f.catalog.stock["s1"])
	assert.Equal(t, "150.00", f.total(t))
	supplies, err := f.svc.GetSupplies(ctx, soID)
	require.NoError(t, err)
	assert.Empty(t, supplies.Items)
}

func TestRemoveSupplyTreatsMissingReservationAsReleased(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil,
		[]domain.SupplyItem{{ID: "s1", ReservationID: "gone", UnitPrice: decimal.RequireFromString("35.50"), Quantity: 1}})
	f.catalog.reservation = map[string]reservedRow{} // 404 from the Catalog
	require.NoError(t, f.svc.RemoveSupply(ctx, soID, "s1"))
	supplies, err := f.svc.GetSupplies(ctx, soID)
	require.NoError(t, err)
	assert.Empty(t, supplies.Items)
}

func TestRemoveSupplyReleaseFailureKeepsUnlinkAndSucceeds(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil,
		[]domain.SupplyItem{{ID: "s1", ReservationID: "res-1", UnitPrice: decimal.RequireFromString("35.50"), Quantity: 1}})
	f.catalog.releaseErr = errBoom
	require.NoError(t, f.svc.RemoveSupply(ctx, soID, "s1"), "the unlink is committed; the failed release is only logged")
	assert.Equal(t, []string{"res-1"}, f.catalog.released)
	supplies, err := f.svc.GetSupplies(ctx, soID)
	require.NoError(t, err)
	assert.Empty(t, supplies.Items, "the link is removed even when the stock could not be released")
}

func TestRemoveSupplyReleasesOnlyAfterUnlinkCommits(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil,
		[]domain.SupplyItem{{ID: "s1", ReservationID: "res-1", UnitPrice: decimal.RequireFromString("35.50"), Quantity: 1}})
	f.repo.removeSupplyErr = errBoom
	assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, "s1"), errBoom)
	assert.Empty(t, f.catalog.released, "no release when the unlink fails: the stock stays reserved")
}

func TestRemoveSupplyRejections(t *testing.T) {
	ctx := context.Background()
	t.Run("blank id", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, " "), domain.ErrInvalidSupplyID)
	})
	t.Run("order not found", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, "s1"), domain.ErrServiceOrderNotFound)
	})
	t.Run("not in diagnosis", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_RECEIVED, nil, nil)
		assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, "s1"), domain.ErrServiceOrderNotInDiagnosis)
	})
	t.Run("supply not linked", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil, nil)
		assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, "s1"), domain.ErrServiceOrderSupplyNotFound)
	})
	t.Run("supplies lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil, nil)
		f.repo.listSuppliesErr = errBoom
		assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, "s1"), errBoom)
	})
	t.Run("unlink fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil,
			[]domain.SupplyItem{{ID: "s1", ReservationID: "res-1", UnitPrice: decimal.RequireFromString("1"), Quantity: 1}})
		f.repo.removeSupplyErr = errBoom
		assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, "s1"), errBoom)
	})
	t.Run("status changed in the transaction", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil,
			[]domain.SupplyItem{{ID: "s1", ReservationID: "res-1", UnitPrice: decimal.RequireFromString("1"), Quantity: 1}})
		f.repo.headerOverride = statusPtr(domain.SERVICE_ORDER_STATUS_RECEIVED)
		assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, "s1"), domain.ErrServiceOrderNotInDiagnosis)
	})
	t.Run("in-transaction lookup fails", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil, nil)
		f.repo.findErr = errBoom
		assert.ErrorIs(t, f.svc.RemoveSupply(ctx, soID, "s1"), errBoom)
	})
}

// ---- Local transitions (T4 rows 4-6) ----

func TestLocalTransitionsPublishStatusChanges(t *testing.T) {
	cases := []struct {
		name string
		from domain.SERVICE_ORDER_STATUS
		run  func(*Service, string) error
		to   domain.SERVICE_ORDER_STATUS
	}{
		{"received", domain.SERVICE_ORDER_STATUS_NEW, svcOp((*Service).Receive), domain.SERVICE_ORDER_STATUS_RECEIVED},
		{"start diagnosis", domain.SERVICE_ORDER_STATUS_RECEIVED, svcOp((*Service).StartDiagnosis), domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS},
		{"deliver", domain.SERVICE_ORDER_STATUS_COMPLETED, svcOp((*Service).Deliver), domain.SERVICE_ORDER_STATUS_DELIVERED},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.seed(t, tc.from, nil, nil)
			require.NoError(t, tc.run(f.svc, soID))
			assert.Equal(t, tc.to, f.status(t))
			assert.Equal(t, []string{"ServiceOrderStatusChanged"}, f.lastEventTypes())
			require.Len(t, f.notifier.calls, 1)
			assert.Equal(t, tc.to, f.notifier.calls[0].NewStatus)
		})
	}
}

// svcOp adapts a context-taking service method to the table used by the tests.
func svcOp(fn func(*Service, context.Context, string) error) func(*Service, string) error {
	return func(s *Service, id string) error { return fn(s, context.Background(), id) }
}

func TestLocalTransitionsRejectWrongStatusOrMissingOrder(t *testing.T) {
	type op struct {
		name string
		run  func(*Service, string) error
		want error
	}
	ops := []op{
		{"receive", svcOp((*Service).Receive), domain.ErrServiceOrderNotNew},
		{"start diagnosis", svcOp((*Service).StartDiagnosis), domain.ErrServiceOrderNotInReceived},
		{"deliver", svcOp((*Service).Deliver), domain.ErrServiceOrderNotCompleted},
	}
	for _, o := range ops {
		t.Run(o.name+" wrong status", func(t *testing.T) {
			f := newFixture(t)
			f.seed(t, domain.SERVICE_ORDER_STATUS_CANCELLED, nil, nil)
			assert.ErrorIs(t, o.run(f.svc, soID), o.want)
			assert.Empty(t, f.lastEventTypes())
		})
		t.Run(o.name+" missing order", func(t *testing.T) {
			f := newFixture(t)
			assert.ErrorIs(t, o.run(f.svc, soID), domain.ErrServiceOrderNotFound)
		})
	}
}

// ---- Saga delegation (T4 send/accept/reject/cancel) ----

func TestSendStartsTheBudgetSaga(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]}, nil)
	require.NoError(t, f.svc.Send(ctx, soID))
	assert.Equal(t, []string{"GenerateBudget"}, f.lastEventTypes())
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, f.status(t))
	assert.ErrorIs(t, f.svc.Send(ctx, soID), domain.ErrSagaAlreadyRunning)
}

// awaitApproval moves the saga and the order to the state a BudgetGenerated reply leaves them in.
func (f *fixture) awaitApproval(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.svc.Send(ctx, soID))
	inst, err := f.sagas.FindRunningBySO(ctx, soID)
	require.NoError(t, err)
	require.NotNil(t, inst)
	so, err := f.sos.FindByID(ctx, soID)
	require.NoError(t, err)
	so.Status = domain.SERVICE_ORDER_STATUS_AWAITING_APPROVAL
	require.NoError(t, f.sos.Update(ctx, &so))
	inst.Step = saga.StepAwaitingCustomer
	inst.Deadline = nil
	require.NoError(t, f.sagas.Update(ctx, inst))
	f.box.Sent = nil
}

func TestAcceptRejectAndCancelDelegateToTheSaga(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]}, nil)
	f.awaitApproval(t)

	assert.ErrorIs(t, f.svc.Accept(ctx, soID, "someone-else"), domain.ErrInvalidCustomerProperty)
	assert.ErrorIs(t, f.svc.Reject(ctx, soID, "someone-else"), domain.ErrInvalidCustomerProperty)

	require.NoError(t, f.svc.Accept(ctx, soID, customerUser))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_IN_PROGRESS, f.status(t))
	assert.Equal(t, []string{"ServiceOrderStatusChanged", "RequestPayment"}, f.lastEventTypes())

	require.NoError(t, f.svc.Cancel(ctx, soID))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_CANCELLED, f.status(t))
}

func TestRejectDelegatesToTheSaga(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]}, nil)
	f.awaitApproval(t)

	require.NoError(t, f.svc.Reject(ctx, soID, customerUser))
	assert.Equal(t, domain.SERVICE_ORDER_STATUS_REJECTED, f.status(t))
	assert.ErrorIs(t, f.svc.Cancel(ctx, soID), domain.ErrServiceOrderNotCancelable)
}

func TestCancelWithoutSagaReleasesSuppliesOnly(t *testing.T) {
	ctx := context.Background()
	t.Run("order with supplies releases them", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, nil,
			[]domain.SupplyItem{{ID: "s1", ReservationID: "res-9", UnitPrice: decimal.RequireFromString("1"), Quantity: 1}})
		require.NoError(t, f.svc.Cancel(ctx, soID))
		assert.Equal(t, domain.SERVICE_ORDER_STATUS_CANCELLED, f.status(t))
		// the stock is given back by the Catalog through the ReleaseStock command, not by a direct call
		assert.Equal(t, []string{"ServiceOrderStatusChanged", "ReleaseStock"}, f.lastEventTypes())
		assert.Empty(t, f.catalog.released)
	})
	t.Run("order not cancelable", func(t *testing.T) {
		f := newFixture(t)
		f.seed(t, domain.SERVICE_ORDER_STATUS_DELIVERED, nil, nil)
		assert.ErrorIs(t, f.svc.Cancel(ctx, soID), domain.ErrServiceOrderNotCancelable)
	})
	t.Run("order not found", func(t *testing.T) {
		f := newFixture(t)
		assert.ErrorIs(t, f.svc.Cancel(ctx, soID), domain.ErrServiceOrderNotFound)
	})
}

func TestExpireDeadlinesWithoutRunningSagas(t *testing.T) {
	f := newFixture(t)
	n, err := f.svc.ExpireDeadlines(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// ---- Reads (T4 GET rows) ----

func TestGetFullByIDAndStatusReadLocalDatabase(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_IN_DIAGNOSIS, []domain.WorkItem{f.catalog.works["w1"]},
		[]domain.SupplyItem{{ID: "s1", ReservationID: "r", Name: "Filtro", UnitPrice: decimal.RequireFromString("35.50"), Quantity: 2, Version: 3}})

	detail, err := f.svc.GetFullByID(ctx, soID)
	require.NoError(t, err)
	assert.Equal(t, soID, detail.ID)
	assert.Equal(t, "IN_DIAGNOSIS", detail.Status)
	assert.Equal(t, 221.0, detail.TotalValue)
	assert.Equal(t, "Fiat - Uno", detail.Vehicle.BrandModel)
	require.Len(t, detail.Works, 1)
	require.Len(t, detail.Supplies, 1)

	_, err = f.svc.GetFullByID(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrServiceOrderNotFound)

	status, err := f.svc.GetStatus(ctx, soID)
	require.NoError(t, err)
	assert.Equal(t, adapters.SOResponse{ID: soID, Status: "IN_DIAGNOSIS"}, status)

	_, err = f.svc.GetStatus(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrServiceOrderNotFound)
}

func TestGetFullByIDFailsWhenAggregateIsIncomplete(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
	f.repo.listWorksErr = errBoom
	_, err := f.svc.GetFullByID(ctx, soID)
	assert.ErrorIs(t, err, errBoom)
}

func TestListFiltersPaginatesAndHidesTerminalOrders(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
	require.NoError(t, f.sos.Insert(ctx, &domain.ServiceOrder{ID: "so-2", Status: domain.SERVICE_ORDER_STATUS_CANCELLED,
		Customer: domain.CustomerSnapshot{ID: customerID}, Vehicle: domain.VehicleSnapshot{ID: vehicleID}}))
	require.NoError(t, f.sos.Insert(ctx, &domain.ServiceOrder{ID: "so-3", Status: domain.SERVICE_ORDER_STATUS_RECEIVED,
		Customer: domain.CustomerSnapshot{ID: "other"}, Vehicle: domain.VehicleSnapshot{ID: "v9"}}))

	page, err := f.svc.List(ctx, adapters.SOFilterParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.TotalItems, "terminal orders are hidden by default")
	assert.Equal(t, int64(1), page.TotalPages)

	byCustomer, err := f.svc.List(ctx, adapters.SOFilterParams{Page: 1, PageSize: 10, CustomerID: customerID})
	require.NoError(t, err)
	assert.Equal(t, int64(1), byCustomer.TotalItems)

	byStatus, err := f.svc.List(ctx, adapters.SOFilterParams{Page: 1, PageSize: 10, Status: "CANCELLED"})
	require.NoError(t, err)
	require.Len(t, byStatus.Items, 1)
	assert.Equal(t, "so-2", byStatus.Items[0].ID)

	paged, err := f.svc.List(ctx, adapters.SOFilterParams{Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, paged.Items, 1)
	assert.Equal(t, int64(2), paged.TotalPages)
	assert.Equal(t, int64(2), paged.Page)
}

func TestListFailsWhenCountOrSearchFails(t *testing.T) {
	f := newFixture(t)
	f.repo.countErr = errBoom
	_, err := f.svc.List(ctx, adapters.SOFilterParams{Page: 1, PageSize: 10})
	assert.ErrorIs(t, err, errBoom)

	f.repo.countErr = nil
	f.repo.searchErr = errBoom
	_, err = f.svc.List(ctx, adapters.SOFilterParams{Page: 1, PageSize: 10})
	assert.ErrorIs(t, err, errBoom)
}

func TestExistsChecksCustomerOrVehicle(t *testing.T) {
	f := newFixture(t)
	f.seed(t, domain.SERVICE_ORDER_STATUS_NEW, nil, nil)
	found, err := f.svc.Exists(ctx, customerID, "")
	require.NoError(t, err)
	assert.True(t, found)
	found, err = f.svc.Exists(ctx, "", "nope")
	require.NoError(t, err)
	assert.False(t, found)

	f.repo.existsErr = errBoom
	_, err = f.svc.Exists(ctx, customerID, "")
	assert.ErrorIs(t, err, errBoom)
}

func TestReleaseAllIgnoresMissingReservations(t *testing.T) {
	f := newFixture(t)
	f.catalog.reservation = map[string]reservedRow{}
	f.svc.releaseAll(ctx, []domain.SupplyItem{{ReservationID: "gone"}})
	assert.Equal(t, []string{"gone"}, f.catalog.released)
	f.catalog.releaseErr = catalog.ErrReservationNotFound
	f.svc.releaseAll(ctx, []domain.SupplyItem{{ReservationID: "gone2"}})
	assert.Equal(t, []string{"gone", "gone2"}, f.catalog.released)
}
