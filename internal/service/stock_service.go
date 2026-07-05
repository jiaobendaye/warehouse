// Package service — StockService implements inbound and outbound stock
// movements with strict atomicity and idempotency guarantees.
//
// Each single-row operation (Inbound, Outbound) wraps stock adjustment and
// flow-row insertion in a single *sql.Tx so partial writes are impossible.
// Each batch (BatchInbound, BatchOutbound) opens a single transaction
// covering every row in the batch, so any failure — invalid input, missing
// accessory, insufficient stock — rolls back every adjustment in that
// batch.
//
// Idempotency is enforced at the application layer via client_ref: when
// the caller supplies a non-empty ClientRef and a flow with that ref
// already exists, the service returns the original flow without touching
// stock. When the ref is empty, no idempotency check runs.
//
// All sentinel errors are returned wrapped with %w so transport layers can
// map via errors.Is.
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

// ErrInsufficientStock is returned by StockService when an outbound would
// drive current_stock below zero. Transport layers (REST/MCP) should map
// this to 409 Conflict with code=INSUFFICIENT_STOCK. ErrInvalidInput and
// ErrNotFound are declared in accessory_service.go and shared across the
// service package.
var ErrInsufficientStock = errors.New("service: insufficient stock")

// InboundCmd is the request payload for a single inbound (stock-in) operation.
// UnitCost, Remark and OccurredAt are optional. ClientRef is the idempotency
// key; if non-empty and a flow with the same client_ref already exists, the
// original flow is returned and no state is changed.
type InboundCmd struct {
	AccessoryID int64   `json:"accessory_id"`
	Quantity    int64   `json:"quantity"`
	UnitCost    float64 `json:"unit_cost,omitempty"`
	Remark      string  `json:"remark,omitempty"`
	OccurredAt  string  `json:"occurred_at,omitempty"`
	ClientRef   string  `json:"client_ref,omitempty"`
}

// OutboundCmd is the request payload for a single outbound (stock-out)
// operation. Fields mirror InboundCmd except UnitPrice replaces UnitCost.
type OutboundCmd struct {
	AccessoryID int64   `json:"accessory_id"`
	Quantity    int64   `json:"quantity"`
	UnitPrice   float64 `json:"unit_price,omitempty"`
	Remark      string  `json:"remark,omitempty"`
	OccurredAt  string  `json:"occurred_at,omitempty"`
	ClientRef   string  `json:"client_ref,omitempty"`
}

// BatchResult summarises a batch operation. Accepted counts the rows that
// succeeded; Flows and IDs mirror each other and contain every row in
// commit order (which is the same as the input order).
type BatchResult struct {
	Accepted int                  `json:"accepted"`
	Flows    []domain.InventoryFlow `json:"flows"`
	IDs      []int64              `json:"ids"`
}

// StockService is the business-logic entry point for stock movements.
// It owns transaction boundaries, idempotency, and the stock-availability
// check on outbound. It never touches the database directly — only through
// its repos.
type StockService struct {
	acc  *repo.AccessoryRepo
	flow *repo.FlowRepo
	db   *sql.DB
}

// NewStockService wires the service to its repos and the underlying DB.
// All three arguments are required; nil panics because there is no
// sensible default for a transactional service layer that lacks any of
// these dependencies.
func NewStockService(acc *repo.AccessoryRepo, flow *repo.FlowRepo, db *sql.DB) *StockService {
	if acc == nil || flow == nil || db == nil {
		panic("service.NewStockService: acc, flow, db must not be nil")
	}
	return &StockService{acc: acc, flow: flow, db: db}
}

// Inbound records a stock-in. Atomicity: tx wraps adjust + insert. Idempotency:
// non-empty ClientRef short-circuits to the original flow when one exists.
func (s *StockService) Inbound(ctx context.Context, in InboundCmd) (domain.InventoryFlow, error) {
	if existing, err := s.checkClientRefIdempotent(ctx, in.ClientRef); err != nil {
		return domain.InventoryFlow{}, err
	} else if existing.ID != 0 {
		return existing, nil
	}
	if in.Quantity <= 0 {
		return domain.InventoryFlow{}, fmt.Errorf("%w: quantity must be positive", ErrInvalidInput)
	}
	if in.AccessoryID <= 0 {
		return domain.InventoryFlow{}, fmt.Errorf("%w: accessory_id is required", ErrInvalidInput)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	cur, err := s.acc.GetStockTx(ctx, tx, in.AccessoryID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.InventoryFlow{}, ErrNotFound
		}
		return domain.InventoryFlow{}, fmt.Errorf("get stock: %w", err)
	}
	newStock := cur + in.Quantity
	if err := s.acc.SetStock(ctx, tx, in.AccessoryID, newStock); err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("set stock: %w", err)
	}

	flow := domain.InventoryFlow{
		AccessoryID:  in.AccessoryID,
		Type:         domain.FlowTypeIn,
		Quantity:     in.Quantity,
		UnitCost:     in.UnitCost,
		BalanceAfter: newStock,
		ClientRef:    in.ClientRef,
		Remark:       in.Remark,
		OccurredAt:   in.OccurredAt,
	}
	if err := flow.Validate(); err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	id, err := s.flow.Insert(ctx, tx, flow)
	if err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("insert flow: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("commit: %w", err)
	}
	committed = true

	out, err := s.flow.GetByID(ctx, id)
	if err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("reload flow: %w", err)
	}
	logOp("stock", "inbound", "flow_id", out.ID, "accessory_id", out.AccessoryID, "qty", out.Quantity, "balance_after", out.BalanceAfter, "client_ref", out.ClientRef)
	return out, nil
}

// Outbound records a stock-out. Atomicity: tx wraps check + adjust + insert.
// Idempotency: same as Inbound.
func (s *StockService) Outbound(ctx context.Context, in OutboundCmd) (domain.InventoryFlow, error) {
	if existing, err := s.checkClientRefIdempotent(ctx, in.ClientRef); err != nil {
		return domain.InventoryFlow{}, err
	} else if existing.ID != 0 {
		return existing, nil
	}
	if in.Quantity <= 0 {
		return domain.InventoryFlow{}, fmt.Errorf("%w: quantity must be positive", ErrInvalidInput)
	}
	if in.AccessoryID <= 0 {
		return domain.InventoryFlow{}, fmt.Errorf("%w: accessory_id is required", ErrInvalidInput)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	cur, err := s.acc.GetStockTx(ctx, tx, in.AccessoryID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.InventoryFlow{}, ErrNotFound
		}
		return domain.InventoryFlow{}, fmt.Errorf("get stock: %w", err)
	}
	if cur < in.Quantity {
		return domain.InventoryFlow{}, fmt.Errorf("%w: have %d, need %d",
			ErrInsufficientStock, cur, in.Quantity)
	}
	newStock := cur - in.Quantity
	if err := s.acc.SetStock(ctx, tx, in.AccessoryID, newStock); err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("set stock: %w", err)
	}

	flow := domain.InventoryFlow{
		AccessoryID:  in.AccessoryID,
		Type:         domain.FlowTypeOut,
		Quantity:     in.Quantity,
		UnitPrice:    in.UnitPrice,
		BalanceAfter: newStock,
		ClientRef:    in.ClientRef,
		Remark:       in.Remark,
		OccurredAt:   in.OccurredAt,
	}
	if err := flow.Validate(); err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	id, err := s.flow.Insert(ctx, tx, flow)
	if err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("insert flow: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("commit: %w", err)
	}
	committed = true

	out, err := s.flow.GetByID(ctx, id)
	if err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("reload flow: %w", err)
	}
	logOp("stock", "outbound", "flow_id", out.ID, "accessory_id", out.AccessoryID, "qty", out.Quantity, "balance_after", out.BalanceAfter, "client_ref", out.ClientRef)
	return out, nil
}

// BatchInbound applies N inbound operations under one transaction.
// Pre-validation runs first (outside the tx) so invalid input fails fast
// without holding a write lock. Validation checks each row's accessory
// existence and rejects duplicate accessory_id within the batch — two
// rows on the same id is almost always a caller mistake (merge quantities
// client-side).
func (s *StockService) BatchInbound(ctx context.Context, items []InboundCmd) (BatchResult, error) {
	if len(items) == 0 {
		return BatchResult{}, fmt.Errorf("%w: batch must not be empty", ErrInvalidInput)
	}
	seen := make(map[int64]int, len(items))
	for i, it := range items {
		if it.AccessoryID <= 0 {
			return BatchResult{}, fmt.Errorf("%w: row %d: accessory_id is required",
				ErrInvalidInput, i)
		}
		if it.Quantity <= 0 {
			return BatchResult{}, fmt.Errorf("%w: row %d: quantity must be positive",
				ErrInvalidInput, i)
		}
		if _, err := s.acc.Get(ctx, it.AccessoryID); err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return BatchResult{}, fmt.Errorf("%w: row %d: accessory %d not found",
					ErrNotFound, i, it.AccessoryID)
			}
			return BatchResult{}, fmt.Errorf("row %d lookup accessory: %w", i, err)
		}
		if prev, ok := seen[it.AccessoryID]; ok {
			return BatchResult{}, fmt.Errorf("%w: row %d: duplicate accessory_id %d (also at row %d)",
				ErrInvalidInput, i, it.AccessoryID, prev)
		}
		seen[it.AccessoryID] = i
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BatchResult{}, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result := BatchResult{
		Flows: make([]domain.InventoryFlow, 0, len(items)),
		IDs:   make([]int64, 0, len(items)),
	}

	for i, it := range items {
		cur, err := s.acc.GetStockTx(ctx, tx, it.AccessoryID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return BatchResult{}, fmt.Errorf("%w: row %d: accessory %d not found",
					ErrNotFound, i, it.AccessoryID)
			}
			return BatchResult{}, fmt.Errorf("row %d get stock: %w", i, err)
		}
		newStock := cur + it.Quantity
		if err := s.acc.SetStock(ctx, tx, it.AccessoryID, newStock); err != nil {
			return BatchResult{}, fmt.Errorf("row %d set stock: %w", i, err)
		}
		flow := domain.InventoryFlow{
			AccessoryID:  it.AccessoryID,
			Type:         domain.FlowTypeIn,
			Quantity:     it.Quantity,
			UnitCost:     it.UnitCost,
			BalanceAfter: newStock,
			ClientRef:    it.ClientRef,
			Remark:       it.Remark,
			OccurredAt:   it.OccurredAt,
		}
		if err := flow.Validate(); err != nil {
			return BatchResult{}, fmt.Errorf("%w: row %d: %s",
				ErrInvalidInput, i, err.Error())
		}
		id, err := s.flow.Insert(ctx, tx, flow)
		if err != nil {
			return BatchResult{}, fmt.Errorf("row %d insert flow: %w", i, err)
		}
		flow.ID = id
		result.Flows = append(result.Flows, flow)
		result.IDs = append(result.IDs, id)
	}

	if err := tx.Commit(); err != nil {
		return BatchResult{}, fmt.Errorf("commit: %w", err)
	}
	committed = true
	result.Accepted = len(items)
	logOp("stock", "batch_inbound", "accepted", result.Accepted, "total", len(items))
	return result, nil
}

// BatchOutbound applies N outbound operations under one transaction.
// Pre-validation runs first so invalid input fails fast.
func (s *StockService) BatchOutbound(ctx context.Context, items []OutboundCmd) (BatchResult, error) {
	if len(items) == 0 {
		return BatchResult{}, fmt.Errorf("%w: batch must not be empty", ErrInvalidInput)
	}
	for i, it := range items {
		if it.AccessoryID <= 0 {
			return BatchResult{}, fmt.Errorf("%w: row %d: accessory_id is required",
				ErrInvalidInput, i)
		}
		if it.Quantity <= 0 {
			return BatchResult{}, fmt.Errorf("%w: row %d: quantity must be positive",
				ErrInvalidInput, i)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BatchResult{}, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result := BatchResult{
		Flows: make([]domain.InventoryFlow, 0, len(items)),
		IDs:   make([]int64, 0, len(items)),
	}

	for i, it := range items {
		cur, err := s.acc.GetStockTx(ctx, tx, it.AccessoryID)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return BatchResult{}, fmt.Errorf("%w: row %d: accessory %d not found",
					ErrNotFound, i, it.AccessoryID)
			}
			return BatchResult{}, fmt.Errorf("row %d get stock: %w", i, err)
		}
		if cur < it.Quantity {
			return BatchResult{}, fmt.Errorf("%w: row %d: accessory %d have %d, need %d",
				ErrInsufficientStock, i, it.AccessoryID, cur, it.Quantity)
		}
		newStock := cur - it.Quantity
		if err := s.acc.SetStock(ctx, tx, it.AccessoryID, newStock); err != nil {
			return BatchResult{}, fmt.Errorf("row %d set stock: %w", i, err)
		}
		flow := domain.InventoryFlow{
			AccessoryID:  it.AccessoryID,
			Type:         domain.FlowTypeOut,
			Quantity:     it.Quantity,
			UnitPrice:    it.UnitPrice,
			BalanceAfter: newStock,
			ClientRef:    it.ClientRef,
			Remark:       it.Remark,
			OccurredAt:   it.OccurredAt,
		}
		if err := flow.Validate(); err != nil {
			return BatchResult{}, fmt.Errorf("%w: row %d: %s",
				ErrInvalidInput, i, err.Error())
		}
		id, err := s.flow.Insert(ctx, tx, flow)
		if err != nil {
			return BatchResult{}, fmt.Errorf("row %d insert flow: %w", i, err)
		}
		flow.ID = id
		result.Flows = append(result.Flows, flow)
		result.IDs = append(result.IDs, id)
	}

	if err := tx.Commit(); err != nil {
		return BatchResult{}, fmt.Errorf("commit: %w", err)
	}
	committed = true
	result.Accepted = len(items)
	logOp("stock", "batch_outbound", "accepted", result.Accepted, "total", len(items))
	return result, nil
}

// FileOutboundItem is one line from a parsed xlsx.
type FileOutboundItem struct {
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
}

// FileForceOutboundResult summarises a force-outbound execution.
type FileForceOutboundResult struct {
	Outbound  int                    `json:"outbound"`
	Created   int                    `json:"created"`
	Shortages int                    `json:"shortages"`
	Flows     []domain.InventoryFlow `json:"flows"`
	IDs       []int64                `json:"ids"`
}

// FileForceOutbound executes a batch outbound with lenient handling:
//   - Missing accessories are auto-created with stock=0.
//   - When stock < needed, current_stock is set to 0 and the
//     low_stock_threshold is increased by the shortage.
//   - When stock ≥ needed, normal outbound logic applies.
//
// All rows run under a single transaction — any unexpected DB error
// rolls everything back.
func (s *StockService) FileForceOutbound(ctx context.Context, items []FileOutboundItem) (FileForceOutboundResult, error) {
	if len(items) == 0 {
		return FileForceOutboundResult{}, fmt.Errorf("%w: batch must not be empty", ErrInvalidInput)
	}
	for i, it := range items {
		if it.Quantity <= 0 {
			return FileForceOutboundResult{}, fmt.Errorf("%w: row %d: quantity must be positive", ErrInvalidInput, i)
		}
		if strings.TrimSpace(it.Name) == "" {
			return FileForceOutboundResult{}, fmt.Errorf("%w: row %d: name is required", ErrInvalidInput, i)
		}
	}

	// Pre-resolve every name → accessory, creating missing ones.
	type row struct {
		acc      domain.Accessory
		qty      int64
		shortage int64 // qty - stock when stock < qty, else 0
		created  bool
	}
	rows := make([]row, len(items))
	createdCount := 0
	shortageCount := 0

	for i, it := range items {
		a, err := s.acc.GetByName(ctx, it.Name)
		if errors.Is(err, repo.ErrNotFound) {
			a, err = s.acc.Create(ctx, domain.Accessory{Name: it.Name, LowStockThreshold: 0})
			if err != nil {
				return FileForceOutboundResult{}, fmt.Errorf("row %d create %q: %w", i, it.Name, err)
			}
			createdCount++
			rows[i].created = true
		} else if err != nil {
			return FileForceOutboundResult{}, fmt.Errorf("row %d lookup %q: %w", i, it.Name, err)
		}
		rows[i].acc = a
		rows[i].qty = it.Quantity
		if a.CurrentStock < it.Quantity {
			rows[i].shortage = it.Quantity - a.CurrentStock
			shortageCount++
		}
	}

	// Execute everything in one tx.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FileForceOutboundResult{}, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result := FileForceOutboundResult{
		Flows:   make([]domain.InventoryFlow, 0, len(items)),
		IDs:     make([]int64, 0, len(items)),
		Outbound: len(items),
		Created:  createdCount,
		Shortages: shortageCount,
	}

	for i, r := range rows {
		// Re-read stock inside tx for correctness.
		cur, err := s.acc.GetStockTx(ctx, tx, r.acc.ID)
		if err != nil {
			return FileForceOutboundResult{}, fmt.Errorf("row %d get stock: %w", i, err)
		}

		newStock := cur - r.qty
		if newStock < 0 {
			newStock = 0
		}
		if err := s.acc.SetStock(ctx, tx, r.acc.ID, newStock); err != nil {
			return FileForceOutboundResult{}, fmt.Errorf("row %d set stock: %w", i, err)
		}

		// Increase threshold by the actual shortage.
		if r.shortage > 0 {
			newThresh := r.acc.LowStockThreshold + r.shortage
			if err := s.acc.SetThresholdTx(ctx, tx, r.acc.ID, newThresh); err != nil {
				return FileForceOutboundResult{}, fmt.Errorf("row %d update threshold: %w", i, err)
			}
		}

		flow := domain.InventoryFlow{
			AccessoryID:  r.acc.ID,
			Type:         domain.FlowTypeOut,
			Quantity:     r.qty,
			BalanceAfter: newStock,
			Remark:       "文件批量出库",
		}
		if err := flow.Validate(); err != nil {
			return FileForceOutboundResult{}, fmt.Errorf("row %d validate flow: %w", i, err)
		}
		id, err := s.flow.Insert(ctx, tx, flow)
		if err != nil {
			return FileForceOutboundResult{}, fmt.Errorf("row %d insert flow: %w", i, err)
		}
		flow.ID = id
		result.Flows = append(result.Flows, flow)
		result.IDs = append(result.IDs, id)
	}

	if err := tx.Commit(); err != nil {
		return FileForceOutboundResult{}, fmt.Errorf("commit: %w", err)
	}
	committed = true

	logOp("stock", "file_force_outbound", "accepted", result.Outbound, "created", result.Created, "shortages", result.Shortages)
	return result, nil
}

// checkClientRefIdempotent returns the existing flow (and a nil err) when a
// non-empty ClientRef matches an existing flow. Empty ClientRef skips the
// check entirely. Repo ErrNotFound is the expected "no match" signal and
// is swallowed; any other error is propagated.
func (s *StockService) checkClientRefIdempotent(ctx context.Context, ref string) (domain.InventoryFlow, error) {
	if ref == "" {
		return domain.InventoryFlow{}, nil
	}
	existing, err := s.flow.GetByClientRef(ctx, ref)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return domain.InventoryFlow{}, nil
		}
		return domain.InventoryFlow{}, fmt.Errorf("idempotency check: %w", err)
	}
	return existing, nil
}