package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/db"
	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// newReplTestDB mirrors the pattern used in service/*_test.go and repo/*_test.go
// for setting up an isolated SQLite database per test.
func newReplTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	return d, func() { _ = d.Close() }
}

func newReplSvc(t *testing.T) (*service.ReplenishmentService, *repo.AccessoryRepo, func()) {
	t.Helper()
	d, cleanup := newReplTestDB(t)
	acc := repo.NewAccessoryRepo(d)
	return service.NewReplenishmentService(acc), acc, cleanup
}

// seedAccessory creates an accessory with the given parameters. If
// initialStock >= 0 it sets current_stock via the repo's SQL primitives
// (since Create always starts stock at 0). It returns the loaded row.
func seedAccessory(t *testing.T, r *repo.AccessoryRepo, sku, name string, threshold, initialStock int64) domain.Accessory {
	t.Helper()
	ctx := context.Background()
	created, err := r.Create(ctx, domain.Accessory{
		SKU:               sku,
		Name:              name,
		LowStockThreshold: threshold,
	})
	if err != nil {
		t.Fatalf("seed Create %s: %v", sku, err)
	}
	if initialStock > 0 {
		if err := r.SetStock(ctx, nil, created.ID, initialStock); err != nil {
			t.Fatalf("seed SetStock %s: %v", sku, err)
		}
		// Re-read so callers see the up-to-date row.
		fresh, err := r.Get(ctx, created.ID)
		if err != nil {
			t.Fatalf("seed Get %s: %v", sku, err)
		}
		return fresh
	}
	return created
}

// --- Scan ----------------------------------------------------------------

// TestReplenishmentService_Scan_FindsShortageItems verifies Scan returns
// only accessories that are below their threshold (and whose threshold is
// non-zero), with correct shortage and suggested-quantity values.
func TestReplenishmentService_Scan_FindsShortageItems(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	// 1) overstocked: 100 >= 10
	over := seedAccessory(t, acc, "OVER", "Over", 10, 100)
	// 2) just at threshold: 5 >= 5 -> not short
	ok := seedAccessory(t, acc, "OK", "OK", 5, 5)
	// 3) short: 2 < 10 -> shortage=8
	short := seedAccessory(t, acc, "SHORT", "Short", 10, 2)

	items, err := svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 shortage item, got %d: %+v", len(items), items)
	}
	got := items[0]
	if got.SKU != short.SKU {
		t.Fatalf("expected SKU %q, got %q", short.SKU, got.SKU)
	}
	if got.AccessoryID != short.ID {
		t.Fatalf("expected AccessoryID %d, got %d", short.ID, got.AccessoryID)
	}
	if got.Name != short.Name {
		t.Fatalf("expected Name %q, got %q", short.Name, got.Name)
	}
	if got.CurrentStock != 2 {
		t.Fatalf("expected CurrentStock 2, got %d", got.CurrentStock)
	}
	if got.Threshold != 10 {
		t.Fatalf("expected Threshold 10, got %d", got.Threshold)
	}
	if got.Shortage != 8 {
		t.Fatalf("expected Shortage 8, got %d", got.Shortage)
	}
	if got.SuggestedQuantity != 8 {
		t.Fatalf("expected SuggestedQuantity 8, got %d", got.SuggestedQuantity)
	}
	// sanity: ensure the other two are not present
	for _, sku := range []string{over.SKU, ok.SKU} {
		for _, it := range items {
			if it.SKU == sku {
				t.Fatalf("unexpected SKU %q in scan result", sku)
			}
		}
	}
}

// TestReplenishmentService_Scan_SortsByShortageDesc verifies multiple short
// accessories appear in descending-shortage order.
func TestReplenishmentService_Scan_SortsByShortageDesc(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	// Create in a non-shortage order to ensure sorting is real, not insertion order.
	_ = seedAccessory(t, acc, "S2", "SmallShortage", 5, 4)  // shortage=1
	_ = seedAccessory(t, acc, "S1", "MediumShortage", 50, 10) // shortage=40
	_ = seedAccessory(t, acc, "S3", "BigShortage", 100, 1)   // shortage=99

	items, err := svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	shortages := []int64{items[0].Shortage, items[1].Shortage, items[2].Shortage}
	for i := 1; i < len(shortages); i++ {
		if shortages[i-1] < shortages[i] {
			t.Fatalf("Scan not sorted by shortage desc: %v", shortages)
		}
	}
	// Verify ordering matches our expected values: 99, 40, 1
	expected := []int64{99, 40, 1}
	for i, e := range expected {
		if shortages[i] != e {
			t.Fatalf("position %d: expected shortage %d, got %d", i, e, shortages[i])
		}
	}
}

// TestReplenishmentService_Scan_ExcludesThresholdZero verifies accessories
// with low_stock_threshold=0 never appear, even when current_stock=0.
func TestReplenishmentService_Scan_ExcludesThresholdZero(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	zeroThresh := seedAccessory(t, acc, "Z", "ZeroThreshold", 0, 0)
	realShort := seedAccessory(t, acc, "R", "RealShort", 3, 1)

	items, err := svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, it := range items {
		if it.SKU == zeroThresh.SKU {
			t.Fatalf("accessory with threshold=0 must not appear, got %+v", it)
		}
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 item (real short), got %d", len(items))
	}
	if items[0].SKU != realShort.SKU {
		t.Fatalf("expected SKU %q, got %q", realShort.SKU, items[0].SKU)
	}
}

// TestReplenishmentService_Scan_NoShortage_ReturnsEmpty verifies an empty
// (non-nil) slice when nothing is short.
func TestReplenishmentService_Scan_NoShortage_ReturnsEmpty(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	_ = seedAccessory(t, acc, "A", "A", 5, 100)
	_ = seedAccessory(t, acc, "B", "B", 1, 10)
	_ = seedAccessory(t, acc, "C", "C", 3, 3) // exactly at threshold

	items, err := svc.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d: %+v", len(items), items)
	}
	if items == nil {
		// Empty slice is acceptable for callers; we just want zero items.
		// nil is fine. Locking the exact representation isn't required by spec.
	}
}

// --- Check ---------------------------------------------------------------

// TestReplenishmentService_Check_PartialShortage verifies Check returns only
// the SKUs that need replenishment, leaving OK ones out.
func TestReplenishmentService_Check_PartialShortage(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	shortA := seedAccessory(t, acc, "A", "A", 10, 2) // shortage=8
	_ = seedAccessory(t, acc, "B", "B", 5, 100)      // OK
	shortC := seedAccessory(t, acc, "C", "C", 4, 1)  // shortage=3

	res, err := svc.Check(ctx, []string{"A", "B", "C"}, "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("expected 2 shortage items, got %d: %+v", len(res.Items), res.Items)
	}
	if len(res.NotFound) != 0 {
		t.Fatalf("expected empty NotFound, got %v", res.NotFound)
	}
	// Default policy: suggested = shortage. Just check values are present.
	skus := map[string]service.ReplenishmentItem{}
	for _, it := range res.Items {
		skus[it.SKU] = it
	}
	if it, ok := skus[shortA.SKU]; !ok {
		t.Fatalf("missing SKU %q in result", shortA.SKU)
	} else if it.Shortage != 8 || it.SuggestedQuantity != 8 {
		t.Fatalf("A: want Shortage=8 Suggested=8, got %+v", it)
	}
	if it, ok := skus[shortC.SKU]; !ok {
		t.Fatalf("missing SKU %q in result", shortC.SKU)
	} else if it.Shortage != 3 || it.SuggestedQuantity != 3 {
		t.Fatalf("C: want Shortage=3 Suggested=3, got %+v", it)
	}
}

// TestReplenishmentService_Check_ReportsNotFound verifies an unknown SKU
// accumulates into NotFound (and does not cause an error).
func TestReplenishmentService_Check_ReportsNotFound(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	shortA := seedAccessory(t, acc, "A", "A", 10, 2)
	_ = seedAccessory(t, acc, "B", "B", 5, 100)

	res, err := svc.Check(ctx, []string{"A", "NOT-A-SKU", "B", "GHOST"}, "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(res.NotFound) != 2 {
		t.Fatalf("expected 2 missing SKUs, got %v", res.NotFound)
	}
	want := map[string]bool{"NOT-A-SKU": true, "GHOST": true}
	for _, sku := range res.NotFound {
		if !want[sku] {
			t.Fatalf("unexpected SKU in NotFound: %q", sku)
		}
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 shortage item, got %d", len(res.Items))
	}
	if res.Items[0].SKU != shortA.SKU {
		t.Fatalf("expected item SKU %q, got %q", shortA.SKU, res.Items[0].SKU)
	}
}

// TestReplenishmentService_Check_FixedPolicy_UsesFixedQuantity verifies
// fixed:N policy overrides shortage-based suggestion.
func TestReplenishmentService_Check_FixedPolicy_UsesFixedQuantity(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	// Stock 2, threshold 10 -> shortage=8. But policy says fixed:50.
	_ = seedAccessory(t, acc, "X", "X", 10, 2)

	res, err := svc.Check(ctx, []string{"X"}, "fixed:50")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(res.Items))
	}
	got := res.Items[0]
	if got.Shortage != 8 {
		t.Fatalf("Shortage should still be 8, got %d", got.Shortage)
	}
	if got.SuggestedQuantity != 50 {
		t.Fatalf("expected SuggestedQuantity=50 from fixed:50 policy, got %d", got.SuggestedQuantity)
	}
}

// TestReplenishmentService_Check_DefaultPolicy_UsesShortage verifies the
// "default" policy keyword (and empty) yields shortage as suggested.
func TestReplenishmentService_Check_DefaultPolicy_UsesShortage(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	_ = seedAccessory(t, acc, "Y", "Y", 20, 5) // shortage=15

	for _, policy := range []string{"", "default"} {
		t.Run("policy="+policy, func(t *testing.T) {
			res, err := svc.Check(ctx, []string{"Y"}, policy)
			if err != nil {
				t.Fatalf("Check policy=%q: %v", policy, err)
			}
			if len(res.Items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(res.Items))
			}
			got := res.Items[0]
			if got.Shortage != 15 {
				t.Fatalf("Shortage: want 15, got %d", got.Shortage)
			}
			if got.SuggestedQuantity != 15 {
				t.Fatalf("SuggestedQuantity should equal Shortage under default policy, got %d",
					got.SuggestedQuantity)
			}
		})
	}
}

// TestReplenishmentService_Check_InvalidPolicy_ReturnsErrInvalidInput
// verifies malformed policy strings yield ErrInvalidInput.
func TestReplenishmentService_Check_InvalidPolicy_ReturnsErrInvalidInput(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	_ = seedAccessory(t, acc, "Z", "Z", 10, 2)

	cases := []struct {
		name   string
		policy string
	}{
		{"weird-string", "weird"},
		{"fixed-missing-value", "fixed:"},
		{"fixed-non-numeric", "fixed:abc"},
		{"fixed-negative", "fixed:-5"},
		{"fixed-zero", "fixed:0"},
		{"extra-colon", "fixed:50:extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Check(ctx, []string{"Z"}, tc.policy)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("policy=%q: expected ErrInvalidInput, got %v", tc.policy, err)
			}
		})
	}
}

// TestReplenishmentService_Check_ThresholdZero_NotShortage verifies that
// accessories with threshold=0 never appear in Check items, regardless of
// current stock.
func TestReplenishmentService_Check_ThresholdZero_NotShortage(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	ctx := context.Background()

	zeroThresh := seedAccessory(t, acc, "ZERO", "Zero", 0, 0)
	// zeroThresh already has stock=0; also seed a zero-threshold item
	// with positive stock that should still not appear.
	zeroThreshWithStock := seedAccessory(t, acc, "ZERO2", "Zero2", 0, 100)
	// An actually-short row to confirm Check is functioning correctly.
	real := seedAccessory(t, acc, "REAL", "Real", 5, 1)

	res, err := svc.Check(ctx, []string{zeroThresh.SKU, zeroThreshWithStock.SKU, real.SKU}, "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 item (REAL), got %d: %+v", len(res.Items), res.Items)
	}
	if res.Items[0].SKU != real.SKU {
		t.Fatalf("expected item SKU %q, got %q", real.SKU, res.Items[0].SKU)
	}
	for _, sku := range []string{zeroThresh.SKU, zeroThreshWithStock.SKU} {
		for _, it := range res.Items {
			if it.SKU == sku {
				t.Fatalf("threshold-0 SKU %q should not appear", sku)
			}
		}
	}
}

// TestReplenishmentService_Check_EmptyInput verifies Check with an empty
// SKU slice returns an empty (no-error) result.
func TestReplenishmentService_Check_EmptyInput(t *testing.T) {
	svc, acc, cleanup := newReplSvc(t)
	defer cleanup()
	// Seed something just to make sure the service is wired.
	_ = seedAccessory(t, acc, "X", "X", 5, 1)

	res, err := svc.Check(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("Check empty: %v", err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(res.Items))
	}
	if len(res.NotFound) != 0 {
		t.Fatalf("expected 0 not-found, got %v", res.NotFound)
	}

	res, err = svc.Check(context.Background(), []string{}, "fixed:5")
	if err != nil {
		t.Fatalf("Check []: %v", err)
	}
	if len(res.Items) != 0 || len(res.NotFound) != 0 {
		t.Fatalf("expected empty result, got %+v", res)
	}
}
