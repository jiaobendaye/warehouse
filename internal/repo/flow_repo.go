package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jiaobendaye/warehouse/internal/domain"
)

// FlowRepo persists and queries inventory_flow ledger rows.
type FlowRepo struct {
	db *sql.DB
}

// NewFlowRepo wires the repo to an open *sql.DB.
func NewFlowRepo(d *sql.DB) *FlowRepo { return &FlowRepo{db: d} }

// Insert appends a single flow row. If tx is non-nil the insert participates
// in that transaction (used by stock operations for atomicity). The unique
// index on client_ref surfaces duplicates as an error, which the service
// layer translates into an idempotent re-return.
func (r *FlowRepo) Insert(ctx context.Context, tx *sql.Tx, f domain.InventoryFlow) (int64, error) {
	q := `INSERT INTO inventory_flow(
			accessory_id, type, quantity, unit_cost, unit_price,
			balance_after, client_ref, remark, occurred_at
		) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, COALESCE(NULLIF(?, ''), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')))`

	args := []any{f.AccessoryID, string(f.Type), f.Quantity, f.UnitCost, f.UnitPrice, f.BalanceAfter, f.ClientRef, f.Remark, f.OccurredAt}

	var (
		res sql.Result
		err error
	)
	if tx != nil {
		res, err = tx.ExecContext(ctx, q, args...)
	} else {
		res, err = r.db.ExecContext(ctx, q, args...)
	}
	if err != nil {
		return 0, fmt.Errorf("insert flow: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// GetByID returns the flow with the given id, or ErrNotFound.
func (r *FlowRepo) GetByID(ctx context.Context, id int64) (domain.InventoryFlow, error) {
	row := r.db.QueryRowContext(ctx, flowSelect+` WHERE id = ?`, id)
	return scanFlow(row)
}

// GetByClientRef returns the existing flow with the matching client_ref, or
// ErrNotFound when no row claims that idempotency key.
func (r *FlowRepo) GetByClientRef(ctx context.Context, clientRef string) (domain.InventoryFlow, error) {
	row := r.db.QueryRowContext(ctx, flowSelect+` WHERE client_ref = ?`, clientRef)
	return scanFlow(row)
}

// List returns flows matching the filter, paginated, plus total under the
// same filter. occurred_at order is descending (newest first) so callers
// can render a most-recent-first ledger view. An empty filter returns all
// flows globally.
func (r *FlowRepo) List(ctx context.Context, f domain.FlowFilter, limit, offset int) ([]domain.InventoryFlow, int, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		conds []string
		args  []any
	)
	if f.AccessoryID > 0 {
		conds = append(conds, "accessory_id = ?")
		args = append(args, f.AccessoryID)
	}
	if f.Type != "" {
		conds = append(conds, "type = ?")
		args = append(args, string(f.Type))
	}
	if f.From != "" {
		conds = append(conds, "occurred_at >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		conds = append(conds, "occurred_at <= ?")
		args = append(args, f.To)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	listSQL := flowSelect + where + " ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?"
	listArgs := append(append([]any{}, args...), limit, offset)
	rows, err := r.db.QueryContext(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list flows: %w", err)
	}
	defer rows.Close()
	out := make([]domain.InventoryFlow, 0)
	for rows.Next() {
		fl, err := scanFlowRows(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, fl)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows iter: %w", err)
	}

	countSQL := "SELECT COUNT(*) FROM inventory_flow" + where
	var total int
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count flows: %w", err)
	}
	return out, total, nil
}

// CountByAccessory returns the number of flow rows attached to an accessory.
// Used by services to gate accessory deletion.
func (r *FlowRepo) CountByAccessory(ctx context.Context, accessoryID int64) (int, error) {
	var n int
	row := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inventory_flow WHERE accessory_id = ?`, accessoryID)
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count flows by accessory: %w", err)
	}
	return n, nil
}

const flowSelect = `SELECT id, accessory_id, type, quantity, unit_cost, unit_price,
	balance_after, COALESCE(client_ref, ''), remark, occurred_at, created_at
	FROM inventory_flow`

func scanFlow(s rowScanner) (domain.InventoryFlow, error) {
	var f domain.InventoryFlow
	var typ string
	err := s.Scan(
		&f.ID, &f.AccessoryID, &typ, &f.Quantity, &f.UnitCost, &f.UnitPrice,
		&f.BalanceAfter, &f.ClientRef, &f.Remark, &f.OccurredAt, &f.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.InventoryFlow{}, ErrNotFound
		}
		return domain.InventoryFlow{}, fmt.Errorf("scan flow: %w", err)
	}
	f.Type = domain.FlowType(typ)
	return f, nil
}

func scanFlowRows(rows *sql.Rows) (domain.InventoryFlow, error) {
	var f domain.InventoryFlow
	var typ string
	err := rows.Scan(
		&f.ID, &f.AccessoryID, &typ, &f.Quantity, &f.UnitCost, &f.UnitPrice,
		&f.BalanceAfter, &f.ClientRef, &f.Remark, &f.OccurredAt, &f.CreatedAt,
	)
	if err != nil {
		return domain.InventoryFlow{}, fmt.Errorf("scan flow rows: %w", err)
	}
	f.Type = domain.FlowType(typ)
	return f, nil
}