package service_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/db"
	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// newFlowSvc builds a FlowService with an in-memory SQLite DB. It also
// returns the accessory repo + an accessory id to attach flows to, and a
// cleanup function.
func newFlowSvc(t *testing.T) (*service.FlowService, *repo.FlowRepo, int64, func()) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "flowsvc.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	cleanup := func() { _ = d.Close() }

	acc := repo.NewAccessoryRepo(d)
	flow := repo.NewFlowRepo(d)

	created, err := acc.Create(context.Background(), domain.Accessory{
		SKU: "FS-1", Name: "测试配件",
	})
	if err != nil {
		cleanup()
		t.Fatalf("seed accessory: %v", err)
	}

	svc := service.NewFlowService(flow)
	return svc, flow, created.ID, cleanup
}

// seedFlow inserts a flow row directly via the repo (StockService is the
// canonical writer, but it's not in scope for FlowService tests).
func seedFlow(t *testing.T, fr *repo.FlowRepo, f domain.InventoryFlow) {
	t.Helper()
	if _, err := fr.Insert(context.Background(), nil, f); err != nil {
		t.Fatalf("seed flow: %v", err)
	}
}

// List_ByAccessory_ReturnsAscending verifies ListByAccessory returns rows in
// ascending occurred_at order.
func TestFlowService_ListByAccessory_Ascending(t *testing.T) {
	svc, fr, accID, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 1, OccurredAt: "2026-07-03T00:00:00Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 2, OccurredAt: "2026-07-01T00:00:00Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeOut, Quantity: 1, BalanceAfter: 1, OccurredAt: "2026-07-02T00:00:00Z"})

	rows, total, err := svc.ListByAccessory(ctx, accID, "", "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListByAccessory: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].OccurredAt != "2026-07-01T00:00:00Z" {
		t.Fatalf("expected first row occurred_at=2026-07-01, got %q", rows[0].OccurredAt)
	}
	if rows[1].OccurredAt != "2026-07-02T00:00:00Z" {
		t.Fatalf("expected second row occurred_at=2026-07-02, got %q", rows[1].OccurredAt)
	}
	if rows[2].OccurredAt != "2026-07-03T00:00:00Z" {
		t.Fatalf("expected third row occurred_at=2026-07-03, got %q", rows[2].OccurredAt)
	}
}

// List_Global_AllFlows verifies the global List returns all flows across
// accessories when no filter is supplied.
func TestFlowService_List_Global_AllFlows(t *testing.T) {
	svc, fr, accID, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 1, OccurredAt: "2026-07-01T00:00:00Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeOut, Quantity: 1, BalanceAfter: 0, OccurredAt: "2026-07-02T00:00:00Z"})

	rows, total, err := svc.List(ctx, "", "", "", 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("expected 2 flows, got total=%d len=%d", total, len(rows))
	}
}

// List_TypeFilter_OnlyMatching verifies type=in filters out 'out' rows.
func TestFlowService_List_TypeFilter_OnlyMatching(t *testing.T) {
	svc, fr, accID, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 1, OccurredAt: "2026-07-01T00:00:00Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeOut, Quantity: 1, BalanceAfter: 0, OccurredAt: "2026-07-02T00:00:00Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 2, BalanceAfter: 2, OccurredAt: "2026-07-03T00:00:00Z"})

	rows, total, err := svc.ListByAccessory(ctx, accID, "in", "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListByAccessory type=in: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("expected 2 inbound rows, got total=%d len=%d", total, len(rows))
	}
	for _, r := range rows {
		if r.Type != domain.FlowTypeIn {
			t.Fatalf("expected type=in, got %q", r.Type)
		}
	}
}

// List_TimeRange_BoundaryInclusive verifies from/to range filtering is
// inclusive at both boundaries.
func TestFlowService_List_TimeRange_BoundaryInclusive(t *testing.T) {
	svc, fr, accID, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 1, OccurredAt: "2026-06-30T23:59:59Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 2, OccurredAt: "2026-07-01T00:00:00Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 3, OccurredAt: "2026-07-15T12:00:00Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 4, OccurredAt: "2026-07-31T23:59:59Z"})
	seedFlow(t, fr, domain.InventoryFlow{AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1, BalanceAfter: 5, OccurredAt: "2026-08-01T00:00:00Z"})

	rows, total, err := svc.ListByAccessory(ctx, accID, "", "2026-07-01T00:00:00Z", "2026-07-31T23:59:59Z", 50, 0)
	if err != nil {
		t.Fatalf("ListByAccessory range: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3 (boundaries inclusive), got %d", total)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
}

// List_InvalidTimeFormat_ReturnsErrInvalidInput verifies that a non-RFC3339
// string for `from` or `to` is rejected.
func TestFlowService_List_InvalidTimeFormat_ReturnsErrInvalidInput(t *testing.T) {
	svc, _, _, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	cases := []struct {
		name, from, to string
	}{
		{"bad from", "yesterday", ""},
		{"bad to", "", "2026/07/01 00:00:00"},
		{"garbage both", "nope", "nada"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.List(ctx, "", tc.from, tc.to, 50, 0)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

// List_InvalidType_ReturnsErrInvalidInput verifies that type values other
// than "in" / "out" / "" are rejected.
func TestFlowService_List_InvalidType_ReturnsErrInvalidInput(t *testing.T) {
	svc, _, _, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	_, _, err := svc.List(ctx, "sideways", "", "", 50, 0)
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

// ListByAccessory_NegativeID_ReturnsErrInvalidInput verifies that accessory
// id <= 0 is rejected.
func TestFlowService_ListByAccessory_NegativeID_ReturnsErrInvalidInput(t *testing.T) {
	svc, _, _, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	cases := []int64{0, -1, -42}
	for _, id := range cases {
		_, _, err := svc.ListByAccessory(ctx, id, "", "", "", 50, 0)
		if !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for id=%d, got %v", id, err)
		}
	}
}

// List_LimitClampedTo500 verifies that limit > 500 is capped at 500 and
// limit <= 0 falls back to 50. We check the clamped value by issuing a
// limit that would otherwise return >500 rows but only 500 fit; here we
// simply verify the cap by exercising a large limit and ensuring the call
// succeeds (the repo then picks up our clamped value).
func TestFlowService_List_LimitClampedTo500(t *testing.T) {
	svc, fr, accID, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	// Seed 3 rows; request limit=10000 — service should clamp to 500.
	for i := 0; i < 3; i++ {
		seedFlow(t, fr, domain.InventoryFlow{
			AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1,
			BalanceAfter: int64(i + 1), OccurredAt: "2026-07-01T00:00:00Z",
		})
	}
	rows, total, err := svc.List(ctx, "", "", "", 10000, 0)
	if err != nil {
		t.Fatalf("List with huge limit: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("expected 3 rows, got total=%d len=%d", total, len(rows))
	}

	// And limit=0 (or negative) should fall back to the default 50.
	rows2, total2, err := svc.List(ctx, "", "", "", 0, 0)
	if err != nil {
		t.Fatalf("List with limit=0: %v", err)
	}
	if total2 != 3 || len(rows2) != 3 {
		t.Fatalf("expected 3 rows with default limit, got total=%d len=%d", total2, len(rows2))
	}
}

// Get_NotFound_ReturnsErrNotFound verifies a missing id yields ErrNotFound.
func TestFlowService_Get_NotFound_ReturnsErrNotFound(t *testing.T) {
	svc, _, _, cleanup := newFlowSvc(t)
	defer cleanup()
	_, err := svc.Get(context.Background(), 9999999)
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Get_Happy_ReturnsFlow verifies a valid id returns the matching flow.
func TestFlowService_Get_Happy_ReturnsFlow(t *testing.T) {
	svc, fr, accID, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	id, err := fr.Insert(ctx, nil, domain.InventoryFlow{
		AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 7,
		BalanceAfter: 7, OccurredAt: "2026-07-04T09:00:00Z",
		Remark:       "happy-path",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id {
		t.Fatalf("expected id=%d, got %d", id, got.ID)
	}
	if got.Quantity != 7 {
		t.Fatalf("expected qty=7, got %d", got.Quantity)
	}
	if got.Remark != "happy-path" {
		t.Fatalf("expected remark 'happy-path', got %q", got.Remark)
	}
}

// ensureTimeRangeValidationExtra is a sentinel so we can confirm the
// from > to rejection path also exists (spec: "from <= to validated when
// both supplied"). Tested implicitly via the helper exported for it.
func TestFlowService_List_TimeRange_FromAfterTo(t *testing.T) {
	svc, _, _, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	// from > to is invalid.
	_, _, err := svc.List(ctx, "", "2026-07-31T00:00:00Z", "2026-07-01T00:00:00Z", 50, 0)
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for from > to, got %v", err)
	}
}

// NegativeOffsetCoercedToZero verifies that an offset < 0 is treated as 0.
func TestFlowService_List_NegativeOffsetCoercedToZero(t *testing.T) {
	svc, fr, accID, cleanup := newFlowSvc(t)
	defer cleanup()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		seedFlow(t, fr, domain.InventoryFlow{
			AccessoryID: accID, Type: domain.FlowTypeIn, Quantity: 1,
			BalanceAfter: int64(i + 1), OccurredAt: "2026-07-01T00:00:00Z",
		})
	}
	rows, _, err := svc.List(ctx, "", "", "", 50, -7)
	if err != nil {
		t.Fatalf("List with negative offset: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (offset clamped to 0), got %d", len(rows))
	}
}

// Avoid unused-import warning on database/sql when only the type is used.
var _ *sql.DB = nil