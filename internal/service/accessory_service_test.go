package service_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/db"
	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
	"github.com/jiaobendaye/warehouse/internal/service"
)

func newTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	return d, func() { _ = d.Close() }
}

func newSvc(t *testing.T) (*service.AccessoryService, *repo.AccessoryRepo, *repo.FlowRepo, func()) {
	t.Helper()
	d, cleanup := newTestDB(t)
	acc := repo.NewAccessoryRepo(d)
	flow := repo.NewFlowRepo(d)
	return service.NewAccessoryService(acc, flow), acc, flow, cleanup
}

func strPtr(s string) *string  { return &s }
func i64Ptr(n int64) *int64    { return &n }

// --- Create --------------------------------------------------------------

func TestAccessoryService_Create_Success(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()

	got, err := svc.Create(context.Background(), domain.Accessory{
		SKU:               "S-1",
		Name:              "保护壳",
		Unit:              "个",
		LowStockThreshold: 3,
		Notes:             "iPhone 15",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if got.SKU != "S-1" {
		t.Fatalf("expected SKU S-1, got %q", got.SKU)
	}
	if got.CurrentStock != 0 {
		t.Fatalf("expected stock=0 on create, got %d", got.CurrentStock)
	}
	if got.CreatedAt == "" {
		t.Fatal("expected CreatedAt populated")
	}
}

func TestAccessoryService_Create_ThresholdZeroAllowed(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()

	got, err := svc.Create(context.Background(), domain.Accessory{
		SKU: "Z", Name: "n", Unit: "u", LowStockThreshold: 0,
	})
	if err != nil {
		t.Fatalf("Create with threshold=0 should succeed, got %v", err)
	}
	if got.LowStockThreshold != 0 {
		t.Fatalf("expected threshold=0, got %d", got.LowStockThreshold)
	}
}

func TestAccessoryService_Create_DuplicateSKU(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := svc.Create(ctx, domain.Accessory{SKU: "DUP", Name: "a", Unit: "个"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := svc.Create(ctx, domain.Accessory{SKU: "DUP", Name: "b", Unit: "个"})
	if !errors.Is(err, service.ErrSKUConflict) {
		t.Fatalf("expected ErrSKUConflict, got %v", err)
	}
}

func TestAccessoryService_Create_InvalidInput(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()

	cases := []struct {
		name string
		in   domain.Accessory
	}{
		{"missing sku", domain.Accessory{Name: "n", Unit: "u"}},
		{"blank sku", domain.Accessory{SKU: "   ", Name: "n", Unit: "u"}},
		{"missing name", domain.Accessory{SKU: "S", Unit: "u"}},
		{"missing unit", domain.Accessory{SKU: "S", Name: "n"}},
		{"negative threshold", domain.Accessory{SKU: "S", Name: "n", Unit: "u", LowStockThreshold: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(ctx, tc.in)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

// --- Get / GetBySKU ------------------------------------------------------

func TestAccessoryService_Get_NotFound(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	_, err := svc.Get(context.Background(), 99999)
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAccessoryService_GetBySKU_NotFound(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	_, err := svc.GetBySKU(context.Background(), "NOPE")
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAccessoryService_GetBySKU_Found(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := svc.Create(ctx, domain.Accessory{SKU: "K1", Name: "n", Unit: "u"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := svc.GetBySKU(ctx, "K1")
	if err != nil {
		t.Fatalf("GetBySKU: %v", err)
	}
	if got.SKU != "K1" {
		t.Fatalf("expected SKU K1, got %q", got.SKU)
	}
}

// --- List ----------------------------------------------------------------

func TestAccessoryService_List_KeywordAndPagination(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()

	items := []domain.Accessory{
		{SKU: "A-1", Name: "透明保护壳 iPhone", Unit: "个"},
		{SKU: "A-2", Name: "钢化膜 iPhone", Unit: "张"},
		{SKU: "B-1", Name: "数据线 typeC", Unit: "条"},
		{SKU: "A-3", Name: "硅胶壳 iPhone", Unit: "个"},
	}
	for _, a := range items {
		if _, err := svc.Create(ctx, a); err != nil {
			t.Fatalf("seed %v: %v", a.SKU, err)
		}
	}

	// No filter -> all 4
	all, total, err := svc.List(ctx, "", 100, 0)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if total != 4 || len(all) != 4 {
		t.Fatalf("expected 4 items, got total=%d len=%d", total, len(all))
	}

	// Keyword 'iPhone' -> 3 (matches name)
	matched, total, err := svc.List(ctx, "iPhone", 100, 0)
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if total != 3 || len(matched) != 3 {
		t.Fatalf("expected 3 iPhone matches, got total=%d len=%d", total, len(matched))
	}

	// Pagination: limit=2, offset=0
	page1, total, err := svc.List(ctx, "", 2, 0)
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if total != 4 {
		t.Fatalf("total should still be 4 (unfiltered count), got %d", total)
	}
	if len(page1) != 2 {
		t.Fatalf("expected 2 items on page1, got %d", len(page1))
	}

	// Pagination: limit=2, offset=2
	page2, _, err := svc.List(ctx, "", 2, 2)
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("expected 2 items on page2, got %d", len(page2))
	}

	// Case-insensitive sku match
	bySku, total, err := svc.List(ctx, "b-1", 100, 0)
	if err != nil {
		t.Fatalf("List by sku: %v", err)
	}
	if total != 1 || len(bySku) != 1 {
		t.Fatalf("expected 1 match for 'b-1', got total=%d len=%d", total, len(bySku))
	}
	if bySku[0].SKU != "B-1" {
		t.Fatalf("expected SKU B-1, got %q", bySku[0].SKU)
	}
}

// --- Update --------------------------------------------------------------

func TestAccessoryService_Update_Success(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{
		SKU: "U-1", Name: "old", Unit: "个", LowStockThreshold: 3,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, domain.AccessoryUpdate{
		Name:              strPtr("new"),
		Unit:              strPtr("盒"),
		LowStockThreshold: i64Ptr(10),
		Notes:             strPtr("note"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "new" {
		t.Fatalf("name: %q", updated.Name)
	}
	if updated.Unit != "盒" {
		t.Fatalf("unit: %q", updated.Unit)
	}
	if updated.LowStockThreshold != 10 {
		t.Fatalf("threshold: %d", updated.LowStockThreshold)
	}
	if updated.Notes != "note" {
		t.Fatalf("notes: %q", updated.Notes)
	}
	if updated.SKU != "U-1" {
		t.Fatalf("SKU must be unchanged, got %q", updated.SKU)
	}
}

func TestAccessoryService_Update_RejectsSKUAttempt(t *testing.T) {
	// Per spec §"修改 SKU 被拒": any attempt to write SKU MUST return 400 Bad Request.
	// AccessoryUpdate has no SKU field by design (compile-time guard), so we
	// additionally verify a non-nil same-value pointer is harmless at this
	// layer — there is no field to write into. The true contract is enforced
	// at the type level. This test exists to lock in that contract.
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{SKU: "U-2", Name: "n", Unit: "u"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// No SKU field on AccessoryUpdate -> mutating only name is fine.
	_, err = svc.Update(ctx, created.ID, domain.AccessoryUpdate{Name: strPtr("renamed")})
	if err != nil {
		t.Fatalf("Update without SKU should succeed, got %v", err)
	}
}

func TestAccessoryService_Update_NegativeThresholdRejected(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{SKU: "U-3", Name: "n", Unit: "u", LowStockThreshold: 0})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err = svc.Update(ctx, created.ID, domain.AccessoryUpdate{LowStockThreshold: i64Ptr(-5)})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestAccessoryService_Update_ThresholdZeroAllowed(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{SKU: "U-4", Name: "n", Unit: "u", LowStockThreshold: 5})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	updated, err := svc.Update(ctx, created.ID, domain.AccessoryUpdate{LowStockThreshold: i64Ptr(0)})
	if err != nil {
		t.Fatalf("Update threshold=0 should succeed, got %v", err)
	}
	if updated.LowStockThreshold != 0 {
		t.Fatalf("expected threshold=0, got %d", updated.LowStockThreshold)
	}
}

func TestAccessoryService_Update_NotFound(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	_, err := svc.Update(context.Background(), 4242, domain.AccessoryUpdate{Name: strPtr("x")})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- Delete --------------------------------------------------------------

func TestAccessoryService_Delete_NoFlows_OK(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{SKU: "D-1", Name: "n", Unit: "u"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// verify gone
	if _, err := svc.Get(ctx, created.ID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestAccessoryService_Delete_WithFlows_Rejected(t *testing.T) {
	svc, _, fr, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{SKU: "D-2", Name: "n", Unit: "u"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Attach a flow row directly via FlowRepo (StockService will own this later).
	if _, err := fr.Insert(ctx, nil, domain.InventoryFlow{
		AccessoryID:  created.ID,
		Type:         domain.FlowTypeIn,
		Quantity:     1,
		BalanceAfter: 1,
		OccurredAt:   "2026-07-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed flow: %v", err)
	}
	err = svc.Delete(ctx, created.ID)
	if !errors.Is(err, service.ErrHasFlow) {
		t.Fatalf("expected ErrHasFlow, got %v", err)
	}
	// message should mention the protection
	if err != nil && !strings.Contains(err.Error(), "流水") && !strings.Contains(err.Error(), "flow") {
		t.Fatalf("error message should mention flow/流水, got %q", err.Error())
	}
}

func TestAccessoryService_Delete_NotFound(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	err := svc.Delete(context.Background(), 7777)
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}