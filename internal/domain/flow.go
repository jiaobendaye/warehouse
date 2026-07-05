package domain

import "errors"

// FlowType identifies whether a ledger row increases or decreases stock.
type FlowType string

const (
	FlowTypeIn  FlowType = "in"
	FlowTypeOut FlowType = "out"
)

// Valid reports whether the FlowType is one of the recognised values.
func (t FlowType) Valid() bool {
	return t == FlowTypeIn || t == FlowTypeOut
}

// InventoryFlow is one immutable ledger entry. Every inbound/outbound
// operation writes exactly one row and updates the matching accessory's
// current_stock atomically.
type InventoryFlow struct {
	ID           int64    `json:"id"`
	AccessoryID  int64    `json:"accessory_id"`
	Type         FlowType `json:"type"`
	Quantity     int64    `json:"quantity"`
	UnitCost     float64  `json:"unit_cost"`
	UnitPrice    float64  `json:"unit_price"`
	BalanceAfter int64    `json:"balance_after"`
	ClientRef    string   `json:"client_ref,omitempty"`
	Remark       string   `json:"remark,omitempty"`
	OccurredAt   string   `json:"occurred_at"`
	CreatedAt    string   `json:"created_at"`
}

// Validate ensures an InventoryFlow is internally consistent before it is
// handed to the repo. It does NOT check stock availability (that lives in
// the service layer); only that required fields are well-formed.
func (f InventoryFlow) Validate() error {
	if f.AccessoryID <= 0 {
		return errors.New("accessory_id is required")
	}
	if !f.Type.Valid() {
		return errors.New("type must be 'in' or 'out'")
	}
	if f.Quantity <= 0 {
		return errors.New("quantity must be positive")
	}
	if f.UnitCost < 0 {
		return errors.New("unit_cost must be non-negative")
	}
	if f.UnitPrice < 0 {
		return errors.New("unit_price must be non-negative")
	}
	return nil
}

// FlowFilter narrows a flow query. Zero-valued fields are ignored.
type FlowFilter struct {
	AccessoryID int64    `json:"accessory_id,omitempty"`
	Type        FlowType `json:"type,omitempty"`
	From        string   `json:"from,omitempty"`
	To          string   `json:"to,omitempty"`
}