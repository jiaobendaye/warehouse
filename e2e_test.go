// Package main_test provides black-box integration (e2e) tests for the
// warehouse binary. Tests start the binary as a subprocess in web-only or
// MCP-stdio mode and drive it through its external interfaces (REST HTTP and
// JSON-RPC over stdin/stdout).
//
// Usage:
//
//	go build -o /tmp/warehouse_e2e . && go test -v -run E2E ./...
package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Binary path — built once in TestMain.
const binaryPath = "/tmp/warehouse_e2e_test"

// TestMain builds the production binary once before any test runs.
// Tests themselves check testing.Short() and t.Skip() as appropriate.
func TestMain(m *testing.M) {
	exec.Command("pnpm", "build").Run() // ensure frontend/dist exists for embed
	out, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// startWebApp starts the warehouse binary. Without -tags wails, it auto-starts
// HTTP (REST+frontend) + MCP-stdio. Returns cmd and HTTP base URL.
func startWebApp(t *testing.T, port int) (*exec.Cmd, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	cmd := exec.Command(binaryPath, "--headless", "--db", dbPath,
		"--port", fmt.Sprintf("%d", port),
		"--host", "127.0.0.1")

	// Capture stderr for diagnostics on failure.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start web app: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("subprocess stderr:\n%s", stderr.String())
		}
	})

	// Poll healthz until the server is ready.
	waitForHealth(t, baseURL, 8*time.Second)
	return cmd, baseURL
}

// waitForHealth polls /healthz until it returns 200 or the deadline expires.
func waitForHealth(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				// Quick sanity check on the body.
				var h map[string]any
				if json.Unmarshal(body, &h) == nil && h["status"] == "ok" {
					return
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become healthy within %v", baseURL, timeout)
}

// httpDo is a thin wrapper around http.Client.Do that fails the test on
// transport errors and returns the full response.
func httpDo(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http %s %s: %v", req.Method, req.URL, err)
	}
	return resp
}

// decodeBody decodes the JSON response body into v and closes it.
func decodeBody(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// readBody reads and closes the response body, returning the raw bytes.
func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

// jsonBody returns an io.Reader holding the JSON encoding of v.
func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return bytes.NewReader(b)
}

// getter returns the "error" wrapper or the value directly.
type errorBody struct {
	Err *errInfo `json:"error"`
}

type errInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ── Test 1: web-only full CRUD ──────────────────────────────────────────────

func TestE2E_WebOnly_FullCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	const port = 19999
	_, baseURL := startWebApp(t, port)

	// ── 1. Health check ────────────────────────────────────────────────
	// Already done by startWebApp, but verify it works explicitly.
	func() {
		resp, err := http.Get(baseURL + "/healthz")
		if err != nil {
			t.Fatalf("healthz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("healthz: want 200, got %d", resp.StatusCode)
		}
		var body map[string]string
		decodeBody(t, resp, &body)
		if body["status"] != "ok" {
			t.Fatalf("healthz status: want ok, got %q", body["status"])
		}
	}()

	// ── 2. Create accessory ─────────────────────────────────────────────
	var created struct {
		ID                int64  `json:"id"`
		SKU               string `json:"sku"`
		Name              string `json:"name"`
		CurrentStock      int64  `json:"current_stock"`
		LowStockThreshold int64  `json:"low_stock_threshold"`
		Notes             string `json:"notes"`
		CreatedAt         string `json:"created_at"`
		UpdatedAt         string `json:"updated_at"`
	}
	func() {
		body := map[string]any{
			"sku":                 "E2E-CRUD-001",
			"name":                "测试充电器",
			"low_stock_threshold": 10,
			"notes":               "e2e full CRUD test",
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/accessories", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create: want 201, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		decodeBody(t, resp, &created)
		if created.ID == 0 || created.SKU != "E2E-CRUD-001" || created.CurrentStock != 0 {
			t.Fatalf("create unexpected: %+v", created)
		}
	}()

	// ── 3. Get accessory by id ──────────────────────────────────────────
	var gotAcc struct {
		ID                int64  `json:"id"`
		SKU               string `json:"sku"`
		Name              string `json:"name"`
		CurrentStock      int64  `json:"current_stock"`
		LowStockThreshold int64  `json:"low_stock_threshold"`
	}
	func() {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/accessories/%d", baseURL, created.ID))
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("get: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		decodeBody(t, resp, &gotAcc)
		if gotAcc.ID != created.ID || gotAcc.SKU != "E2E-CRUD-001" {
			t.Fatalf("get mismatch: %+v", gotAcc)
		}
	}()

	// ── 4. Search accessories ───────────────────────────────────────────
	func() {
		resp, err := http.Get(baseURL + "/api/v1/accessories?q=E2E-CRUD-001")
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("search: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var searchRes struct {
			Items []struct {
				ID  int64  `json:"id"`
				SKU string `json:"sku"`
			} `json:"items"`
			Total int `json:"total"`
		}
		decodeBody(t, resp, &searchRes)
		if searchRes.Total < 1 || searchRes.Items[0].ID != created.ID {
			t.Fatalf("search: expected item %d, got %+v", created.ID, searchRes)
		}
	}()

	// ── 5. Update accessory ─────────────────────────────────────────────
	var updatedAcc struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Notes string `json:"notes"`
	}
	func() {
		body := map[string]any{
			"name":  "Updated充电器",
			"notes": "updated notes",
		}
		req, _ := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/accessories/%d", baseURL, created.ID), jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("update: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		decodeBody(t, resp, &updatedAcc)
		if updatedAcc.Name != "Updated充电器" || updatedAcc.Notes != "updated notes" {
			t.Fatalf("update mismatch: %+v", updatedAcc)
		}
	}()

	// ── 6. Inbound ──────────────────────────────────────────────────────
	var inboundFlow struct {
		ID           int64 `json:"id"`
		AccessoryID  int64 `json:"accessory_id"`
		Type         string `json:"type"`
		Quantity     int64 `json:"quantity"`
		BalanceAfter int64 `json:"balance_after"`
	}
	func() {
		body := map[string]any{
			"accessory_id": created.ID,
			"quantity":     5,
			"unit_cost":    10.0,
			"remark":       "e2e inbound",
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/stock/inbound", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("inbound: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		decodeBody(t, resp, &inboundFlow)
		if inboundFlow.Type != "in" || inboundFlow.Quantity != 5 || inboundFlow.BalanceAfter != 5 {
			t.Fatalf("inbound unexpected: %+v", inboundFlow)
		}
	}()

	// ── 7. Outbound ─────────────────────────────────────────────────────
	var outboundFlow struct {
		ID           int64  `json:"id"`
		Type         string `json:"type"`
		Quantity     int64  `json:"quantity"`
		BalanceAfter int64  `json:"balance_after"`
	}
	func() {
		body := map[string]any{
			"accessory_id": created.ID,
			"quantity":     2,
			"unit_price":   25.0,
			"remark":       "e2e outbound",
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/stock/outbound", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("outbound: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		decodeBody(t, resp, &outboundFlow)
		if outboundFlow.Type != "out" || outboundFlow.Quantity != 2 || outboundFlow.BalanceAfter != 3 {
			t.Fatalf("outbound unexpected: %+v", outboundFlow)
		}
	}()

	// ── 8. List flows ───────────────────────────────────────────────────
	func() {
		url := fmt.Sprintf("%s/api/v1/flows?accessory_id=%d", baseURL, created.ID)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("list flows: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("list flows: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var flowList struct {
			Items []struct {
				ID           int64  `json:"id"`
				AccessoryID  int64  `json:"accessory_id"`
				Type         string `json:"type"`
				Quantity     int64  `json:"quantity"`
				BalanceAfter int64  `json:"balance_after"`
			} `json:"items"`
			Total int `json:"total"`
		}
		decodeBody(t, resp, &flowList)
		if flowList.Total < 2 {
			t.Fatalf("flows: expected at least 2, got %d", flowList.Total)
		}
		// Check both flows are there by scanning for our flow IDs.
		foundIn := false
		foundOut := false
		for _, f := range flowList.Items {
			if f.ID == inboundFlow.ID {
				foundIn = true
			}
			if f.ID == outboundFlow.ID {
				foundOut = true
			}
		}
		if !foundIn || !foundOut {
			t.Fatalf("flows: missing flow records (have %d items, inbound=%d outbound=%d)",
				len(flowList.Items), inboundFlow.ID, outboundFlow.ID)
		}
	}()

	// ── 9. Replenishment scan ────────────────────────────────────────────
	func() {
		resp, err := http.Get(baseURL + "/api/v1/replenishment/scan")
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("scan: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var scanRes struct {
			Items []struct {
				AccessoryID      int64  `json:"accessory_id"`
				SKU              string `json:"sku"`
				CurrentStock     int64  `json:"current_stock"`
				Threshold        int64  `json:"threshold"`
				Shortage         int64  `json:"shortage"`
				SuggestedQuantity int64 `json:"suggested_quantity"`
			} `json:"items"`
		}
		decodeBody(t, resp, &scanRes)
		// With threshold=10 and stock=3 (after outbound), our accessory
		// should be in shortage (shortage=7).
		found := false
		for _, it := range scanRes.Items {
			if it.AccessoryID == created.ID {
				found = true
				if it.Shortage != 7 {
					t.Fatalf("scan shortage: want 7, got %d", it.Shortage)
				}
				break
			}
		}
		if !found {
			t.Fatalf("scan: accessory %d not in shortage list: %+v", created.ID, scanRes.Items)
		}
	}()

	// ── 10. Replenishment check ─────────────────────────────────────────
	func() {
		body := map[string]any{
			"skus":   []string{"E2E-CRUD-001", "NONEXISTENT-SKU"},
			"policy": "default",
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/replenishment/check", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("check: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var checkRes struct {
			Items    []map[string]any `json:"items"`
			NotFound []string         `json:"not_found"`
		}
		decodeBody(t, resp, &checkRes)
		if len(checkRes.Items) != 1 {
			t.Fatalf("check items: expected 1, got %d: %+v", len(checkRes.Items), checkRes)
		}
		sku, _ := checkRes.Items[0]["sku"].(string)
		if sku != "E2E-CRUD-001" {
			t.Fatalf("check item sku: want E2E-CRUD-001, got %q", sku)
		}
		if len(checkRes.NotFound) != 1 || checkRes.NotFound[0] != "NONEXISTENT-SKU" {
			t.Fatalf("check not_found: expected [NONEXISTENT-SKU], got %v", checkRes.NotFound)
		}
	}()

	// ── 11. Delete accessory ────────────────────────────────────────────
	// Create a fresh accessory with no flows, delete it.
	var deleteID int64
	func() {
		body := map[string]any{
			"sku":  "E2E-DEL-ONLY",
			"name": "delete-only",
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/accessories", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create delete-target: want 201, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var fresh struct {
			ID int64 `json:"id"`
		}
		decodeBody(t, resp, &fresh)
		deleteID = fresh.ID
	}()
	func() {
		req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/accessories/%d", baseURL, deleteID), nil)
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("delete: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var delRes struct {
			Deleted int64 `json:"deleted"`
		}
		decodeBody(t, resp, &delRes)
		if delRes.Deleted != deleteID {
			t.Fatalf("delete response: want deleted=%d, got %d", deleteID, delRes.Deleted)
		}
		// Verify it's gone.
		getResp, err := http.Get(fmt.Sprintf("%s/api/v1/accessories/%d", baseURL, deleteID))
		if err != nil {
			t.Fatalf("get deleted: %v", err)
		}
		defer getResp.Body.Close()
		if getResp.StatusCode != 404 {
			t.Fatalf("get deleted: want 404, got %d", getResp.StatusCode)
		}
	}()
}

// ── Test 2: error codes ──────────────────────────────────────────────────────

func TestE2E_WebOnly_ErrorCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	const port = 19998
	_, baseURL := startWebApp(t, port)

	// ── Create an accessory for error-path tests ─────────────────────────
	var accID int64
	accSKU := "E2E-ERR-001"
	func() {
		body := map[string]any{
			"sku":  accSKU,
			"name": "error-test",
			"low_stock_threshold": 5,
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/accessories", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("setup create: want 201, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var out struct {
			ID int64 `json:"id"`
		}
		decodeBody(t, resp, &out)
		accID = out.ID
	}()

	// ── Duplicate SKU → 409 CONFLICT ────────────────────────────────────
	t.Run("duplicate_sku", func(t *testing.T) {
		body := map[string]any{
			"sku":  accSKU,
			"name": "duplicate",
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/accessories", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 409 {
			t.Fatalf("duplicate sku: want 409, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var eb errorBody
		decodeBody(t, resp, &eb)
		if eb.Err == nil || eb.Err.Code != "CONFLICT" {
			t.Fatalf("duplicate sku: want code=CONFLICT, got %+v", eb)
		}
	})

	// ── Get missing accessory → 404 NOT_FOUND ───────────────────────────
	t.Run("not_found", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/accessories/999999")
		if err != nil {
			t.Fatalf("get missing: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("get missing: want 404, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var eb errorBody
		decodeBody(t, resp, &eb)
		if eb.Err == nil || eb.Err.Code != "NOT_FOUND" {
			t.Fatalf("get missing: want code=NOT_FOUND, got %+v", eb)
		}
	})

	// ── Outbound insufficient stock → 409 INSUFFICIENT_STOCK ────────────
	t.Run("insufficient_stock", func(t *testing.T) {
		body := map[string]any{
			"accessory_id": accID,
			"quantity":     999, // more than available (stock is 0)
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/stock/outbound", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 409 {
			t.Fatalf("insufficient stock: want 409, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var eb errorBody
		decodeBody(t, resp, &eb)
		if eb.Err == nil || eb.Err.Code != "INSUFFICIENT_STOCK" {
			t.Fatalf("insufficient stock: want code=INSUFFICIENT_STOCK, got %+v", eb)
		}
	})

	// ── Invalid input → 400 BAD_REQUEST ─────────────────────────────────
	t.Run("invalid_input", func(t *testing.T) {
		body := map[string]any{
			// Missing required "sku", "name"
			"notes": "incomplete",
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/accessories", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("invalid input: want 400, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var eb errorBody
		decodeBody(t, resp, &eb)
		if eb.Err == nil || eb.Err.Code != "BAD_REQUEST" {
			t.Fatalf("invalid input: want code=BAD_REQUEST, got %+v", eb)
		}
	})
}

// ── Test 3: MCP via HTTP endpoint ─────────────────────────────────────────

func TestE2E_MCPViaHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	_, baseURL := startWebApp(t, 19996)
	mcpURL := baseURL + "/mcp"

	// MCP via HTTP: POST JSON-RPC request to /mcp.
	// The streamable HTTP transport requires both Accept headers.
	callMCP := func(body map[string]any) *http.Response {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", mcpURL, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		return httpDo(t, req)
	}

	// tools/list
	resp := callMCP(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{},
	})
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("MCP /mcp tools/list response: %s", string(raw)[:min(len(raw), 200)])
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("MCP /mcp returned %d: %s", resp.StatusCode, string(raw))
	}

	// accessory.create via MCP
	resp2 := callMCP(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name": "accessory.create",
			"arguments": map[string]any{
			},
		},
	})
	raw2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("MCP accessory.create returned %d: %s", resp2.StatusCode, string(raw2))
	}
	t.Logf("MCP via HTTP: roundtrip OK")
}

func min(a, b int) int { if a < b { return a }; return b }


func TestE2E_ClientRef_Idempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	const port = 19997
	_, baseURL := startWebApp(t, port)

	// Create an accessory.
	var accID int64
	func() {
		body := map[string]any{
			"sku":  "E2E-IDM-001",
			"name": "idempotency-test",
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/accessories", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("idm create: want 201, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var out struct {
			ID int64 `json:"id"`
		}
		decodeBody(t, resp, &out)
		accID = out.ID
	}()

	clientRef := "e2e-test-1"

	// First inbound.
	var firstFlowID int64
	func() {
		body := map[string]any{
			"accessory_id": accID,
			"quantity":     10,
			"unit_cost":    5.0,
			"client_ref":   clientRef,
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/stock/inbound", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("first inbound: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var flow struct {
			ID           int64  `json:"id"`
			AccessoryID  int64  `json:"accessory_id"`
			ClientRef    string `json:"client_ref"`
			BalanceAfter int64  `json:"balance_after"`
		}
		decodeBody(t, resp, &flow)
		firstFlowID = flow.ID
		if flow.ClientRef != clientRef {
			t.Fatalf("first inbound client_ref: want %q, got %q", clientRef, flow.ClientRef)
		}
		if flow.BalanceAfter != 10 {
			t.Fatalf("first inbound balance: want 10, got %d", flow.BalanceAfter)
		}
	}()

	// Second inbound with same client_ref → same flow, stock unchanged.
	func() {
		body := map[string]any{
			"accessory_id": accID,
			"quantity":     10,
			"unit_cost":    5.0,
			"client_ref":   clientRef,
		}
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/stock/inbound", jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		resp := httpDo(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("second inbound: want 200, got %d: %s", resp.StatusCode, readBody(t, resp))
		}
		var flow struct {
			ID           int64  `json:"id"`
			ClientRef    string `json:"client_ref"`
			BalanceAfter int64  `json:"balance_after"`
		}
		decodeBody(t, resp, &flow)
		if flow.ID != firstFlowID {
			t.Fatalf("idempotency: second flow id %d != first %d", flow.ID, firstFlowID)
		}
		if flow.BalanceAfter != 10 {
			t.Fatalf("idempotency: balance should still be 10, got %d", flow.BalanceAfter)
		}
	}()

	// Verify current_stock on the accessory is still 10 (not 20).
	func() {
		resp, err := http.Get(fmt.Sprintf("%s/api/v1/accessories/%d", baseURL, accID))
		if err != nil {
			t.Fatalf("get after idempotency: %v", err)
		}
		defer resp.Body.Close()
		var acc struct {
			CurrentStock int64 `json:"current_stock"`
		}
		decodeBody(t, resp, &acc)
		if acc.CurrentStock != 10 {
			t.Fatalf("idempotency: stock should be 10, got %d", acc.CurrentStock)
		}
	}()
}
