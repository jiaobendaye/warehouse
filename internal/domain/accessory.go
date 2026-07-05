// Package domain holds the data types and lightweight validation rules for
// the warehouse business. Repos and services depend on this package, never
// the other way around.
package domain

import (
	"errors"
	"strings"
)

// Accessory is one phone-accessory item tracked by the system. Name is the
// unique business identifier. Quantity is always counted in 个 (single
// units) — there is no separate unit field.
type Accessory struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	CurrentStock      int64  `json:"current_stock"`
	LowStockThreshold int64  `json:"low_stock_threshold"`
	Notes             string `json:"notes"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// AccessoryUpdate carries the mutable fields of an accessory.
type AccessoryUpdate struct {
	Name              *string `json:"name,omitempty"`
	LowStockThreshold *int64  `json:"low_stock_threshold,omitempty"`
	Notes             *string `json:"notes,omitempty"`
}

// Validate checks that an Accessory passed to Create has all required fields
// and that the threshold is non-negative.
func (a Accessory) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("name is required")
	}
	if a.LowStockThreshold < 0 {
		return errors.New("low_stock_threshold must be non-negative")
	}
	return nil
}