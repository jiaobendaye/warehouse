package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// newStockSvc builds a StockService with a fresh test DB and one accessory
// pre-created. Returns the service, the accessory repo (for stock assertions),
// the flow repo (for flow counting assertions), and a cleanup func.
func newStockSvc(t *testing.T) (*service.StockService, *repo.AccessoryRepo, *repo.FlowRepo, *sql.DB, func()) {
	t.Helper()
	d, dbCleanup := newTestDB(t)
	acc := repo.NewAccessoryRepo(d)
	flow := repo.NewFlowRepo(d)
	svc := service.NewStockService(acc, flow, d)
	return svc, acc, flow, d, dbCleanup
}

func seedAccessoryWithStock(t *testing.T, acc *repo.AccessoryRepo, name string, stock int64) domain.Accessory {
	t.Helper()
	a, err := acc.Create(context.Background(), domain.Accessory{
		Name: name,
	})
	if err != nil {
		t.Fatalf("seed create %s: %v", name, err)
	}
	if stock != 0 {
		// SetStock with nil tx writes via the underlying *sql.DB. Test-only
		// scaffolding; production code goes through service transactions.
		if err := acc.SetStock(context.Background(), nil, a.ID, stock); err != nil {
			t.Fatalf("seed set stock %s: %v", name, err)
		}
	}
	return a
}

// helper: current stock for a given accessory (read after writes).
func currentStock(t *testing.T, acc *repo.AccessoryRepo, id int64) int64 {
	t.Helper()
	a, err := acc.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("read stock: %v", err)
	}
	return a.CurrentStock
}

// helper: count of flow rows for an accessory.
func flowCount(t *testing.T, flow *repo.FlowRepo, accessoryID int64) int {
	t.Helper()
	n, err := flow.CountByAccessory(context.Background(), accessoryID)
	if err != nil {
		t.Fatalf("count flows: %v", err)
	}
	return n
}

// --- Single Inbound ----------------------------------------------------

func TestStockService_Inbound_Happy(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "IN-1", 0)

	got, err := svc.Inbound(ctx, service.InboundCmd{
		AccessoryID: a.ID,
		Quantity:    10,
		UnitCost:    5.5,
		Remark:      "first batch",
	})
	if err != nil {
		t.Fatalf("Inbound: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero flow ID")
	}
	if got.Type != domain.FlowTypeIn {
		t.Fatalf("expected type=in, got %q", got.Type)
	}
	if got.BalanceAfter != 10 {
		t.Fatalf("expected balance_after=10, got %d", got.BalanceAfter)
	}
	if got.Quantity != 10 {
		t.Fatalf("expected quantity=10, got %d", got.Quantity)
	}
	if got.UnitCost != 5.5 {
		t.Fatalf("expected unit_cost=5.5, got %v", got.UnitCost)
	}
	if got.OccurredAt == "" {
		t.Fatal("expected occurred_at populated")
	}
	if got.Remark != "first batch" {
		t.Fatalf("expected remark set, got %q", got.Remark)
	}
	// Side effects
	if s := currentStock(t, acc, a.ID); s != 10 {
		t.Fatalf("expected stock=10, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 1 {
		t.Fatalf("expected 1 flow row, got %d", n)
	}
}

func TestStockService_Inbound_BalanceAfterReflectsPostOp(t *testing.T) {
	svc, acc, _, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "IN-BA", 7)

	got, err := svc.Inbound(ctx, service.InboundCmd{
		AccessoryID: a.ID,
		Quantity:    3,
	})
	if err != nil {
		t.Fatalf("Inbound: %v", err)
	}
	if got.BalanceAfter != 10 {
		t.Fatalf("expected balance_after=10, got %d", got.BalanceAfter)
	}
	if s := currentStock(t, acc, a.ID); s != 10 {
		t.Fatalf("expected stock=10, got %d", s)
	}
}

func TestStockService_Inbound_ZeroQty_Rejected(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "IN-Z", 5)

	for _, q := range []int64{0, -1, -100} {
		_, err := svc.Inbound(ctx, service.InboundCmd{
			AccessoryID: a.ID,
			Quantity:    q,
		})
		if !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("qty=%d: expected ErrInvalidInput, got %v", q, err)
		}
	}
	// No state change
	if s := currentStock(t, acc, a.ID); s != 5 {
		t.Fatalf("stock should be unchanged at 5, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 0 {
		t.Fatalf("expected 0 flow rows, got %d", n)
	}
}

func TestStockService_Inbound_NotFound(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "IN-NF", 0)

	_, err := svc.Inbound(ctx, service.InboundCmd{
		AccessoryID: 99999,
		Quantity:    3,
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// No state change to the seeded accessory
	if s := currentStock(t, acc, a.ID); s != 0 {
		t.Fatalf("unrelated stock should be unchanged, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 0 {
		t.Fatalf("expected 0 flow rows, got %d", n)
	}
}

func TestStockService_Inbound_NoClientRef_FirstCallInserts(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "IN-NCR", 0)

	got, err := svc.Inbound(ctx, service.InboundCmd{
		AccessoryID: a.ID,
		Quantity:    4,
	})
	if err != nil {
		t.Fatalf("Inbound: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected inserted flow ID")
	}
	if got.ClientRef != "" {
		t.Fatalf("expected empty client_ref in returned flow, got %q", got.ClientRef)
	}
	if s := currentStock(t, acc, a.ID); s != 4 {
		t.Fatalf("expected stock=4, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 1 {
		t.Fatalf("expected 1 flow row, got %d", n)
	}
}

func TestStockService_Inbound_ClientRef_FirstAndSecondCalls(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "IN-CR", 0)

	first, err := svc.Inbound(ctx, service.InboundCmd{
		AccessoryID: a.ID,
		Quantity:    5,
		ClientRef:   "ref-IN-CR-1",
	})
	if err != nil {
		t.Fatalf("first Inbound: %v", err)
	}
	if first.ID == 0 {
		t.Fatal("expected first flow ID")
	}
	if first.ClientRef != "ref-IN-CR-1" {
		t.Fatalf("expected client_ref preserved, got %q", first.ClientRef)
	}
	if first.BalanceAfter != 5 {
		t.Fatalf("expected balance_after=5, got %d", first.BalanceAfter)
	}

	// Second call with same client_ref must be idempotent.
	second, err := svc.Inbound(ctx, service.InboundCmd{
		AccessoryID: a.ID,
		Quantity:    999, // would normally blow past stock — but must be ignored
		ClientRef:   "ref-IN-CR-1",
	})
	if err != nil {
		t.Fatalf("second Inbound (idempotent): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same flow ID %d, got %d", first.ID, second.ID)
	}
	if second.BalanceAfter != first.BalanceAfter {
		t.Fatalf("expected same balance_after %d, got %d",
			first.BalanceAfter, second.BalanceAfter)
	}
	// Stock must be unchanged after the duplicate call.
	if s := currentStock(t, acc, a.ID); s != 5 {
		t.Fatalf("stock must not change on idempotent re-submit, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 1 {
		t.Fatalf("expected 1 flow row, got %d", n)
	}
}

func TestStockService_Inbound_DifferentClientRef_InsertsNew(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "IN-DCR", 0)

	if _, err := svc.Inbound(ctx, service.InboundCmd{AccessoryID: a.ID, Quantity: 2, ClientRef: "r1"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.Inbound(ctx, service.InboundCmd{AccessoryID: a.ID, Quantity: 3, ClientRef: "r2"}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if s := currentStock(t, acc, a.ID); s != 5 {
		t.Fatalf("expected stock=5, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 2 {
		t.Fatalf("expected 2 flow rows, got %d", n)
	}
}

// --- Single Outbound ---------------------------------------------------

func TestStockService_Outbound_Happy(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "OUT-1", 10)

	got, err := svc.Outbound(ctx, service.OutboundCmd{
		AccessoryID: a.ID,
		Quantity:    3,
		UnitPrice:   8.0,
		Remark:      "sold to customer A",
	})
	if err != nil {
		t.Fatalf("Outbound: %v", err)
	}
	if got.Type != domain.FlowTypeOut {
		t.Fatalf("expected type=out, got %q", got.Type)
	}
	if got.BalanceAfter != 7 {
		t.Fatalf("expected balance_after=7, got %d", got.BalanceAfter)
	}
	if got.Quantity != 3 {
		t.Fatalf("expected quantity=3, got %d", got.Quantity)
	}
	if got.UnitPrice != 8.0 {
		t.Fatalf("expected unit_price=8.0, got %v", got.UnitPrice)
	}
	if s := currentStock(t, acc, a.ID); s != 7 {
		t.Fatalf("expected stock=7, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 1 {
		t.Fatalf("expected 1 flow row, got %d", n)
	}
}

func TestStockService_Outbound_InsufficientStock_NoStateChange(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "OUT-IS", 5)

	_, err := svc.Outbound(ctx, service.OutboundCmd{
		AccessoryID: a.ID,
		Quantity:    6,
	})
	if !errors.Is(err, service.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	if s := currentStock(t, acc, a.ID); s != 5 {
		t.Fatalf("stock must be unchanged on insufficient, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 0 {
		t.Fatalf("expected 0 flow rows on insufficient, got %d", n)
	}
}

func TestStockService_Outbound_StockEqualsQty_OK(t *testing.T) {
	svc, acc, _, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "OUT-EQ", 7)

	got, err := svc.Outbound(ctx, service.OutboundCmd{
		AccessoryID: a.ID,
		Quantity:    7,
	})
	if err != nil {
		t.Fatalf("Outbound with stock==qty should succeed, got %v", err)
	}
	if got.BalanceAfter != 0 {
		t.Fatalf("expected balance_after=0, got %d", got.BalanceAfter)
	}
	if s := currentStock(t, acc, a.ID); s != 0 {
		t.Fatalf("expected stock=0, got %d", s)
	}
}

func TestStockService_Outbound_ZeroQty_Rejected(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "OUT-Z", 5)

	for _, q := range []int64{0, -1} {
		_, err := svc.Outbound(ctx, service.OutboundCmd{
			AccessoryID: a.ID,
			Quantity:    q,
		})
		if !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("qty=%d: expected ErrInvalidInput, got %v", q, err)
		}
	}
	if s := currentStock(t, acc, a.ID); s != 5 {
		t.Fatalf("stock should be unchanged, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 0 {
		t.Fatalf("expected 0 flow rows, got %d", n)
	}
}

func TestStockService_Outbound_NotFound(t *testing.T) {
	svc, _, _, _, cleanup := newStockSvc(t)
	defer cleanup()

	_, err := svc.Outbound(context.Background(), service.OutboundCmd{
		AccessoryID: 88888,
		Quantity:    1,
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStockService_Outbound_ClientRef_Idempotent(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "OUT-CR", 10)

	first, err := svc.Outbound(ctx, service.OutboundCmd{
		AccessoryID: a.ID,
		Quantity:    4,
		ClientRef:   "ref-OUT-CR-1",
	})
	if err != nil {
		t.Fatalf("first Outbound: %v", err)
	}
	if first.BalanceAfter != 6 {
		t.Fatalf("expected balance_after=6, got %d", first.BalanceAfter)
	}

	second, err := svc.Outbound(ctx, service.OutboundCmd{
		AccessoryID: a.ID,
		Quantity:    999, // must be ignored on idempotent re-submit
		ClientRef:   "ref-OUT-CR-1",
	})
	if err != nil {
		t.Fatalf("second (idempotent): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same flow ID, got %d vs %d", first.ID, second.ID)
	}
	if second.BalanceAfter != first.BalanceAfter {
		t.Fatalf("balance_after must match first, got %d vs %d",
			second.BalanceAfter, first.BalanceAfter)
	}
	if s := currentStock(t, acc, a.ID); s != 6 {
		t.Fatalf("stock must not change on idempotent re-submit, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 1 {
		t.Fatalf("expected 1 flow row, got %d", n)
	}
}

// --- Batch Inbound -----------------------------------------------------

func TestStockService_BatchInbound_AllSucceed(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a1 := seedAccessoryWithStock(t, acc, "BI-1", 0)
	a2 := seedAccessoryWithStock(t, acc, "BI-2", 5)
	a3 := seedAccessoryWithStock(t, acc, "BI-3", 0)

	res, err := svc.BatchInbound(ctx, []service.InboundCmd{
		{AccessoryID: a1.ID, Quantity: 10, UnitCost: 1.0},
		{AccessoryID: a2.ID, Quantity: 3, UnitCost: 2.0},
		{AccessoryID: a3.ID, Quantity: 5, UnitCost: 1.0},
	})
	if err != nil {
		t.Fatalf("BatchInbound: %v", err)
	}
	if res.Accepted != 3 {
		t.Fatalf("expected accepted=3, got %d", res.Accepted)
	}
	if len(res.IDs) != 3 {
		t.Fatalf("expected 3 flow IDs, got %d", len(res.IDs))
	}
	if len(res.Flows) != 3 {
		t.Fatalf("expected 3 flows, got %d", len(res.Flows))
	}
	// Side effects
	if s := currentStock(t, acc, a1.ID); s != 10 {
		t.Fatalf("a1 stock: expected 10, got %d", s)
	}
	if s := currentStock(t, acc, a2.ID); s != 8 {
		t.Fatalf("a2 stock: expected 8, got %d", s)
	}
	if s := currentStock(t, acc, a3.ID); s != 5 {
		t.Fatalf("a3 stock: expected 5, got %d", s)
	}
	if n := flowCount(t, fr, a1.ID); n != 1 {
		t.Fatalf("a1 flows: expected 1, got %d", n)
	}
	if n := flowCount(t, fr, a2.ID); n != 1 {
		t.Fatalf("a2 flows: expected 1, got %d", n)
	}
	if n := flowCount(t, fr, a3.ID); n != 1 {
		t.Fatalf("a3 flows: expected 1, got %d", n)
	}
	// balance_after ordering
	if res.Flows[0].BalanceAfter != 10 {
		t.Fatalf("flows[0] balance_after: expected 10, got %d", res.Flows[0].BalanceAfter)
	}
	if res.Flows[1].BalanceAfter != 8 {
		t.Fatalf("flows[1] balance_after: expected 8, got %d", res.Flows[1].BalanceAfter)
	}
	if res.Flows[2].BalanceAfter != 5 {
		t.Fatalf("flows[2] balance_after: expected 5, got %d", res.Flows[2].BalanceAfter)
	}
}

func TestStockService_BatchInbound_OneBadRow_AllRolledBack(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a1 := seedAccessoryWithStock(t, acc, "BI-RB-1", 0)
	a2 := seedAccessoryWithStock(t, acc, "BI-RB-2", 0)
	a3 := seedAccessoryWithStock(t, acc, "BI-RB-3", 0)

	_, err := svc.BatchInbound(ctx, []service.InboundCmd{
		{AccessoryID: a1.ID, Quantity: 10},
		{AccessoryID: a2.ID, Quantity: 5},
		{AccessoryID: a3.ID, Quantity: 0}, // bad: zero qty
	})
	if err == nil {
		t.Fatal("expected error from BatchInbound with bad row, got nil")
	}
	// No state change on any accessory.
	if s := currentStock(t, acc, a1.ID); s != 0 {
		t.Fatalf("a1 stock should be unchanged, got %d", s)
	}
	if s := currentStock(t, acc, a2.ID); s != 0 {
		t.Fatalf("a2 stock should be unchanged, got %d", s)
	}
	if s := currentStock(t, acc, a3.ID); s != 0 {
		t.Fatalf("a3 stock should be unchanged, got %d", s)
	}
	// No flow rows inserted.
	if n := flowCount(t, fr, a1.ID); n != 0 {
		t.Fatalf("a1 flows should be 0, got %d", n)
	}
	if n := flowCount(t, fr, a2.ID); n != 0 {
		t.Fatalf("a2 flows should be 0, got %d", n)
	}
	if n := flowCount(t, fr, a3.ID); n != 0 {
		t.Fatalf("a3 flows should be 0, got %d", n)
	}
}

func TestStockService_BatchInbound_NotFoundRow_RollsBack(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a1 := seedAccessoryWithStock(t, acc, "BI-NF-1", 0)

	_, err := svc.BatchInbound(ctx, []service.InboundCmd{
		{AccessoryID: a1.ID, Quantity: 4},
		{AccessoryID: 99999, Quantity: 1}, // not found
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if s := currentStock(t, acc, a1.ID); s != 0 {
		t.Fatalf("a1 stock should be unchanged, got %d", s)
	}
	if n := flowCount(t, fr, a1.ID); n != 0 {
		t.Fatalf("a1 flows should be 0, got %d", n)
	}
}

func TestStockService_BatchInbound_DuplicateAccessoryID_Rejected(t *testing.T) {
	// Spec rule: duplicate accessory_id within a batch is a 400 with failing index.
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a1 := seedAccessoryWithStock(t, acc, "BI-DUP-1", 0)

	_, err := svc.BatchInbound(ctx, []service.InboundCmd{
		{AccessoryID: a1.ID, Quantity: 3},
		{AccessoryID: a1.ID, Quantity: 2}, // duplicate within batch
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if s := currentStock(t, acc, a1.ID); s != 0 {
		t.Fatalf("stock should be unchanged, got %d", s)
	}
	if n := flowCount(t, fr, a1.ID); n != 0 {
		t.Fatalf("flows should be 0, got %d", n)
	}
}

// --- Batch Outbound ----------------------------------------------------

func TestStockService_BatchOutbound_AllSucceed(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a1 := seedAccessoryWithStock(t, acc, "BO-1", 20)
	a2 := seedAccessoryWithStock(t, acc, "BO-2", 5)

	res, err := svc.BatchOutbound(ctx, []service.OutboundCmd{
		{AccessoryID: a1.ID, Quantity: 8, UnitPrice: 10},
		{AccessoryID: a2.ID, Quantity: 5, UnitPrice: 20},
	})
	if err != nil {
		t.Fatalf("BatchOutbound: %v", err)
	}
	if res.Accepted != 2 {
		t.Fatalf("expected accepted=2, got %d", res.Accepted)
	}
	if s := currentStock(t, acc, a1.ID); s != 12 {
		t.Fatalf("a1 stock: expected 12, got %d", s)
	}
	if s := currentStock(t, acc, a2.ID); s != 0 {
		t.Fatalf("a2 stock: expected 0, got %d", s)
	}
	if n := flowCount(t, fr, a1.ID); n != 1 {
		t.Fatalf("a1 flows: expected 1, got %d", n)
	}
	if n := flowCount(t, fr, a2.ID); n != 1 {
		t.Fatalf("a2 flows: expected 1, got %d", n)
	}
}

func TestStockService_BatchOutbound_InsufficientOnRow3_AllRolledBack(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a1 := seedAccessoryWithStock(t, acc, "BO-RB-1", 10)
	a2 := seedAccessoryWithStock(t, acc, "BO-RB-2", 10)
	a3 := seedAccessoryWithStock(t, acc, "BO-RB-3", 10)

	_, err := svc.BatchOutbound(ctx, []service.OutboundCmd{
		{AccessoryID: a1.ID, Quantity: 3},
		{AccessoryID: a2.ID, Quantity: 4},
		{AccessoryID: a3.ID, Quantity: 999}, // insufficient
	})
	if !errors.Is(err, service.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	// All stocks unchanged.
	if s := currentStock(t, acc, a1.ID); s != 10 {
		t.Fatalf("a1 stock must be unchanged, got %d", s)
	}
	if s := currentStock(t, acc, a2.ID); s != 10 {
		t.Fatalf("a2 stock must be unchanged, got %d", s)
	}
	if s := currentStock(t, acc, a3.ID); s != 10 {
		t.Fatalf("a3 stock must be unchanged, got %d", s)
	}
	// No flow rows written.
	for _, id := range []int64{a1.ID, a2.ID, a3.ID} {
		if n := flowCount(t, fr, id); n != 0 {
			t.Fatalf("accessory %d: expected 0 flows, got %d", id, n)
		}
	}
}

func TestStockService_BatchOutbound_BadQty_RollsBack(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a1 := seedAccessoryWithStock(t, acc, "BO-BQ-1", 10)

	_, err := svc.BatchOutbound(ctx, []service.OutboundCmd{
		{AccessoryID: a1.ID, Quantity: 1},
		{AccessoryID: a1.ID, Quantity: -1},
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if s := currentStock(t, acc, a1.ID); s != 10 {
		t.Fatalf("stock should be unchanged, got %d", s)
	}
	if n := flowCount(t, fr, a1.ID); n != 0 {
		t.Fatalf("expected 0 flows, got %d", n)
	}
}

// --- Concurrent idempotency (sequential demonstration) -----------------
//
// Note: Batch 4 tests run sequentially. Real concurrent idempotency would
// require a SQL-level unique constraint check; the schema enforces this via
// uq_flow_client_ref. Here we demonstrate that the application layer always
// returns the original flow and never writes a duplicate when two sequential
// calls share the same client_ref — that mirrors what a serialized retry
// stream would observe under network retries.

func TestStockService_Inbound_ConcurrentIdempotency_Sequential(t *testing.T) {
	svc, acc, fr, _, cleanup := newStockSvc(t)
	defer cleanup()
	ctx := context.Background()

	a := seedAccessoryWithStock(t, acc, "IN-CC", 0)

	first, err := svc.Inbound(ctx, service.InboundCmd{
		AccessoryID: a.ID,
		Quantity:    7,
		ClientRef:   "shared-ref",
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	// 5 retries, all with the same client_ref.
	var last domain.InventoryFlow
	for i := 0; i < 5; i++ {
		got, err := svc.Inbound(ctx, service.InboundCmd{
			AccessoryID: a.ID,
			Quantity:    7 + int64(i), // must be ignored
			ClientRef:   "shared-ref",
		})
		if err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		if got.ID != first.ID {
			t.Fatalf("retry %d: expected same ID %d, got %d", i, first.ID, got.ID)
		}
		if got.BalanceAfter != first.BalanceAfter {
			t.Fatalf("retry %d: balance_after must match, got %d vs %d",
				i, got.BalanceAfter, first.BalanceAfter)
		}
		last = got
	}
	if last.BalanceAfter != 7 {
		t.Fatalf("final balance_after must be 7, got %d", last.BalanceAfter)
	}
	if s := currentStock(t, acc, a.ID); s != 7 {
		t.Fatalf("stock must be 7 after retries, got %d", s)
	}
	if n := flowCount(t, fr, a.ID); n != 1 {
		t.Fatalf("expected exactly 1 flow row after 6 calls, got %d", n)
	}
}