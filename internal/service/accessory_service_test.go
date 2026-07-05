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

func strPtr(s string) *string { return &s }
func i64Ptr(n int64) *int64   { return &n }

// --- Create --------------------------------------------------------------

func TestAccessoryService_Create_Success(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()

	got, err := svc.Create(context.Background(), domain.Accessory{
		Name:              "保护壳",
		LowStockThreshold: 3,
		Notes:             "iPhone 15",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if got.Name != "保护壳" {
		t.Fatalf("expected name 保护壳, got %q", got.Name)
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
		Name: "n", LowStockThreshold: 0,
	})
	if err != nil {
		t.Fatalf("Create with threshold=0 should succeed, got %v", err)
	}
	if got.LowStockThreshold != 0 {
		t.Fatalf("expected threshold=0, got %d", got.LowStockThreshold)
	}
}

func TestAccessoryService_Create_DuplicateName(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := svc.Create(ctx, domain.Accessory{Name: "DUP"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := svc.Create(ctx, domain.Accessory{Name: "DUP"})
	if !errors.Is(err, service.ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict, got %v", err)
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
		{"missing name", domain.Accessory{Name: ""}},
		{"blank name", domain.Accessory{Name: "   "}},
		{"negative threshold", domain.Accessory{Name: "n", LowStockThreshold: -1}},
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

// --- Get / GetByName ------------------------------------------------------

func TestAccessoryService_Get_NotFound(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	_, err := svc.Get(context.Background(), 99999)
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAccessoryService_GetByName_NotFound(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	_, err := svc.GetByName(context.Background(), "NOPE")
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAccessoryService_GetByName_Found(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := svc.Create(ctx, domain.Accessory{Name: "K1"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := svc.GetByName(ctx, "K1")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Name != "K1" {
		t.Fatalf("expected name K1, got %q", got.Name)
	}
}

// --- List ----------------------------------------------------------------

func TestAccessoryService_List_KeywordAndPagination(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()

	items := []domain.Accessory{
		{Name: "透明保护壳 iPhone"},
		{Name: "钢化膜 iPhone"},
		{Name: "数据线 typeC"},
		{Name: "硅胶壳 iPhone"},
	}
	for _, a := range items {
		if _, err := svc.Create(ctx, a); err != nil {
			t.Fatalf("seed %v: %v", a.Name, err)
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

	// Case-insensitive name match
	byName, total, err := svc.List(ctx, "数据线", 100, 0)
	if err != nil {
		t.Fatalf("List by name: %v", err)
	}
	if total != 1 || len(byName) != 1 {
		t.Fatalf("expected 1 match for '数据线', got total=%d len=%d", total, len(byName))
	}
	if byName[0].Name != "数据线 typeC" {
		t.Fatalf("expected name 数据线 typeC, got %q", byName[0].Name)
	}
}

// --- Update --------------------------------------------------------------

func TestAccessoryService_Update_Success(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{
		Name: "old", LowStockThreshold: 3,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, domain.AccessoryUpdate{
		Name:              strPtr("new"),
		LowStockThreshold: i64Ptr(10),
		Notes:             strPtr("note"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "new" {
		t.Fatalf("name: %q", updated.Name)
	}
	if updated.LowStockThreshold != 10 {
		t.Fatalf("threshold: %d", updated.LowStockThreshold)
	}
	if updated.Notes != "note" {
		t.Fatalf("notes: %q", updated.Notes)
	}
}

func TestAccessoryService_Update_RenamesName(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{Name: "renameme"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err = svc.Update(ctx, created.ID, domain.AccessoryUpdate{Name: strPtr("renamed")})
	if err != nil {
		t.Fatalf("Update should succeed, got %v", err)
	}
}

func TestAccessoryService_Update_NegativeThresholdRejected(t *testing.T) {
	svc, _, _, cleanup := newSvc(t)
	defer cleanup()
	ctx := context.Background()
	created, err := svc.Create(ctx, domain.Accessory{Name: "n", LowStockThreshold: 0})
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
	created, err := svc.Create(ctx, domain.Accessory{Name: "n", LowStockThreshold: 5})
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
	created, err := svc.Create(ctx, domain.Accessory{Name: "n"})
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
	created, err := svc.Create(ctx, domain.Accessory{Name: "n"})
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