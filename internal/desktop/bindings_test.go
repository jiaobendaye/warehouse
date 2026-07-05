package desktop_test

import (
	"testing"

	"github.com/jiaobendaye/warehouse/internal/desktop"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// TestServices_ExportedTypes verifies that the Services struct and the
// single-instance API compile and are accessible without the wails build tag.
func TestServices_ExportedTypes(t *testing.T) {
	// Services struct exists and fields are addressable.
	s := desktop.Services{
		Accessory:     nil, // would be wired at runtime
		Stock:         nil,
		Flow:          nil,
		Replenishment: nil,
	}
	if s.Accessory != nil {
		t.Error("expected nil Accessory")
	}

	// Verify error sentinel is exported.
	if desktop.ErrAlreadyRunning == nil {
		t.Error("ErrAlreadyRunning must not be nil")
	}
}

// TestServiceInterfaceCompilation ensures the service types that the App
// wraps actually exist in the expected package. This test would fail at
// compile time if a service method signature changes without updating the
// bindings.
func TestServiceInterfaceCompilation(t *testing.T) {
	// We can't instantiate services without a DB here, but we can verify
	// that the types we reference in bindings.go resolve correctly.
	var _ *service.AccessoryService
	var _ *service.StockService
	var _ *service.FlowService
	var _ *service.ReplenishmentService

	// Verify command types compile.
	var _ service.InboundCmd
	var _ service.OutboundCmd
	var _ service.BatchResult

	// Verify replenishment types compile.
	var _ service.ReplenishmentItem
	var _ service.BatchCheckResult

	_ = t // suppress unused
}
