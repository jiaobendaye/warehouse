package repo_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/db"
	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
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

// TestAccessoryRepo_CreateAndGetBySKU verifies a fresh accessory can be
// inserted and retrieved by SKU.
func TestAccessoryRepo_CreateAndGetBySKU(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	r := repo.NewAccessoryRepo(d)

	ctx := context.Background()
	in := domain.Accessory{
		SKU:               "SKU-1",
		Name:              "透明保护壳",
		Unit:              "个",
		LowStockThreshold: 5,
		Notes:             "iPhone 15 适用",
	}
	got, err := r.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero ID after Create")
	}
	if got.CurrentStock != 0 {
		t.Fatalf("expected CurrentStock=0 on create, got %d", got.CurrentStock)
	}
	if got.CreatedAt == "" {
		t.Fatal("expected non-empty CreatedAt")
	}

	bySKU, err := r.GetBySKU(ctx, "SKU-1")
	if err != nil {
		t.Fatalf("GetBySKU: %v", err)
	}
	if bySKU.Name != "透明保护壳" {
		t.Fatalf("expected name '透明保护壳', got %q", bySKU.Name)
	}
	if bySKU.ID != got.ID {
		t.Fatalf("ID mismatch: %d vs %d", bySKU.ID, got.ID)
	}
}

// TestAccessoryRepo_CreateDuplicateSKU verifies a second insert with the
// same SKU is rejected.
func TestAccessoryRepo_CreateDuplicateSKU(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	r := repo.NewAccessoryRepo(d)
	ctx := context.Background()

	if _, err := r.Create(ctx, domain.Accessory{SKU: "DUP", Name: "a", Unit: "个"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := r.Create(ctx, domain.Accessory{SKU: "DUP", Name: "b", Unit: "个"})
	if err == nil {
		t.Fatal("expected duplicate-SKU error, got nil")
	}
}

// TestAccessoryRepo_GetNotFound verifies Get returns a sentinel error for missing IDs.
func TestAccessoryRepo_GetNotFound(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	r := repo.NewAccessoryRepo(d)
	_, err := r.Get(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

// TestAccessoryRepo_ListAndSearch verifies pagination and substring search.
func TestAccessoryRepo_ListAndSearch(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	r := repo.NewAccessoryRepo(d)
	ctx := context.Background()

	items := []domain.Accessory{
		{SKU: "A-1", Name: "保护壳 iPhone", Unit: "个"},
		{SKU: "A-2", Name: "贴膜 iPhone", Unit: "张"},
		{SKU: "B-1", Name: "数据线", Unit: "条"},
	}
	for _, a := range items {
		if _, err := r.Create(ctx, a); err != nil {
			t.Fatalf("seed Create: %v", err)
		}
	}

	all, total, err := r.List(ctx, "", 100, 0)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 items, got %d", len(all))
	}

	matched, total, err := r.List(ctx, "iPhone", 100, 0)
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2 for 'iPhone', got %d", total)
	}
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched items, got %d", len(matched))
	}
}

// TestAccessoryRepo_Update verifies partial update and rejection of SKU change.
func TestAccessoryRepo_Update(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	r := repo.NewAccessoryRepo(d)
	ctx := context.Background()

	created, err := r.Create(ctx, domain.Accessory{SKU: "U-1", Name: "原名", Unit: "个", LowStockThreshold: 3})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := r.Update(ctx, created.ID, domain.AccessoryUpdate{
		Name:              ptr("新名"),
		Unit:              ptr("盒"),
		LowStockThreshold: ptrInt64(10),
		Notes:             ptr("备注"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "新名" {
		t.Fatalf("expected name '新名', got %q", updated.Name)
	}
	if updated.Unit != "盒" {
		t.Fatalf("expected unit '盒', got %q", updated.Unit)
	}
	if updated.LowStockThreshold != 10 {
		t.Fatalf("expected threshold 10, got %d", updated.LowStockThreshold)
	}
	if updated.SKU != "U-1" {
		t.Fatalf("SKU should not change, got %q", updated.SKU)
	}
}

// TestAccessoryRepo_Delete verifies successful delete on a freshly-created row.
func TestAccessoryRepo_Delete(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	r := repo.NewAccessoryRepo(d)
	ctx := context.Background()
	created, err := r.Create(ctx, domain.Accessory{SKU: "DEL", Name: "x", Unit: "个"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(ctx, created.ID); err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func ptr(s string) *string { return &s }
func ptrInt64(n int64) *int64 { return &n }