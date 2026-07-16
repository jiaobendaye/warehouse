package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
)

// ReplenishmentItem is the result row for both Scan and Check. It contains
// just enough of the accessory to drive a UI / inventory decision without
// forcing the caller to fetch full domain.Accessory rows.
//
// Shortage = Threshold - CurrentStock (only meaningful when threshold > 0
// and the row was emitted in the first place).
// SuggestedQuantity is the recommended replenishment quantity. Under the
// default policy it equals Shortage; under fixed:N it equals N.
type ReplenishmentItem struct {
	AccessoryID       int64  `json:"accessory_id"`
	Name              string `json:"name"`
	Stall             string `json:"stall"`
	CurrentStock      int64  `json:"current_stock"`
	Threshold         int64  `json:"threshold"`
	Shortage          int64  `json:"shortage"`
	SuggestedQuantity int64  `json:"suggested_quantity"`
}

// BatchCheckResult bundles Check's two outputs: the items that need
// replenishment and any names the caller asked about that don't exist.
type BatchCheckResult struct {
	Items    []ReplenishmentItem `json:"items"`
	NotFound []string            `json:"not_found"`
}

// ReplenishmentService advises callers on which accessories need to be
// replenished and how much to order. It is a read-only service — it never
// mutates state — so no transaction is needed.
//
// Two entry points:
//   - Scan(ctx) returns every accessory that is below its threshold
//     (threshold > 0 AND current_stock < threshold), sorted by shortage
//     descending.
//   - Check(ctx, names, policy) returns the subset of named accessories
//     that need replenishment, plus a NotFound list of any names that
//     don't exist.
//
// Scaling note: Scan fetches the entire catalog via List(...) and filters
// in-memory. For v1 with small catalogs (hundreds of items) this is fine.
// Once the catalog grows into the tens of thousands, replace the filter
// pass with a SQL query that emits shortage directly:
//     SELECT id, name, current_stock, low_stock_threshold,
//            (low_stock_threshold - current_stock) AS shortage
//     FROM accessories
//     WHERE low_stock_threshold > 0 AND current_stock < low_stock_threshold
//     ORDER BY shortage DESC;
type ReplenishmentService struct {
	acc *repo.AccessoryRepo
}

// NewReplenishmentService wires the service to its accessory repo. Nil panics
// because there is no sensible default for a read-only advisor without
// persistence.
func NewReplenishmentService(acc *repo.AccessoryRepo) *ReplenishmentService {
	if acc == nil {
		panic("service.NewReplenishmentService: accessory repo must not be nil")
	}
	return &ReplenishmentService{acc: acc}
}

// Scan returns every accessory whose threshold is positive and whose
// current_stock is strictly less than the threshold, sorted by stall
// ascending then shortage descending. Threshold-zero accessories are
// never emitted regardless of stock level — that contract is locked in
// by TestReplenishmentService_Scan_ExcludesThresholdZero.
func (s *ReplenishmentService) Scan(ctx context.Context) ([]ReplenishmentItem, error) {
	// Pick a limit comfortably above any expected catalog size for v1.
	// See the type doc for the scaling note.
	const unlimitedPage = 1_000_000
	all, _, err := s.acc.List(ctx, "", "", unlimitedPage, 0)
	if err != nil {
		return nil, fmt.Errorf("scan: list accessories: %w", err)
	}

	out := make([]ReplenishmentItem, 0)
	for _, a := range all {
		if !isShortage(a) {
			continue
		}
		out = append(out, buildItem(a, a.LowStockThreshold-a.CurrentStock))
	}

	sortReplenishment(out)

	return out, nil
}

// Check inspects the listed names and returns those that need replenishment,
// plus any names the catalog doesn't know about. policy may be:
//   - "" or "default": suggested = shortage.
//   - "fixed:<N>":     suggested = N (N must be a positive int64).
//   - anything else:   ErrInvalidInput.
//
// Threshold-zero accessories are filtered out for the same reason as Scan:
// they're not considered to need replenishment at all.
func (s *ReplenishmentService) Check(ctx context.Context, names []string, policy string) (BatchCheckResult, error) {
	suggested, err := parsePolicy(policy)
	if err != nil {
		return BatchCheckResult{}, err
	}

	res := BatchCheckResult{
		Items:    make([]ReplenishmentItem, 0),
		NotFound: make([]string, 0),
	}
	for _, name := range names {
		// Skip blank name entries silently — they're a no-op and don't
		// deserve a NotFound flag (the caller may have generated them).
		if strings.TrimSpace(name) == "" {
			continue
		}
		a, err := s.acc.GetByName(ctx, name)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				res.NotFound = append(res.NotFound, name)
				continue
			}
			return BatchCheckResult{}, fmt.Errorf("check name %q: %w", name, err)
		}
		if !isShortage(a) {
			continue
		}
		shortage := a.LowStockThreshold - a.CurrentStock
		res.Items = append(res.Items, buildItem(a, suggested(shortage)))
	}

	// Sort for stable, predictable output (matches Scan ordering).
	sortReplenishment(res.Items)

	return res, nil
}

// sortReplenishment sorts items by stall ascending then shortage
// descending, so the export and UI group accessories by stall with the
// most urgent items first within each stall group.
func sortReplenishment(items []ReplenishmentItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Stall != items[j].Stall {
			return items[i].Stall < items[j].Stall
		}
		return items[i].Shortage > items[j].Shortage
	})
}

// isShortage encodes the threshold-zero rule: only accessories with a
// positive threshold can be considered short, regardless of current stock.
func isShortage(a domain.Accessory) bool {
	return a.LowStockThreshold > 0 && a.CurrentStock < a.LowStockThreshold
}

// buildItem assembles the public ReplenishmentItem from the loaded row.
func buildItem(a domain.Accessory, suggested int64) ReplenishmentItem {
	return ReplenishmentItem{
		AccessoryID:       a.ID,
		Name:              a.Name,
		Stall:             a.Stall,
		CurrentStock:      a.CurrentStock,
		Threshold:         a.LowStockThreshold,
		Shortage:          a.LowStockThreshold - a.CurrentStock,
		SuggestedQuantity: suggested,
	}
}

// parsePolicy converts the policy string into a function that computes the
// suggested quantity from the shortage. An invalid policy yields ErrInvalidInput.
func parsePolicy(policy string) (func(shortage int64) int64, error) {
	switch strings.TrimSpace(policy) {
	case "", "default":
		return func(s int64) int64 { return s }, nil
	}
	const prefix = "fixed:"
	if !strings.HasPrefix(policy, prefix) {
		return nil, fmt.Errorf("%w: unknown policy %q", ErrInvalidInput, policy)
	}
	body := strings.TrimPrefix(policy, prefix)
	// Reject obviously malformed inputs: a trailing colon with no number
	// and any value that contains additional colons.
	if body == "" || strings.Contains(body, ":") {
		return nil, fmt.Errorf("%w: malformed fixed policy %q", ErrInvalidInput, policy)
	}
	n, err := strconv.ParseInt(body, 10, 64)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("%w: fixed policy %q must be a positive integer", ErrInvalidInput, policy)
	}
	return func(int64) int64 { return n }, nil
}