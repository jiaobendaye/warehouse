package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jiaobendaye/warehouse/internal/api"
	"github.com/jiaobendaye/warehouse/internal/db"
	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/repo"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// Test wiring: build an in-memory SQLite + services + router.
func newRouter(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	accRepo := repo.NewAccessoryRepo(d)
	flowRepo := repo.NewFlowRepo(d)
	accSvc := service.NewAccessoryService(accRepo, flowRepo)
	stockSvc := service.NewStockService(accRepo, flowRepo, d)
	flowSvc := service.NewFlowService(flowRepo)
	replSvc := service.NewReplenishmentService(accRepo)

	svcs := api.Services{
		Accessory:      accSvc,
		Stock:          stockSvc,
		Flow:           flowSvc,
		Replenishment:  replSvc,
	}

	return api.NewRouter(svcs, api.RouterOptions{
		AllowedOrigins: []string{"http://127.0.0.1:17880"},
	})
}

// httpDo is a convenience helper that runs a request against the test server
// and returns the response, decoded JSON body (if Content-Type is JSON), and
// the raw body string.
func httpDo(t *testing.T, h http.Handler, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, raw
}

// decodeEnvelope reads either an error envelope ({"error":{...}}) or a
// non-error body, returning a generic map for assertion convenience.
func decodeEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode json: %v (body=%s)", err, raw)
	}
	return m
}

func mustOK(t *testing.T, resp *http.Response, raw []byte) map[string]any {
	t.Helper()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("expected 2xx, got %d body=%s", resp.StatusCode, raw)
	}
	return decodeEnvelope(t, raw)
}

func mustError(t *testing.T, resp *http.Response, raw []byte, wantStatus int, wantCode string) map[string]any {
	t.Helper()
	if resp.StatusCode != wantStatus {
		t.Fatalf("expected status %d, got %d body=%s", wantStatus, resp.StatusCode, raw)
	}
	m := decodeEnvelope(t, raw)
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got body=%s", raw)
	}
	if errObj["code"] != wantCode {
		t.Fatalf("expected code=%s, got %v body=%s", wantCode, errObj["code"], raw)
	}
	if _, ok := errObj["message"].(string); !ok {
		t.Fatalf("expected error.message string, got %v", errObj["message"])
	}
	return m
}

func newAccessory(t *testing.T, h http.Handler, sku string) domain.Accessory {
	t.Helper()
	body := domain.Accessory{
		SKU:               sku,
		Name:              "保护壳 " + sku,
		LowStockThreshold: 5,
	}
	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/accessories", body)
	m := mustOK(t, resp, raw)
	idF, _ := m["id"].(float64)
	out, _ := json.Marshal(m["id"])
	var id int64
	_ = json.Unmarshal(out, &id)
	body.ID = int64(idF)
	if id != 0 {
		body.ID = id
	}
	return body
}

// TestAccessories_FullCRUD — happy path: Create → Get → Update → List → Delete.
func TestAccessories_FullCRUD(t *testing.T) {
	h := newRouter(t)

	// Create
	body := domain.Accessory{
		SKU:               "SKU-A",
		Name:              "保护壳",
		LowStockThreshold: 3,
		Notes:             "iPhone 15",
	}
	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/accessories", body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create status: %d body=%s", resp.StatusCode, raw)
	}
	m := decodeEnvelope(t, raw)
	idF, _ := m["id"].(float64)
	id := int64(idF)
	if id == 0 {
		t.Fatalf("no id in response: %s", raw)
	}
	if sku, _ := m["sku"].(string); sku != "SKU-A" {
		t.Fatalf("sku mismatch: %v", m["sku"])
	}

	// Get
	resp, raw = httpDo(t, h, http.MethodGet, fmt.Sprintf("/api/v1/accessories/%d", id), nil)
	m = mustOK(t, resp, raw)
	if m["sku"] != "SKU-A" {
		t.Fatalf("get sku: %v", m["sku"])
	}

	// Update (name only)
	resp, raw = httpDo(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/accessories/%d", id),
		domain.AccessoryUpdate{Name: pStr("updated")})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status: %d body=%s", resp.StatusCode, raw)
	}
	m = decodeEnvelope(t, raw)
	if m["name"] != "updated" {
		t.Fatalf("update name: %v", m["name"])
	}

	// List
	resp, raw = httpDo(t, h, http.MethodGet, "/api/v1/accessories", nil)
	m = mustOK(t, resp, raw)
	items, _ := m["items"].([]any)
	if len(items) < 1 {
		t.Fatalf("list empty: %s", raw)
	}

	// Delete
	resp, raw = httpDo(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/accessories/%d", id), nil)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status: %d body=%s", resp.StatusCode, raw)
	}
}

// TestAccessories_DuplicateSKU_409 — second create with same SKU returns 409 CONFLICT.
func TestAccessories_DuplicateSKU_409(t *testing.T) {
	h := newRouter(t)
	body := domain.Accessory{SKU: "DUPE", Name: "保护壳"}
	resp, _ := httpDo(t, h, http.MethodPost, "/api/v1/accessories", body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("first create failed: %d", resp.StatusCode)
	}
	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/accessories", body)
	mustError(t, resp, raw, http.StatusConflict, "CONFLICT")
}

// TestAccessories_DeleteWithFlows_409 — create + inbound + delete returns 409 CONFLICT.
func TestAccessories_DeleteWithFlows_409(t *testing.T) {
	h := newRouter(t)
	acc := newAccessory(t, h, "HAF")

	inbound := service.InboundCmd{AccessoryID: acc.ID, Quantity: 2}
	resp, _ := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound", inbound)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("inbound status=%d", resp.StatusCode)
	}

	resp, raw := httpDo(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/accessories/%d", acc.ID), nil)
	mustError(t, resp, raw, http.StatusConflict, "CONFLICT")
}

// TestStock_Inbound_UpdatesAndReturnsFlow — POST inbound, verify response + flow list contains it.
func TestStock_Inbound_UpdatesAndReturnsFlow(t *testing.T) {
	h := newRouter(t)
	acc := newAccessory(t, h, "INB")

	inbound := service.InboundCmd{AccessoryID: acc.ID, Quantity: 7, UnitCost: 1.5, ClientRef: "inv-1", Remark: "测试入库"}
	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound", inbound)
	m := mustOK(t, resp, raw)
	if int(m["balance_after"].(float64)) != 7 {
		t.Fatalf("balance_after: %v", m["balance_after"])
	}
	if m["client_ref"] != "inv-1" {
		t.Fatalf("client_ref: %v", m["client_ref"])
	}
	idF, _ := m["id"].(float64)
	flowID := int64(idF)

	// Verify the flow is queryable.
	resp, raw = httpDo(t, h, http.MethodGet, fmt.Sprintf("/api/v1/flows/%d", flowID), nil)
	m = mustOK(t, resp, raw)
	if int(m["accessory_id"].(float64)) != int(acc.ID) {
		t.Fatalf("accessory_id mismatch: %v", m["accessory_id"])
	}
}

// TestStock_Outbound_Insufficient_409 — returns 409 with code INSUFFICIENT_STOCK.
func TestStock_Outbound_Insufficient_409(t *testing.T) {
	h := newRouter(t)
	acc := newAccessory(t, h, "OFB")

	inbound := service.InboundCmd{AccessoryID: acc.ID, Quantity: 3}
	resp, _ := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound", inbound)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("inbound fail: %d", resp.StatusCode)
	}

	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/stock/outbound",
		service.OutboundCmd{AccessoryID: acc.ID, Quantity: 5})
	mustError(t, resp, raw, http.StatusConflict, "INSUFFICIENT_STOCK")
}

// TestStock_BatchInbound_AllOrNothing — partial failure returns 4xx; no state change.
func TestStock_BatchInbound_AllOrNothing(t *testing.T) {
	h := newRouter(t)
	acc := newAccessory(t, h, "BAT")

	// 2nd item references a non-existent accessory to trigger a rollback.
	items := []service.InboundCmd{
		{AccessoryID: acc.ID, Quantity: 4},
		{AccessoryID: 9999, Quantity: 1},
	}
	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/stock/batch_inbound", items)
	if resp.StatusCode/100 == 2 {
		t.Fatalf("expected failure, got %d body=%s", resp.StatusCode, raw)
	}
	mustError(t, resp, raw, http.StatusNotFound, "NOT_FOUND")

	// Stock for acc must be unchanged (0).
	resp, raw = httpDo(t, h, http.MethodGet, fmt.Sprintf("/api/v1/accessories/%d", acc.ID), nil)
	m := mustOK(t, resp, raw)
	if int(m["current_stock"].(float64)) != 0 {
		t.Fatalf("stock should still be 0: %v", m["current_stock"])
	}
}

// TestStock_ClientRef_Idempotency — two POST inbound with same client_ref return same flow id.
func TestStock_ClientRef_Idempotency(t *testing.T) {
	h := newRouter(t)
	acc := newAccessory(t, h, "IDM")

	cmd := service.InboundCmd{AccessoryID: acc.ID, Quantity: 2, ClientRef: "shared-ref-1"}
	resp1, raw1 := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound", cmd)
	m1 := mustOK(t, resp1, raw1)

	resp2, raw2 := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound", cmd)
	m2 := mustOK(t, resp2, raw2)

	if m1["id"] != m2["id"] {
		t.Fatalf("idempotency failed: %v vs %v", m1["id"], m2["id"])
	}
	if int(m2["balance_after"].(float64)) != 2 {
		t.Fatalf("balance_after: %v (should still be 2)", m2["balance_after"])
	}
}

// TestFlows_ListByAccessory — query returns only flows for that accessory.
func TestFlows_ListByAccessory(t *testing.T) {
	h := newRouter(t)
	a := newAccessory(t, h, "FLA")
	b := newAccessory(t, h, "FLB")

	httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound", service.InboundCmd{AccessoryID: a.ID, Quantity: 1})
	httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound", service.InboundCmd{AccessoryID: b.ID, Quantity: 2})

	resp, raw := httpDo(t, h, http.MethodGet, fmt.Sprintf("/api/v1/flows?accessory_id=%d", a.ID), nil)
	m := mustOK(t, resp, raw)
	items, _ := m["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 flow for a, got %d: %s", len(items), raw)
	}
	first := items[0].(map[string]any)
	if int(first["accessory_id"].(float64)) != int(a.ID) {
		t.Fatalf("accessory_id mismatch: %v", first["accessory_id"])
	}
}

// TestReplenishment_Scan_ReturnsShortages — verify threshold=0 items excluded.
func TestReplenishment_Scan_ReturnsShortages(t *testing.T) {
	h := newRouter(t)
	newAccessory(t, h, "LOW1") // default threshold 5
	// Manually set up a zero-threshold accessory that should NOT appear.
	body := domain.Accessory{SKU: "ZER0", Name: "不需要补货", LowStockThreshold: 0}
	resp, _ := httpDo(t, h, http.MethodPost, "/api/v1/accessories", body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("zero-thresh create: %d", resp.StatusCode)
	}

	resp, raw := httpDo(t, h, http.MethodGet, "/api/v1/replenishment/scan", nil)
	m := mustOK(t, resp, raw)
	items, _ := m["items"].([]any)
	for _, it := range items {
		row := it.(map[string]any)
		if int(row["threshold"].(float64)) == 0 {
			t.Fatalf("threshold=0 accessory leaked into scan: %s", raw)
		}
	}
}

// TestReplenishment_Check_WithFixedPolicy — verify suggested_quantity = fixed N.
func TestReplenishment_Check_WithFixedPolicy(t *testing.T) {
	h := newRouter(t)
	acc := newAccessory(t, h, "FIX") // threshold 5, stock 0
	_ = acc

	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/replenishment/check", map[string]any{
		"skus":   []string{"FIX"},
		"policy": "fixed:42",
	})
	m := mustOK(t, resp, raw)
	items, _ := m["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %s", raw)
	}
	row := items[0].(map[string]any)
	if int(row["suggested_quantity"].(float64)) != 42 {
		t.Fatalf("expected suggested=42, got %v", row["suggested_quantity"])
	}
	if int(row["shortage"].(float64)) != 5 {
		t.Fatalf("expected shortage=5, got %v", row["shortage"])
	}
}

// TestErrors_HaveUnifiedShape — any error returns { "error": { "code", "message" } }.
func TestErrors_HaveUnifiedShape(t *testing.T) {
	h := newRouter(t)
	// Bad JSON on POST /accessories -> 400 BAD_REQUEST.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accessories", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	mustError(t, resp, raw, http.StatusBadRequest, "BAD_REQUEST")

	// Invalid type filter -> 400 BAD_REQUEST.
	resp, raw = httpDo(t, h, http.MethodGet, "/api/v1/flows?type=bogus", nil)
	mustError(t, resp, raw, http.StatusBadRequest, "BAD_REQUEST")
}

// TestNotFound_RouteReturns404 — unknown accessory id returns 404 NOT_FOUND.
func TestNotFound_RouteReturns404(t *testing.T) {
	h := newRouter(t)
	resp, raw := httpDo(t, h, http.MethodGet, "/api/v1/accessories/99999", nil)
	mustError(t, resp, raw, http.StatusNotFound, "NOT_FOUND")

	// Unknown flow too.
	resp, raw = httpDo(t, h, http.MethodGet, "/api/v1/flows/99999", nil)
	mustError(t, resp, raw, http.StatusNotFound, "NOT_FOUND")
}

// TestMethodNotAllowed_405 — PUT to a POST-only endpoint returns 405.
func TestMethodNotAllowed_405(t *testing.T) {
	h := newRouter(t)
	resp, raw := httpDo(t, h, http.MethodPut, "/api/v1/stock/inbound", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%s", resp.StatusCode, raw)
	}
}

// Helper: pointer-to-string.
func pStr(s string) *string { return &s }

// sanity: ensure context import kept (silences unused imports across edits).
var _ = context.Background

// silence unused import warnings (sql used by reader; we don't actually need
// it here but keep imports for parity with future error-translation tests).
var _ sql.IsolationLevel = sql.LevelDefault
