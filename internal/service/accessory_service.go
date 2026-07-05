// Package service holds the business-logic layer for the warehouse. Services
// sit between HTTP/MCP handlers and the SQL repos, enforcing invariants that
// are too rich for the domain type alone but should not be re-implemented at
// every transport.
//
// AccessoryService is the canonical home for accessory-catalog rules:
// name uniqueness, threshold non-negative, and refuse-delete-when-flows-exist.
// It translates persistence errors into typed sentinels so transport layers
// can map them to HTTP/MCP status codes.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
)

// Sentinel errors returned by AccessoryService. Transport layers (REST/MCP)
// should map these to status codes:
//
//	ErrInvalidInput  → 400 Bad Request
//	ErrNameConflict  → 409 Conflict
//	ErrNotFound      → 404 Not Found
//	ErrHasFlow       → 409 Conflict
var (
	ErrInvalidInput = errors.New("service: invalid input")
	ErrNameConflict = errors.New("service: name already exists")
	ErrNotFound     = errors.New("service: not found")
	ErrHasFlow      = errors.New("service: accessory has inventory flows; delete refused")
)

// AccessoryService is the business-logic entry point for the accessory
// catalog. It owns validation, name uniqueness, and delete-with-flow
// protection; it never touches the database directly.
type AccessoryService struct {
	acc  *repo.AccessoryRepo
	flow *repo.FlowRepo
}

// NewAccessoryService wires the service to its repos. Both arguments are
// required; the service panics if either is nil because there is no
// sensible default for a business-logic layer that lacks persistence.
func NewAccessoryService(acc *repo.AccessoryRepo, flow *repo.FlowRepo) *AccessoryService {
	if acc == nil || flow == nil {
		panic("service.NewAccessoryService: repos must not be nil")
	}
	return &AccessoryService{acc: acc, flow: flow}
}

// Create validates the input and inserts a new accessory. A duplicate name
// surfaces as ErrNameConflict; any other persistence failure is wrapped.
func (s *AccessoryService) Create(ctx context.Context, in domain.Accessory) (domain.Accessory, error) {
	if err := in.Validate(); err != nil {
		return domain.Accessory{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	// Probe the unique index up-front so we can produce a typed conflict
	// error instead of leaking the underlying UNIQUE-constraint string.
	if _, err := s.acc.GetByName(ctx, in.Name); err == nil {
		return domain.Accessory{}, fmt.Errorf("%w: name %q already exists", ErrNameConflict, in.Name)
	} else if !errors.Is(err, repo.ErrNotFound) {
		return domain.Accessory{}, fmt.Errorf("check name: %w", err)
	}
	out, err := s.acc.Create(ctx, in)
	if err != nil {
		return domain.Accessory{}, fmt.Errorf("create accessory: %w", err)
	}
	logOp("accessory", "create", "id", out.ID, "name", out.Name, "threshold", out.LowStockThreshold)
	return out, nil
}

// Get returns the accessory by id, translating repo.ErrNotFound to
// service.ErrNotFound.
func (s *AccessoryService) Get(ctx context.Context, id int64) (domain.Accessory, error) {
	a, err := s.acc.Get(ctx, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.Accessory{}, ErrNotFound
		}
		return domain.Accessory{}, fmt.Errorf("get accessory: %w", err)
	}
	return a, nil
}

// GetByName returns the accessory by its unique name, translating
// repo.ErrNotFound to service.ErrNotFound.
func (s *AccessoryService) GetByName(ctx context.Context, name string) (domain.Accessory, error) {
	a, err := s.acc.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.Accessory{}, ErrNotFound
		}
		return domain.Accessory{}, fmt.Errorf("get accessory by name: %w", err)
	}
	return a, nil
}

// List returns accessories matching q (case-insensitive substring on name)
// with limit/offset pagination, plus the total count under the same filter.
// q may be empty for an unfiltered list.
func (s *AccessoryService) List(ctx context.Context, q string, limit, offset int) ([]domain.Accessory, int, error) {
	rows, total, err := s.acc.List(ctx, q, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list accessories: %w", err)
	}
	return rows, total, nil
}

// Update applies a partial update. Name is mutable. The threshold, when
// provided, must be non-negative.
func (s *AccessoryService) Update(ctx context.Context, id int64, u domain.AccessoryUpdate) (domain.Accessory, error) {
	if u.LowStockThreshold != nil && *u.LowStockThreshold < 0 {
		return domain.Accessory{}, fmt.Errorf("%w: low_stock_threshold must be non-negative", ErrInvalidInput)
	}
	out, err := s.acc.Update(ctx, id, u)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.Accessory{}, ErrNotFound
		}
		return domain.Accessory{}, fmt.Errorf("update accessory: %w", err)
	}
	logOp("accessory", "update", "id", out.ID, "name", out.Name)
	return out, nil
}

// Delete removes an accessory, but only when no inventory_flow rows
// reference it. When flows exist it returns ErrHasFlow. The count is
// issued via the flow repo; the schema's FK RESTRICT on
// inventory_flow.accessory_id is the atomicity guarantee — if a flow is
// inserted between the count and the delete, the FK violation surfaces
// here and we still translate it to ErrHasFlow.
func (s *AccessoryService) Delete(ctx context.Context, id int64) error {
	n, err := s.flow.CountByAccessory(ctx, id)
	if err != nil {
		return fmt.Errorf("delete accessory: count flows: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: 该配件存在 %d 条流水记录，禁止删除", ErrHasFlow, n)
	}
	if err := s.acc.Delete(ctx, id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return ErrNotFound
		}
		// Race window: a flow was inserted between CountByAccessory and
		// Delete. SQLite's FOREIGN KEY RESTRICT surfaces as
		// "FOREIGN KEY constraint failed" inside the repo error.
		if isFKViolation(err) {
			return fmt.Errorf("%w: 该配件存在流水记录，禁止删除", ErrHasFlow)
		}
		return fmt.Errorf("delete accessory: %w", err)
	}
	logOp("accessory", "delete", "id", id)
	return nil
}

// isFKViolation detects SQLite's foreign-key constraint failure message
// inside an error chain. We match on the well-known English string
// because SQLite does not export a typed error code across the driver.
func isFKViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "FOREIGN KEY constraint failed") ||
		strings.Contains(msg, "foreign key constraint failed")
}