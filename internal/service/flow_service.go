// Package service — FlowService is the read-only query layer for the
// inventory_flow ledger. It wraps FlowRepo with input validation
// (RFC3339 time parsing, type-filter whitelist, pagination clamping) so
// transport layers can hand user input directly without re-implementing
// those checks.
//
// The flow ledger is immutable: FlowService exposes only List, ListByAccessory,
// and Get. There is intentionally no Update or Delete — adding one would
// violate the "audit source of truth" guarantee stated in
// changes/mobile-accessories-management/specs/inventory-flow.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
)

// Pagination bounds for list queries. Defaults match the spec:
// limit <= 0 falls back to 50; limit is capped at 500 to protect the
// transport from accidentally pulling huge pages.
const (
	defaultFlowLimit = 50
	maxFlowLimit     = 500
)

// FlowService is the read-only entry point for inventory-flow queries.
// It validates user input and delegates to FlowRepo.
type FlowService struct {
	flow *repo.FlowRepo
}

// NewFlowService wires the service to its repo. The repo argument is
// required; nil panics because a query service has no sensible default
// without persistence.
func NewFlowService(flow *repo.FlowRepo) *FlowService {
	if flow == nil {
		panic("service.NewFlowService: flow repo must not be nil")
	}
	return &FlowService{flow: flow}
}

// List returns all flows matching the optional type / from / to filters,
// paginated. No accessory_id filter is applied. occurred_at order is
// ascending (oldest first).
func (s *FlowService) List(ctx context.Context, typ, from, to string, limit, offset int) ([]domain.InventoryFlow, int, error) {
	if err := validateType(typ); err != nil {
		return nil, 0, err
	}
	if err := validateTimeRange(from, to); err != nil {
		return nil, 0, err
	}
	limit, offset = clampPagination(limit, offset)
	rows, total, err := s.flow.List(ctx, domain.FlowFilter{
		Type: domain.FlowType(typ),
		From: from,
		To:   to,
	}, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list flows: %w", err)
	}
	return rows, total, nil
}

// ListByAccessory returns flows for one accessory, paginated. Same filter
// and validation semantics as List, plus a required accessoryID > 0.
func (s *FlowService) ListByAccessory(ctx context.Context, accessoryID int64, typ, from, to string, limit, offset int) ([]domain.InventoryFlow, int, error) {
	if accessoryID <= 0 {
		return nil, 0, fmt.Errorf("%w: accessory_id must be positive", ErrInvalidInput)
	}
	if err := validateType(typ); err != nil {
		return nil, 0, err
	}
	if err := validateTimeRange(from, to); err != nil {
		return nil, 0, err
	}
	limit, offset = clampPagination(limit, offset)
	rows, total, err := s.flow.List(ctx, domain.FlowFilter{
		AccessoryID: accessoryID,
		Type:        domain.FlowType(typ),
		From:        from,
		To:          to,
	}, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list flows by accessory: %w", err)
	}
	return rows, total, nil
}

// Get returns the flow with the given id, or ErrNotFound when no such row
// exists. The error is translated from repo.ErrNotFound so transport
// layers can rely on errors.Is(err, service.ErrNotFound).
func (s *FlowService) Get(ctx context.Context, id int64) (domain.InventoryFlow, error) {
	if id <= 0 {
		return domain.InventoryFlow{}, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	fl, err := s.flow.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.InventoryFlow{}, ErrNotFound
		}
		return domain.InventoryFlow{}, fmt.Errorf("get flow: %w", err)
	}
	return fl, nil
}

// validateType enforces the whitelist: empty (no filter), "in", or "out".
// Anything else is ErrInvalidInput.
func validateType(typ string) error {
	if typ == "" {
		return nil
	}
	t := domain.FlowType(typ)
	if !t.Valid() {
		return fmt.Errorf("%w: type must be 'in' or 'out'", ErrInvalidInput)
	}
	return nil
}

// validateTimeRange parses from / to as RFC3339 and, when both are
// supplied, enforces from <= to. Either field may be empty. A parse
// failure wraps ErrInvalidInput so transport layers can detect it with
// errors.Is.
func validateTimeRange(from, to string) error {
	var fromT, toT time.Time
	var err error
	if from != "" {
		fromT, err = time.Parse(time.RFC3339, from)
		if err != nil {
			return fmt.Errorf("%w: invalid from time (want RFC3339): %v", ErrInvalidInput, err)
		}
	}
	if to != "" {
		toT, err = time.Parse(time.RFC3339, to)
		if err != nil {
			return fmt.Errorf("%w: invalid to time (want RFC3339): %v", ErrInvalidInput, err)
		}
	}
	if from != "" && to != "" && fromT.After(toT) {
		return fmt.Errorf("%w: from must be <= to", ErrInvalidInput)
	}
	return nil
}

// clampPagination applies the documented limit / offset defaults:
//   - limit <= 0      -> 50
//   - limit > 500     -> 500
//   - offset < 0      -> 0
func clampPagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultFlowLimit
	}
	if limit > maxFlowLimit {
		limit = maxFlowLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}