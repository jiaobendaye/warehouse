package repo_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
)

// seedAccessory creates one accessory to attach flows to.
func seedAccessory(t *testing.T, d *sql.DB) domain.Accessory {
	t.Helper()
	a, err := repo.NewAccessoryRepo(d).Create(context.Background(), domain.Accessory{
		SKU: "F-1", Name: "x",
	})
	if err != nil {
		t.Fatalf("seed accessory: %v", err)
	}
	return a
}

// TestFlowRepo_InsertAndGetByID verifies a flow can be inserted and looked up.
func TestFlowRepo_InsertAndGetByID(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	a := seedAccessory(t, d)
	r := repo.NewFlowRepo(d)
	ctx := context.Background()

	in := domain.InventoryFlow{
		AccessoryID:  a.ID,
		Type:         domain.FlowTypeIn,
		Quantity:     10,
		UnitCost:     5.5,
		BalanceAfter: 10,
		OccurredAt:   "2026-07-01T10:00:00Z",
	}
	id, err := r.Insert(ctx, nil, in)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := r.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Quantity != 10 {
		t.Fatalf("expected qty 10, got %d", got.Quantity)
	}
	if got.Type != domain.FlowTypeIn {
		t.Fatalf("expected type in, got %q", got.Type)
	}
}

// TestFlowRepo_ClientRefIdempotent verifies inserting with the same client_ref
// returns an error so callers can detect duplicates.
func TestFlowRepo_ClientRefIdempotent(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	a := seedAccessory(t, d)
	r := repo.NewFlowRepo(d)
	ctx := context.Background()

	flow := domain.InventoryFlow{
		AccessoryID:  a.ID,
		Type:         domain.FlowTypeIn,
		Quantity:     1,
		BalanceAfter: 1,
		ClientRef:    "abc",
	}
	if _, err := r.Insert(ctx, nil, flow); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if _, err := r.Insert(ctx, nil, flow); err == nil {
		t.Fatal("expected duplicate-client_ref error")
	}

	existing, err := r.GetByClientRef(ctx, "abc")
	if err != nil {
		t.Fatalf("GetByClientRef: %v", err)
	}
	if existing.Quantity != 1 {
		t.Fatalf("expected existing qty 1, got %d", existing.Quantity)
	}
}

// TestFlowRepo_ListByAccessoryAndType verifies filtering by accessory + type.
func TestFlowRepo_ListByAccessoryAndType(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	a := seedAccessory(t, d)
	r := repo.NewFlowRepo(d)
	ctx := context.Background()

	in1 := domain.InventoryFlow{AccessoryID: a.ID, Type: domain.FlowTypeIn, Quantity: 5, BalanceAfter: 5, OccurredAt: "2026-07-01T00:00:00Z"}
	in2 := domain.InventoryFlow{AccessoryID: a.ID, Type: domain.FlowTypeOut, Quantity: 2, BalanceAfter: 3, OccurredAt: "2026-07-02T00:00:00Z"}
	in3 := domain.InventoryFlow{AccessoryID: a.ID, Type: domain.FlowTypeIn, Quantity: 7, BalanceAfter: 10, OccurredAt: "2026-07-03T00:00:00Z"}
	for _, f := range []domain.InventoryFlow{in1, in2, in3} {
		if _, err := r.Insert(ctx, nil, f); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	filter := domain.FlowFilter{
		AccessoryID: a.ID,
		Type:        domain.FlowTypeIn,
	}
	rows, total, err := r.List(ctx, filter, 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2 for type=in, got %d", total)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// Verify ascending occurred_at order
	first, _ := time.Parse(time.RFC3339Nano, rows[0].OccurredAt)
	second, _ := time.Parse(time.RFC3339Nano, rows[1].OccurredAt)
	if !first.Before(second) {
		t.Fatalf("expected ascending order, got %s then %s", rows[0].OccurredAt, rows[1].OccurredAt)
	}
}

// TestFlowRepo_ListTimeRange verifies occurred_at range filtering.
func TestFlowRepo_ListTimeRange(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	a := seedAccessory(t, d)
	r := repo.NewFlowRepo(d)
	ctx := context.Background()

	in := []domain.InventoryFlow{
		{AccessoryID: a.ID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 1, OccurredAt: "2026-06-01T00:00:00Z"},
		{AccessoryID: a.ID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 2, OccurredAt: "2026-06-15T00:00:00Z"},
		{AccessoryID: a.ID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 3, OccurredAt: "2026-07-01T00:00:00Z"},
		{AccessoryID: a.ID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 4, OccurredAt: "2026-07-15T00:00:00Z"},
	}
	for _, f := range in {
		if _, err := r.Insert(ctx, nil, f); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	filter := domain.FlowFilter{
		AccessoryID: a.ID,
		From:        "2026-07-01T00:00:00Z",
		To:          "2026-07-31T00:00:00Z",
	}
	rows, total, err := r.List(ctx, filter, 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2 in July, got %d", total)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows in July, got %d", len(rows))
	}
}

// TestFlowRepo_CountByAccessory verifies count is 0 for unused accessory.
func TestFlowRepo_CountByAccessory(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	a := seedAccessory(t, d)
	r := repo.NewFlowRepo(d)
	n, err := r.CountByAccessory(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("CountByAccessory: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}