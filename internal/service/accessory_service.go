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
	"database/sql"
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
var (
	ErrInvalidInput = errors.New("service: invalid input")
	ErrNameConflict = errors.New("service: name already exists")
	ErrNotFound     = errors.New("service: not found")
)

// AccessoryService is the business-logic entry point for the accessory
// catalog. It owns validation, name uniqueness, and the transactional
// cascade-delete (accessory + its flows); it never touches the database
// directly except for opening the delete transaction.
type AccessoryService struct {
	db   *sql.DB
	acc  *repo.AccessoryRepo
	flow *repo.FlowRepo
}

// NewAccessoryService wires the service to its repos and the underlying DB
// (needed for transactional cascade-delete). All arguments are required;
// the service panics if any is nil because there is no sensible default
// for a business-logic layer that lacks persistence.
func NewAccessoryService(db *sql.DB, acc *repo.AccessoryRepo, flow *repo.FlowRepo) *AccessoryService {
	if db == nil || acc == nil || flow == nil {
		panic("service.NewAccessoryService: db and repos must not be nil")
	}
	return &AccessoryService{db: db, acc: acc, flow: flow}
}

// Create validates the input and inserts a new accessory. A duplicate name
// surfaces as ErrNameConflict; any other persistence failure is wrapped.
func (s *AccessoryService) Create(ctx context.Context, in domain.Accessory) (domain.Accessory, error) {
	if err := in.Validate(); err != nil {
		return domain.Accessory{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	// Default stall to "未分配" when the caller (e.g. file-inbound or a
	// manual create without stall) leaves it blank.
	if strings.TrimSpace(in.Stall) == "" {
		in.Stall = "未分配"
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
// and the optional stall filter, with limit/offset pagination, plus the
// total count under the same filters. Either q or stall may be empty.
func (s *AccessoryService) List(ctx context.Context, q, stall string, limit, offset int) ([]domain.Accessory, int, error) {
	rows, total, err := s.acc.List(ctx, q, stall, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list accessories: %w", err)
	}
	return rows, total, nil
}

// ListStalls returns the distinct stall values in use, for the frontend
// autocomplete / filter dropdown.
func (s *AccessoryService) ListStalls(ctx context.Context) ([]string, error) {
	return s.acc.ListStalls(ctx)
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

// Delete removes an accessory and all its inventory_flow rows in a single
// transaction. Returns the number of flow rows removed (0 when none). The
// schema's FK RESTRICT on inventory_flow.accessory_id is a defense-in-depth
// safety net — because we delete flows first inside the same tx, it never
// fires in the happy path; if some future code path bypasses this method,
// the FK still prevents orphaned flows.
func (s *AccessoryService) Delete(ctx context.Context, id int64) (int64, error) {
	// Verify the accessory exists up-front. This makes "delete 0 rows but
	// return success" impossible — important because the row count alone
	// cannot distinguish "nothing to delete" from "deleted successfully".
	if _, err := s.acc.Get(ctx, id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("delete accessory: lookup: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("delete accessory: begin tx: %w", err)
	}
	// Safe to call even after a successful Commit (Rollback returns sql.ErrTxDone,
	// which we ignore).
	defer tx.Rollback()

	flowN, err := s.flow.DeleteByAccessory(ctx, tx, id)
	if err != nil {
		return 0, fmt.Errorf("delete accessory: delete flows: %w", err)
	}
	if err := s.acc.DeleteTx(ctx, tx, id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			// Race: someone else deleted the accessory between Get and DeleteTx.
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("delete accessory: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("delete accessory: commit: %w", err)
	}

	logOp("accessory", "delete", "id", id, "flows_deleted", flowN)
	return flowN, nil
}