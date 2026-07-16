package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

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
	accSvc := service.NewAccessoryService(d, accRepo, flowRepo)
	stockSvc := service.NewStockService(accRepo, flowRepo, d)
	flowSvc := service.NewFlowService(flowRepo)
	replSvc := service.NewReplenishmentService(accRepo)

	svcs := api.Services{
		Accessory:     accSvc,
		Stock:         stockSvc,
		Flow:          flowSvc,
		Replenishment: replSvc,
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

func newAccessory(t *testing.T, h http.Handler, name string) domain.Accessory {
	t.Helper()
	body := domain.Accessory{
		Name:              name,
		LowStockThreshold: 5,
	}
	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/accessories", body)
	m := mustOK(t, resp, raw)
	idF, _ := m["id"].(float64)
	body.ID = int64(idF)
	return body
}

// TestAccessories_FullCRUD — happy path: Create → Get → Update → List → Delete.
func TestAccessories_FullCRUD(t *testing.T) {
	h := newRouter(t)

	// Create
	body := domain.Accessory{
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
	if name, _ := m["name"].(string); name != "保护壳" {
		t.Fatalf("name mismatch: %v", m["name"])
	}

	// Get
	resp, raw = httpDo(t, h, http.MethodGet, fmt.Sprintf("/api/v1/accessories/%d", id), nil)
	m = mustOK(t, resp, raw)
	if m["name"] != "保护壳" {
		t.Fatalf("get name: %v", m["name"])
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

// TestAccessories_DuplicateName_409 — second create with same name returns 409 CONFLICT.
func TestAccessories_DuplicateName_409(t *testing.T) {
	h := newRouter(t)
	body := domain.Accessory{Name: "DUPE"}
	resp, _ := httpDo(t, h, http.MethodPost, "/api/v1/accessories", body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("first create failed: %d", resp.StatusCode)
	}
	resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/accessories", body)
	mustError(t, resp, raw, http.StatusConflict, "CONFLICT")
}

// TestAccessories_DeleteCascadesFlows — create + inbound + delete returns 200
// with flows_deleted=1 in the body; the accessory and the flow are both gone.
func TestAccessories_DeleteCascadesFlows(t *testing.T) {
	h := newRouter(t)
	acc := newAccessory(t, h, "HAF")

	inbound := service.InboundCmd{AccessoryID: acc.ID, Quantity: 2}
	resp, _ := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound", inbound)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("inbound status=%d", resp.StatusCode)
	}

	resp, raw := httpDo(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/accessories/%d", acc.ID), nil)
	m := mustOK(t, resp, raw)
	if int(m["deleted"].(float64)) != int(acc.ID) {
		t.Fatalf("deleted = %v, want %d", m["deleted"], acc.ID)
	}
	if int(m["flows_deleted"].(float64)) != 1 {
		t.Fatalf("flows_deleted = %v, want 1", m["flows_deleted"])
	}

	// Accessory gone.
	resp, raw = httpDo(t, h, http.MethodGet, fmt.Sprintf("/api/v1/accessories/%d", acc.ID), nil)
	mustError(t, resp, raw, http.StatusNotFound, "NOT_FOUND")

	// Flow ledger for that accessory is empty.
	resp, raw = httpDo(t, h, http.MethodGet, fmt.Sprintf("/api/v1/flows?accessory_id=%d", acc.ID), nil)
	m = mustOK(t, resp, raw)
	if int(m["total"].(float64)) != 0 {
		t.Fatalf("flows total = %v, want 0", m["total"])
	}
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
	body := domain.Accessory{Name: "不需要补货", LowStockThreshold: 0}
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
		"names":  []string{"FIX"},
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
// --- File inbound end-to-end --------------------------------------------

func TestStockFileInbound_HappyPath_CreatesAndStocks(t *testing.T) {
	h := newRouter(t)

	// Pre-seed one existing accessory so we exercise both branches.
	resp, _ := httpDo(t, h, http.MethodPost, "/api/v1/accessories", map[string]any{
		"name": "FI-EXISTS",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed create: status=%d", resp.StatusCode)
	}
	// And a baseline inbound so we can verify the stock adds on top.
	// Get its id by listing.
	listResp, listRaw := httpDo(t, h, http.MethodGet, "/api/v1/accessories?q=FI-EXISTS", nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list: status=%d body=%s", listResp.StatusCode, listRaw)
	}
	var listed struct {
		Items []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Stock int64  `json:"current_stock"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listRaw, &listed); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("want 1 listed, got %d", len(listed.Items))
	}
	existingID := listed.Items[0].ID
	_ = existingID // referenced indirectly via stock assertion below

	// Build xlsx with one existing + two new rows; the existing name
	// has a trailing space to exercise trim semantics.
	xf := excelize.NewFile()
	defer xf.Close()
	putXlsxRow(t, xf, 0, "配件", "数量")
	putXlsxRow(t, xf, 1, "FI-EXISTS ", "3")
	putXlsxRow(t, xf, 2, "FI-NEW-1", "7")
	putXlsxRow(t, xf, 3, "FI-NEW-2", "5")
	var xbuf bytes.Buffer
	if err := xf.Write(&xbuf); err != nil {
		t.Fatalf("xlsx write: %v", err)
	}

	resp, raw := postMultipartFile(t, h, "/api/v1/stock/file_inbound", "file", "入库.xlsx", &xbuf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	var got api.FileInboundResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, raw)
	}
	if got.Inbound != 3 || got.Created != 2 {
		t.Fatalf("result = %+v, want inbound=3 created=2", got)
	}
	// Re-list to confirm stock numbers.
	listed.Items = nil
	listResp, listRaw = httpDo(t, h, http.MethodGet, "/api/v1/accessories?q=FI-", nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("re-list: status=%d", listResp.StatusCode)
	}
	if err := json.Unmarshal(listRaw, &listed); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	want := map[string]int64{
		"FI-EXISTS": 3, // 0 baseline + 3 inbound
		"FI-NEW-1":  7,
		"FI-NEW-2":  5,
	}
	for _, it := range listed.Items {
		if w, ok := want[it.Name]; ok {
			if it.Stock != w {
				t.Errorf("%s stock = %d, want %d", it.Name, it.Stock, w)
			}
			delete(want, it.Name)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing rows in catalog after inbound: %+v", want)
	}
}

func TestStockFileInbound_RejectsMissingFile(t *testing.T) {
	h := newRouter(t)
	// Send a multipart with the wrong field name.
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, _ := mw.CreateFormFile("attachment", "x.xlsx")
	_, _ = fw.Write([]byte("not an xlsx"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stock/file_inbound", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// --- Accessory xlsx export ----------------------------------------------

// TestAccessoriesExport_RoundTrip seeds two accessories, hits the export
// endpoint, and verifies the returned xlsx re-opens with the same rows.
// This guards both the HTTP plumbing (status, headers, content-type) and
// the build function (cell layout) in one assertion path.
func TestAccessoriesExport_RoundTrip(t *testing.T) {
	h := newRouter(t)

	// Seed: two accessories, with distinct stock + threshold so we can
	// tell them apart in the export.
	a := newAccessory(t, h, "导出-A")
	if resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound",
		service.InboundCmd{AccessoryID: a.ID, Quantity: 4}); resp.StatusCode/100 != 2 {
		t.Fatalf("inbound A: %d %s", resp.StatusCode, raw)
	}
	if resp, raw := httpDo(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/accessories/%d", a.ID),
		domain.AccessoryUpdate{Notes: pStr("A-remark")}); resp.StatusCode/100 != 2 {
		t.Fatalf("patch A notes: %d %s", resp.StatusCode, raw)
	}
	b := newAccessory(t, h, "导出-B")
	if resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound",
		service.InboundCmd{AccessoryID: b.ID, Quantity: 11}); resp.StatusCode/100 != 2 {
		t.Fatalf("inbound B: %d %s", resp.StatusCode, raw)
	}

	// Hit export. We use httpDo with a nil body so Content-Type is not set
	// — the endpoint is GET, no body needed.
	resp, raw := httpDo(t, h, http.MethodGet, "/api/v1/accessories/export", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, raw)
	}

	// Headers: content-type must be the xlsx mime, content-disposition
	// must carry attachment + a sensible filename.
	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("Content-Type = %q, want xlsx mime", ct)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, ".xlsx") {
		t.Errorf("Content-Disposition = %q, want attachment with .xlsx filename", cd)
	}
	// The body must round-trip as a real xlsx, not an empty file.
	xf, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open xlsx: %v (body=%d bytes)", err, len(raw))
	}
	defer xf.Close()

	sheet := "配件库存"
	rows, err := xf.GetRows(sheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	// 1 header + 2 data rows.
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2); sheet=%+v", len(rows), rows)
	}
	// Header sanity.
	wantHeaders := []string{"档口", "名称", "当前库存", "低库存阈值", "备注", "创建时间", "更新时间"}
	for i, want := range wantHeaders {
		if i >= len(rows[0]) || rows[0][i] != want {
			t.Errorf("header[%d] = %q, want %q", i, safeCell(rows, 0, i), want)
		}
	}

	// Find each seeded accessory by name. The export sorts by stall ASC
	// then name ASC; both seeded rows have stall=未分配 so the secondary
	// key sorts 导出-A before 导出-B. Look up by name regardless of position.
	got := map[string]map[string]string{}
	for _, row := range rows[1:] {
		if len(row) == 0 {
			continue
		}
		name := safeCellFromRow(row, 1)
		got[name] = map[string]string{
			"stall":     safeCellFromRow(row, 0),
			"stock":     safeCellFromRow(row, 2),
			"threshold": safeCellFromRow(row, 3),
			"notes":     safeCellFromRow(row, 4),
		}
	}
	if g, ok := got["导出-A"]; !ok {
		t.Errorf("导出-A missing from export")
	} else {
		if g["stall"] != "未分配" {
			t.Errorf("导出-A stall = %q, want 未分配 (default)", g["stall"])
		}
		if g["stock"] != "4" {
			t.Errorf("导出-A stock = %q, want 4", g["stock"])
		}
		if g["threshold"] != "5" {
			t.Errorf("导出-A threshold = %q, want 5 (newAccessory default)", g["threshold"])
		}
		if g["notes"] != "A-remark" {
			t.Errorf("导出-A notes = %q, want A-remark", g["notes"])
		}
	}
	if g, ok := got["导出-B"]; !ok {
		t.Errorf("导出-B missing from export")
	} else if g["stock"] != "11" {
		t.Errorf("导出-B stock = %q, want 11", g["stock"])
	}
}

// TestAccessoriesExport_Empty — exporting an empty catalog must still
// succeed and produce a valid xlsx with just the header row.
func TestAccessoriesExport_Empty(t *testing.T) {
	h := newRouter(t)

	resp, raw := httpDo(t, h, http.MethodGet, "/api/v1/accessories/export", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, raw)
	}
	xf, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	defer xf.Close()
	rows, err := xf.GetRows("配件库存")
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (header only); got %+v", len(rows), rows)
	}
	if len(rows[0]) != 7 {
		t.Errorf("header column count = %d, want 7; got %+v", len(rows[0]), rows[0])
	}
}

// --- Replenishment xlsx export ------------------------------------------

// TestReplenishmentExport_RoundTrip seeds two accessories that are both
// in shortage (Scan filters out non-short rows), with different severity
// so the sort-by-shortage-DESC ordering is observable. Hits the export
// endpoint and verifies the xlsx round-trips with the correct columns.
func TestReplenishmentExport_RoundTrip(t *testing.T) {
	h := newRouter(t)

	// 导出-A: stock=1, threshold=5 (default) → shortage=4 (most urgent).
	a := newAccessory(t, h, "导出-A")
	if resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound",
		service.InboundCmd{AccessoryID: a.ID, Quantity: 1}); resp.StatusCode/100 != 2 {
		t.Fatalf("inbound A: %d %s", resp.StatusCode, raw)
	}
	// 导出-B: stock=2, threshold=5 → shortage=3 (less urgent, must sort after A).
	b := newAccessory(t, h, "导出-B")
	if resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/stock/inbound",
		service.InboundCmd{AccessoryID: b.ID, Quantity: 2}); resp.StatusCode/100 != 2 {
		t.Fatalf("inbound B: %d %s", resp.StatusCode, raw)
	}

	resp, raw := httpDo(t, h, http.MethodGet, "/api/v1/replenishment/export", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, raw)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("Content-Type = %q, want xlsx mime", ct)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, ".xlsx") {
		t.Errorf("Content-Disposition = %q, want attachment with .xlsx filename", cd)
	}

	xf, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open xlsx: %v (body=%d bytes)", err, len(raw))
	}
	defer xf.Close()

	sheet := "告急补货"
	rows, err := xf.GetRows(sheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	// 1 header + 2 data rows.
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2); sheet=%+v", len(rows), rows)
	}

	wantHeaders := []string{"档口", "名称", "当前库存", "阈值", "缺货量", "建议补货"}
	for i, want := range wantHeaders {
		if i >= len(rows[0]) || rows[0][i] != want {
			t.Errorf("header[%d] = %q, want %q", i, safeCell(rows, 0, i), want)
		}
	}

	// Sort by stall ASC then shortage DESC. Both test rows have stall=未分配
	// (default), so the secondary key decides: A (shortage=4) must come before
	// B (shortage=3). The stall column lives at index 0; name at index 1.
	if rows[1][1] != "导出-A" {
		t.Errorf("row[1] name = %q, want 导出-A (highest shortage first)", rows[1][1])
	}
	if rows[1][2] != "1" {
		t.Errorf("导出-A stock = %q, want 1", rows[1][2])
	}
	if rows[1][4] != "4" {
		t.Errorf("导出-A shortage = %q, want 4", rows[1][4])
	}
	if rows[2][1] != "导出-B" {
		t.Errorf("row[2] name = %q, want 导出-B", rows[2][1])
	}
	if rows[2][2] != "2" {
		t.Errorf("导出-B stock = %q, want 2", rows[2][2])
	}
	if rows[2][4] != "3" {
		t.Errorf("导出-B shortage = %q, want 3", rows[2][4])
	}
}

// TestReplenishmentExport_Empty — exporting when no accessories exist
// must still produce a valid xlsx with just the header row.
func TestReplenishmentExport_Empty(t *testing.T) {
	h := newRouter(t)

	resp, raw := httpDo(t, h, http.MethodGet, "/api/v1/replenishment/export", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, raw)
	}
	xf, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	defer xf.Close()
	rows, err := xf.GetRows("告急补货")
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (header only); got %+v", len(rows), rows)
	}
	if len(rows[0]) != 6 {
		t.Errorf("header column count = %d, want 6; got %+v", len(rows[0]), rows[0])
	}
}

// safeCell returns rows[r][c] or "<missing>" when the cell is absent —
// keeps failure messages readable when the sheet is malformed.
func safeCell(rows [][]string, r, c int) string {
	if r >= len(rows) {
		return "<missing row>"
	}
	return safeCellFromRow(rows[r], c)
}

func safeCellFromRow(row []string, c int) string {
	if c >= len(row) {
		return ""
	}
	return row[c]
}

// --- helpers -------------------------------------------------------------

func putXlsxRow(t *testing.T, xf *excelize.File, r int, cells ...string) {
	t.Helper()
	for c, v := range cells {
		cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
		if err := xf.SetCellValue("Sheet1", cell, v); err != nil {
			t.Fatalf("set cell %s: %v", cell, err)
		}
	}
}

func postMultipartFile(t *testing.T, h http.Handler, path, field, filename string, content io.Reader) (*http.Response, []byte) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.Copy(fw, content); err != nil {
		t.Fatalf("copy content: %v", err)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// TestAccessories_StallsEndpoint — verifies /api/v1/accessories/stalls
// returns the distinct stalls in use and that ?stall= filters the list.
func TestAccessories_StallsEndpoint(t *testing.T) {
	h := newRouter(t)

	// Seed three accessories with two distinct stalls. Use the PATCH
	// endpoint to set the stall on the create-without-stall default.
	post := func(name, stall string) domain.Accessory {
		t.Helper()
		resp, raw := httpDo(t, h, http.MethodPost, "/api/v1/accessories",
			domain.Accessory{Name: name, LowStockThreshold: 5})
		m := mustOK(t, resp, raw)
		idF, _ := m["id"].(float64)
		if stall != "" {
			stallPtr := stall
			resp, raw = httpDo(t, h, http.MethodPatch,
				fmt.Sprintf("/api/v1/accessories/%d", int64(idF)),
				domain.AccessoryUpdate{Stall: &stallPtr})
			mustOK(t, resp, raw)
		}
		return domain.Accessory{ID: int64(idF), Name: name, Stall: stall}
	}
	_ = post("JY-支架-A", "JY")
	_ = post("JY-支架-B", "JY")
	_ = post("优博-推拉", "优博")
	_ = post("无档口-X", "")

	// 1. /accessories/stalls returns the two named stalls plus "未分配".
	resp, raw := httpDo(t, h, http.MethodGet, "/api/v1/accessories/stalls", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	m := mustOK(t, resp, raw)
	rawList, _ := m["stalls"].([]any)
	gotStalls := make([]string, 0, len(rawList))
	for _, s := range rawList {
		str, _ := s.(string)
		gotStalls = append(gotStalls, str)
	}
	want := []string{"JY", "优博", "未分配"}
	if !reflect.DeepEqual(gotStalls, want) {
		t.Errorf("stalls = %v, want %v", gotStalls, want)
	}

	// 2. ?stall=JY returns exactly the two JY items.
	resp, raw = httpDo(t, h, http.MethodGet, "/api/v1/accessories?stall=JY", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	m = mustOK(t, resp, raw)
	items, _ := m["items"].([]any)
	if len(items) != 2 {
		t.Errorf("filtered items len = %d, want 2", len(items))
	}
	for _, raw := range items {
		obj, _ := raw.(map[string]any)
		if obj["stall"] != "JY" {
			t.Errorf("filtered item has stall %v, want JY", obj["stall"])
		}
	}
}
