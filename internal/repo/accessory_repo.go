// Package repo contains the SQL persistence layer for the warehouse domain.
// Each repo takes a *sql.DB at construction and exposes only typed methods.
package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jiaobendaye/warehouse/internal/domain"
)

// ErrNotFound is returned when a lookup by primary key finds no row.
var ErrNotFound = errors.New("repo: not found")

// AccessoryRepo persists and queries Accessory rows in the accessories table.
type AccessoryRepo struct {
	db *sql.DB
}

// NewAccessoryRepo wires the repo to an open *sql.DB.
func NewAccessoryRepo(d *sql.DB) *AccessoryRepo { return &AccessoryRepo{db: d} }

// Create inserts a new accessory. SKU is enforced unique by the schema; a
// conflict surfaces as the underlying SQLite UNIQUE error.
func (r *AccessoryRepo) Create(ctx context.Context, in domain.Accessory) (domain.Accessory, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO accessories(sku, name, unit, current_stock, low_stock_threshold, notes)
		 VALUES (?, ?, ?, 0, ?, ?)`,
		in.SKU, in.Name, in.Unit, in.LowStockThreshold, in.Notes,
	)
	if err != nil {
		return domain.Accessory{}, fmt.Errorf("insert accessory: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Accessory{}, fmt.Errorf("last insert id: %w", err)
	}
	return r.Get(ctx, id)
}

// Get loads an accessory by primary key.
func (r *AccessoryRepo) Get(ctx context.Context, id int64) (domain.Accessory, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, sku, name, unit, current_stock, low_stock_threshold, notes, created_at, updated_at
		 FROM accessories WHERE id = ?`, id)
	return scanAccessory(row)
}

// GetBySKU loads an accessory by its unique SKU.
func (r *AccessoryRepo) GetBySKU(ctx context.Context, sku string) (domain.Accessory, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, sku, name, unit, current_stock, low_stock_threshold, notes, created_at, updated_at
		 FROM accessories WHERE sku = ?`, sku)
	return scanAccessory(row)
}

// List returns accessories whose SKU or NAME contains q (case-insensitive),
// paginated by limit/offset, plus the total count under the same filter.
func (r *AccessoryRepo) List(ctx context.Context, q string, limit, offset int) ([]domain.Accessory, int, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		rows *sql.Rows
		err  error
	)
	if q == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, sku, name, unit, current_stock, low_stock_threshold, notes, created_at, updated_at
			 FROM accessories ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
			limit, offset)
	} else {
		like := "%" + q + "%"
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, sku, name, unit, current_stock, low_stock_threshold, notes, created_at, updated_at
			 FROM accessories
			 WHERE sku LIKE ? COLLATE NOCASE OR name LIKE ? COLLATE NOCASE
			 ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
			like, like, limit, offset)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list query: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Accessory, 0)
	for rows.Next() {
		a, err := scanAccessoryRows(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows iter: %w", err)
	}

	var total int
	if q == "" {
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accessories`).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count all: %w", err)
		}
	} else {
		like := "%" + q + "%"
		if err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM accessories
			 WHERE sku LIKE ? COLLATE NOCASE OR name LIKE ? COLLATE NOCASE`,
			like, like).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count filtered: %w", err)
		}
	}
	return out, total, nil
}

// Update applies the provided partial update. SKU is intentionally not
// writable. Updated_at is bumped via SQL CURRENT_TIMESTAMP.
func (r *AccessoryRepo) Update(ctx context.Context, id int64, u domain.AccessoryUpdate) (domain.Accessory, error) {
	cur, err := r.Get(ctx, id)
	if err != nil {
		return domain.Accessory{}, err
	}
	if u.Name != nil {
		cur.Name = *u.Name
	}
	if u.Unit != nil {
		cur.Unit = *u.Unit
	}
	if u.LowStockThreshold != nil {
		cur.LowStockThreshold = *u.LowStockThreshold
	}
	if u.Notes != nil {
		cur.Notes = *u.Notes
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE accessories
		 SET name = ?, unit = ?, low_stock_threshold = ?, notes = ?,
		     updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		 WHERE id = ?`,
		cur.Name, cur.Unit, cur.LowStockThreshold, cur.Notes, id,
	); err != nil {
		return domain.Accessory{}, fmt.Errorf("update accessory: %w", err)
	}
	return r.Get(ctx, id)
}

// Delete removes an accessory by id. The schema's foreign-key RESTRICT on
// inventory_flow.accessory_id causes this to fail when flows exist; that
// is the intended contract — services translate it to a "has flow" error.
func (r *AccessoryRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM accessories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete accessory: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AdjustStock atomically applies delta to current_stock. Negative deltas
// (outbound) are rejected by the caller; this method is the safe primitive.
func (r *AccessoryRepo) AdjustStock(ctx context.Context, tx *sql.Tx, id, delta int64) error {
	q := `UPDATE accessories SET current_stock = current_stock + ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`
	var err error
	if tx != nil {
		_, err = tx.ExecContext(ctx, q, delta, id)
	} else {
		_, err = r.db.ExecContext(ctx, q, delta, id)
	}
	if err != nil {
		return fmt.Errorf("adjust stock: %w", err)
	}
	return nil
}

// SetStock overwrites current_stock. Used inside transactions to reflect
// the new balance after a flow is recorded.
func (r *AccessoryRepo) SetStock(ctx context.Context, tx *sql.Tx, id, stock int64) error {
	q := `UPDATE accessories SET current_stock = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`
	var err error
	if tx != nil {
		_, err = tx.ExecContext(ctx, q, stock, id)
	} else {
		_, err = r.db.ExecContext(ctx, q, stock, id)
	}
	if err != nil {
		return fmt.Errorf("set stock: %w", err)
	}
	return nil
}

// GetStockTx returns the current stock within a transaction. Useful when
// the caller needs to check availability atomically with an update.
func (r *AccessoryRepo) GetStockTx(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	var stock int64
	row := tx.QueryRowContext(ctx, `SELECT current_stock FROM accessories WHERE id = ?`, id)
	if err := row.Scan(&stock); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("get stock: %w", err)
	}
	return stock, nil
}

// --- scan helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccessory(s rowScanner) (domain.Accessory, error) {
	var a domain.Accessory
	err := s.Scan(
		&a.ID, &a.SKU, &a.Name, &a.Unit, &a.CurrentStock,
		&a.LowStockThreshold, &a.Notes, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Accessory{}, ErrNotFound
		}
		return domain.Accessory{}, fmt.Errorf("scan accessory: %w", err)
	}
	return a, nil
}

func scanAccessoryRows(rows *sql.Rows) (domain.Accessory, error) {
	var a domain.Accessory
	err := rows.Scan(
		&a.ID, &a.SKU, &a.Name, &a.Unit, &a.CurrentStock,
		&a.LowStockThreshold, &a.Notes, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return domain.Accessory{}, fmt.Errorf("scan rows: %w", err)
	}
	return a, nil
}