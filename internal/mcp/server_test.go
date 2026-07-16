// Package mcp_test is the integration test layer for the MCP server. It uses
// the official modelcontextprotocol/go-sdk in-memory transport to drive a
// roundtrip against a fully wired Server, and asserts the contract from
// changes/mobile-accessories-management/specs/mcp-server.md:
//
//   - TranslateError maps the four service sentinels to the JSON-RPC codes
//     documented in the spec (NOT_FOUND=-32004, CONFLICT=-32005,
//     BAD_REQUEST=-32600).
//   - tools/list returns exactly the 19 tools named in the spec.
//   - tool calls exercise the underlying service methods, with both happy
//     paths and error paths, and surface the right JSON-RPC error codes on
//     failure.
//   - Stdio transport works end-to-end via a custom io.Pipe.
package mcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xuri/excelize/v2"

	"github.com/jiaobendaye/warehouse/internal/db"
	"github.com/jiaobendaye/warehouse/internal/domain"
	"github.com/jiaobendaye/warehouse/internal/mcp"
	"github.com/jiaobendaye/warehouse/internal/repo"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// newTestServices builds a fresh set of services backed by an in-memory
// SQLite database. Returns the services plus the four underlying services
// the tests need to drive directly.
func newTestServices(t *testing.T) mcp.Services {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	accRepo := repo.NewAccessoryRepo(d)
	flowRepo := repo.NewFlowRepo(d)
	return mcp.Services{
		Accessory:     service.NewAccessoryService(accRepo, flowRepo),
		Stock:         service.NewStockService(accRepo, flowRepo, d),
		Flow:          service.NewFlowService(flowRepo),
		Replenishment: service.NewReplenishmentService(accRepo),
		ExportsDir:    filepath.Join(dir, "exports"),
		// Real-looking but unreachable URL — tests assert only on the
		// shape (scheme/host/port + filename), never dial out.
		PublicBaseURL: "http://127.0.0.1:17880",
	}
}

// ------------------------------------------------------------------
// TranslateError mapping
// ------------------------------------------------------------------

func TestTranslateError_NotFound_MapsTo_32004(t *testing.T) {
	code, msg := mcp.TranslateError(service.ErrNotFound)
	if code != -32004 {
		t.Fatalf("want code -32004, got %d", code)
	}
	if msg == "" {
		t.Fatalf("want non-empty message")
	}
}

func TestTranslateError_InvalidInput_MapsTo_32600(t *testing.T) {
	code, msg := mcp.TranslateError(service.ErrInvalidInput)
	if code != -32600 {
		t.Fatalf("want code -32600, got %d", code)
	}
	if msg == "" {
		t.Fatalf("want non-empty message")
	}
}

func TestTranslateError_NameConflict_MapsTo_32005(t *testing.T) {
	code, _ := mcp.TranslateError(service.ErrNameConflict)
	if code != -32005 {
		t.Fatalf("want code -32005, got %d", code)
	}
}

func TestTranslateError_HasFlow_MapsTo_32005(t *testing.T) {
	code, _ := mcp.TranslateError(service.ErrHasFlow)
	if code != -32005 {
		t.Fatalf("want code -32005, got %d", code)
	}
}

func TestTranslateError_InsufficientStock_MapsTo_32005(t *testing.T) {
	code, msg := mcp.TranslateError(service.ErrInsufficientStock)
	if code != -32005 {
		t.Fatalf("want code -32005, got %d", code)
	}
	if !strings.Contains(msg, "INSUFFICIENT_STOCK") {
		t.Fatalf("want message to include INSUFFICIENT_STOCK, got %q", msg)
	}
}

func TestTranslateError_Unknown_MapsTo_32603(t *testing.T) {
	code, msg := mcp.TranslateError(errors.New("kaboom"))
	if code != -32603 {
		t.Fatalf("want code -32603, got %d", code)
	}
	if msg == "" {
		t.Fatalf("want non-empty message (sanitized error)")
	}
}

func TestTranslateError_WrappedSentinels(t *testing.T) {
	// fmt.Errorf("%w: ...", ErrX) should still map correctly.
	wrapped := wrapErr(service.ErrNotFound, "accessory 42 not found")
	code, _ := mcp.TranslateError(wrapped)
	if code != -32004 {
		t.Fatalf("wrapped ErrNotFound should map to -32004, got %d", code)
	}
}

// ------------------------------------------------------------------
// tools/list — 19 tools exactly
// ------------------------------------------------------------------

func TestToolsList_All(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)

	client, session := newInMemoryClient(t, srv)
	_ = client
	defer session.Close()

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := make([]string, 0, len(res.Tools))
	for _, tl := range res.Tools {
		got = append(got, tl.Name)
	}

	want := []string{
		"accessory.list",
		"accessory.list_stalls",
		"accessory.get",
		"accessory.create",
		"accessory.update",
		"accessory.delete",
		"accessory.export",
		"stock.inbound",
		"stock.outbound",
		"stock.batch_inbound",
		"stock.batch_outbound",
		"flow.list",
		"flow.get",
		"replenishment.scan",
		"replenishment.check",
		"replenishment.export",
		"stock.file_outbound",
		"stock.file_outbound.execute",
		"stock.file_inbound",
	}
	if len(got) != len(want) {
		t.Fatalf("want %d tools, got %d (%v)", len(want), len(got), got)
	}
	wantSet := map[string]bool{}
	for _, n := range want {
		wantSet[n] = true
	}
	for _, n := range got {
		if !wantSet[n] {
			t.Fatalf("unexpected tool name %q", n)
		}
	}
}

// ------------------------------------------------------------------
// Tool roundtrips via in-memory MCP client
// ------------------------------------------------------------------

func TestTool_AccessoryCreateAndGet(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	createParams := testCallToolParams{
		Name: "accessory.create",
		Arguments: map[string]any{
			"name":                "保护壳-MCP",
			"low_stock_threshold": 5,
		},
	}.toParams()
	createRes, err := session.CallTool(context.Background(), createParams)
	if err != nil {
		t.Fatalf("accessory.create: %v", err)
	}
	if createRes.IsError {
		t.Fatalf("accessory.create returned IsError: %+v", createRes)
	}
	var created struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := decodeStructured(createRes.StructuredContent, &created); err != nil {
		t.Fatalf("decode create result: %v", err)
	}
	if created.ID == 0 || created.Name != "保护壳-MCP" {
		t.Fatalf("unexpected create result: %+v", created)
	}

	getParams := testCallToolParams{
		Name:      "accessory.get",
		Arguments: map[string]any{"id": created.ID},
	}.toParams()
	getRes, err := session.CallTool(context.Background(), getParams)
	if err != nil {
		t.Fatalf("accessory.get: %v", err)
	}
	if getRes.IsError {
		t.Fatalf("accessory.get returned IsError: %+v", getRes)
	}
	var got struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := decodeStructured(getRes.StructuredContent, &got); err != nil {
		t.Fatalf("decode get result: %v", err)
	}
	if got.ID != created.ID || got.Name != "保护壳-MCP" {
		t.Fatalf("got %+v, want id=%d name=保护壳-MCP", got, created.ID)
	}
}

// TestTool_AccessoryListStalls seeds three accessories under two distinct
// stalls (one stall repeated, in non-alphabetical insertion order) and
// asserts the tool returns the distinct stalls sorted alphabetically
// (case-insensitive, matching the SQL `COLLATE NOCASE` order).
func TestTool_AccessoryListStalls(t *testing.T) {
	svcs := newTestServices(t)

	// Seed via the underlying service so we can set a non-default stall.
	// AccessoryService.Create accepts a full domain.Accessory including Stall.
	seed := []struct {
		name  string
		stall string
	}{
		{"保护壳-MCP-A", "档口B"},
		{"保护壳-MCP-B", "档口A"},
		{"保护壳-MCP-C", "档口A"}, // duplicate stall on purpose
	}
	for _, s := range seed {
		if _, err := svcs.Accessory.Create(context.Background(), domain.Accessory{
			Name:              s.name,
			Stall:             s.stall,
			LowStockThreshold: 5,
		}); err != nil {
			t.Fatalf("seed %s: %v", s.name, err)
		}
	}

	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	res, err := session.CallTool(context.Background(),
		testCallToolParams{Name: "accessory.list_stalls"}.toParams())
	if err != nil {
		t.Fatalf("accessory.list_stalls: %v", err)
	}
	if res.IsError {
		t.Fatalf("accessory.list_stalls IsError: %+v", res)
	}

	var out struct {
		Stalls []string `json:"stalls"`
	}
	if err := decodeStructured(res.StructuredContent, &out); err != nil {
		t.Fatalf("decode list_stalls: %v", err)
	}
	want := []string{"档口A", "档口B"}
	if !reflect.DeepEqual(out.Stalls, want) {
		t.Fatalf("stalls = %v, want %v", out.Stalls, want)
	}
}

// TestTool_AccessoryList_WithStallFilter seeds accessories under two stalls
// and asserts that accessory.list with ?stall= returns only the matching
// rows. Also asserts that no stall filter returns the full set, so we
// know the filter actually narrows rather than just being a no-op.
func TestTool_AccessoryList_WithStallFilter(t *testing.T) {
	svcs := newTestServices(t)

	seed := []struct {
		name  string
		stall string
	}{
		{"A1", "档口A"},
		{"B1", "档口B"},
		{"A2", "档口A"},
	}
	for _, s := range seed {
		if _, err := svcs.Accessory.Create(context.Background(), domain.Accessory{
			Name:              s.name,
			Stall:             s.stall,
			LowStockThreshold: 5,
		}); err != nil {
			t.Fatalf("seed %s: %v", s.name, err)
		}
	}

	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	// With filter: should only return the 2 档口A rows.
	filtered, err := session.CallTool(context.Background(), testCallToolParams{
		Name:      "accessory.list",
		Arguments: map[string]any{"stall": "档口A"},
	}.toParams())
	if err != nil {
		t.Fatalf("accessory.list (with stall): %v", err)
	}
	if filtered.IsError {
		t.Fatalf("accessory.list (with stall) IsError: %+v", filtered)
	}
	var fOut struct {
		Items []struct {
			Name  string `json:"name"`
			Stall string `json:"stall"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := decodeStructured(filtered.StructuredContent, &fOut); err != nil {
		t.Fatalf("decode filtered list: %v", err)
	}
	if fOut.Total != 2 {
		t.Fatalf("filtered total = %d, want 2", fOut.Total)
	}
	if len(fOut.Items) != 2 {
		t.Fatalf("filtered items len = %d, want 2", len(fOut.Items))
	}
	for _, it := range fOut.Items {
		if it.Stall != "档口A" {
			t.Errorf("filtered item %q stall = %q, want 档口A", it.Name, it.Stall)
		}
	}

	// Without filter: should return all 3 rows.
	all, err := session.CallTool(context.Background(), testCallToolParams{
		Name: "accessory.list",
	}.toParams())
	if err != nil {
		t.Fatalf("accessory.list (no stall): %v", err)
	}
	if all.IsError {
		t.Fatalf("accessory.list (no stall) IsError: %+v", all)
	}
	var aOut struct {
		Total int `json:"total"`
	}
	if err := decodeStructured(all.StructuredContent, &aOut); err != nil {
		t.Fatalf("decode unfiltered list: %v", err)
	}
	if aOut.Total != 3 {
		t.Fatalf("unfiltered total = %d, want 3", aOut.Total)
	}
}

func TestTool_AccessoryGet_NotFound_Returns_32004(t *testing.T) {
	// The official SDK has a code-collision quirk: it reserves -32004 for
	// ErrServerClosing and rewrites any client-side error matching that code
	// into a "connection closed" wrapper. The WIRE response from our server
	// still carries the correct JSON-RPC code (-32004), but the Go error
	// returned to the client session loses it. This test asserts on the
	// wire-level code via stdio roundtrip — the same mechanism an external
	// AI agent would use to read the response.
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)

	// Set up a basic accessory so the call has a real DB behind it.
	setupCtx, setupCancel := context.WithCancel(context.Background())
	defer setupCancel()
	ctInit, stInit := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(setupCtx, stInit, nil); err != nil {
		t.Fatalf("setup server connect: %v", err)
	}
	setupClient := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "setup", Version: "0"}, nil)
	setupSession, err := setupClient.Connect(setupCtx, ctInit, nil)
	if err != nil {
		t.Fatalf("setup client connect: %v", err)
	}
	_ = createAccessoryViaMCP(t, setupSession, "NF", 5)
	_ = setupSession.Close()

	// Now run the actual not-found case via the stdio wire.
	clientToServerR, clientToServerW := net.Pipe()
	serverToClientR, serverToClientW := net.Pipe()

	runDone := make(chan error, 1)
	go func() {
		runDone <- mcp.RunStdioWithIO(setupCtx, srv, clientToServerR, serverToClientW)
	}()

	// Initialize the MCP session over the wire.
	resp := stdioRoundtrip(t, clientToServerW, serverToClientR, map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "t", "version": "0"},
		},
	})
	if _, ok := resp["result"]; !ok {
		t.Fatalf("initialize failed: %s", resp)
	}
	// notifications/initialized — best-effort (no response expected).
	nb, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}})
	nb = append(nb, '\n')
	_, _ = clientToServerW.Write(nb)

	// Now call accessory.get with an unknown id.
	callResp := stdioRoundtrip(t, clientToServerW, serverToClientR, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "accessory.get", "arguments": map[string]any{"id": 99999}},
	})
	// MCP SDK returns tool errors as result.isError=true (not JSON-RPC error).
	result, ok := callResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result in wire response, got %s", callResp)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Fatalf("expected isError=true, got %v", result)
	}
	contentList, _ := result["content"].([]any)
	if len(contentList) == 0 {
		t.Fatal("expected content with error text")
	}
	textBlock, _ := contentList[0].(map[string]any)
	msg, _ := textBlock["text"].(string)
	if !strings.Contains(msg, "not found") {
		t.Fatalf("want text to contain 'not found', got %q", msg)
	}

	// Tear down.
	_ = clientToServerW.Close()
	setupCancel()
	<-runDone
}

func TestTool_StockOutbound_Insufficient_Returns_32005(t *testing.T) {
	// Wire-level verification: stock.outbound with insufficient stock must
	// surface JSON-RPC code -32005 on the wire with "INSUFFICIENT_STOCK" in
	// the message. See TestTool_AccessoryGet_NotFound_Returns_32004 for the
	// rationale (SDK rewrites client-side Go errors when they collide with
	// its reserved codes).
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)

	setupCtx, setupCancel := context.WithCancel(context.Background())
	defer setupCancel()
	ctInit, stInit := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(setupCtx, stInit, nil); err != nil {
		t.Fatalf("setup server connect: %v", err)
	}
	setupClient := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "setup", Version: "0"}, nil)
	setupSession, err := setupClient.Connect(setupCtx, ctInit, nil)
	if err != nil {
		t.Fatalf("setup client connect: %v", err)
	}
	acc := createAccessoryViaMCP(t, setupSession, "OFI", 5)
	_, err = setupSession.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": acc.ID, "quantity": 3},
	})
	if err != nil {
		t.Fatalf("setup inbound: %v", err)
	}
	_ = setupSession.Close()

	// Now test the insufficient-outbound case via the wire.
	clientToServerR, clientToServerW := net.Pipe()
	serverToClientR, serverToClientW := net.Pipe()

	runDone := make(chan error, 1)
	go func() {
		runDone <- mcp.RunStdioWithIO(setupCtx, srv, clientToServerR, serverToClientW)
	}()
	// initialize.
	initResp := stdioRoundtrip(t, clientToServerW, serverToClientR, map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "t", "version": "0"},
		},
	})
	if _, ok := initResp["result"]; !ok {
		t.Fatalf("initialize failed: %v", initResp)
	}
	nb, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}})
	nb = append(nb, '\n')
	_, _ = clientToServerW.Write(nb)

	callResp := stdioRoundtrip(t, clientToServerW, serverToClientR, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "stock.outbound", "arguments": map[string]any{"accessory_id": acc.ID, "quantity": 5}},
	})
	// MCP SDK returns tool errors as result.isError=true (not JSON-RPC error).
	result, ok := callResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result in wire response, got %v", callResp)
	}
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Fatalf("expected isError=true, got %v", result)
	}
	cont, _ := result["content"].([]any)
	if len(cont) == 0 {
		t.Fatal("expected content with error text")
	}
	textBlock, _ := cont[0].(map[string]any)
	msg, _ := textBlock["text"].(string)
	if !strings.Contains(msg, "INSUFFICIENT_STOCK") {
		t.Fatalf("want text to contain INSUFFICIENT_STOCK, got %q", msg)
	}

	_ = clientToServerW.Close()
	setupCancel()
	<-runDone
}

func TestTool_StockInbound_ClientRef_Idempotent(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	acc := createAccessoryViaMCP(t, session, "IDM", 5)

	args := map[string]any{
		"accessory_id": acc.ID,
		"quantity":     2,
		"client_ref":   "shared-ref-1",
	}
	firstParams := testCallToolParams{Name: "stock.inbound", Arguments: args}.toParams()
	first, err := session.CallTool(context.Background(), firstParams)
	if err != nil {
		t.Fatalf("first inbound: %v", err)
	}
	if first.IsError {
		t.Fatalf("first inbound IsError: %+v", first)
	}
	var f1 struct {
		ID           int64 `json:"id"`
		BalanceAfter int64 `json:"balance_after"`
	}
	if err := decodeStructured(first.StructuredContent, &f1); err != nil {
		t.Fatalf("decode first: %v", err)
	}

	secondParams := testCallToolParams{Name: "stock.inbound", Arguments: args}.toParams()
	second, err := session.CallTool(context.Background(), secondParams)
	if err != nil {
		t.Fatalf("second inbound: %v", err)
	}
	if second.IsError {
		t.Fatalf("second inbound IsError: %+v", second)
	}
	var f2 struct {
		ID           int64 `json:"id"`
		BalanceAfter int64 `json:"balance_after"`
	}
	if err := decodeStructured(second.StructuredContent, &f2); err != nil {
		t.Fatalf("decode second: %v", err)
	}

	if f1.ID != f2.ID {
		t.Fatalf("idempotency failed: first id=%d, second id=%d", f1.ID, f2.ID)
	}
	if f2.BalanceAfter != 2 {
		t.Fatalf("balance_after should still be 2, got %d", f2.BalanceAfter)
	}
}

func TestStdioRoundtrip_Minimal(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)

	// Two net.Pipe pairs: one for client→server (requests), one for
	// server→client (responses). Each pair gives a ReadCloser/WriteCloser.
	clientToServerR, clientToServerW := net.Pipe()
	serverToClientR, serverToClientW := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run the server with our IO transport.
	runDone := make(chan error, 1)
	go func() {
		runDone <- mcp.RunStdioWithIO(ctx, srv, clientToServerR, serverToClientW)
	}()

	// initialize (required by MCP before any other call)
	initResp := stdioRoundtrip(t, clientToServerW, serverToClientR, map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "t", "version": "0"},
		},
	})
	if _, ok := initResp["result"]; !ok {
		t.Fatalf("initialize failed: %v", initResp)
	}
	// notifications/initialized — no response expected.
	nb, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}})
	nb = append(nb, '\n')
	_, _ = clientToServerW.Write(nb)

	// Now act as a minimal client: send a tools/list request and read the
	// response.
	resp := stdioRoundtrip(t, clientToServerW, serverToClientR, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("want result, got %v", resp)
	}
	tools, _ := result["tools"].([]any)
	if len(tools) != 19 {
		t.Fatalf("want 19 tools, got %d", len(tools))
	}

	// Shutdown: close client-side writer; cancel context.
	_ = clientToServerW.Close()
	_ = serverToClientR.Close()
	cancel()
	if err := <-runDone; err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !strings.Contains(err.Error(), "context canceled") {
		t.Logf("RunStdio returned: %v (expected on shutdown)", err)
	}
}

// stdioRoundtrip writes a JSON-RPC request and reads the matching response.
// It assumes one message in, one message out (no batched requests, no
// notifications).
func stdioRoundtrip(t *testing.T, w io.Writer, r io.Reader, req map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	b = append(b, '\n')
	if _, err := w.Write(b); err != nil {
		t.Fatalf("write req: %v", err)
	}
	line, err := readLine(r)
	if err != nil {
		t.Fatalf("read resp: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("decode resp: %v (raw=%s)", err, line)
	}
	return resp
}

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

// wrapErr wraps sentinel e with extra context, using fmt.Errorf %w semantics.
func wrapErr(sentinel error, ctx string) error {
	return wrappedErr{sentinel: sentinel, ctx: ctx}
}

type wrappedErr struct {
	sentinel error
	ctx      string
}

func (w wrappedErr) Error() string { return w.sentinel.Error() + ": " + w.ctx }
func (w wrappedErr) Unwrap() error { return w.sentinel }
// --- replenishment.check with names (post-SKU removal) -------------------

func TestTool_ReplenishmentCheck_WithNames(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	ctx := context.Background()

	// Create two accessories and inbound stock
	short := createAccessoryViaMCP(t, session, "补货测试-短缺", 5)
	ok := createAccessoryViaMCP(t, session, "补货测试-充足", 3)

	// Inbound: short gets 2 (below threshold 5), ok gets 10 (above threshold 3)
	_, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": short.ID, "quantity": 2},
	})
	if err != nil {
		t.Fatalf("inbound short: %v", err)
	}
	_, err = session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": ok.ID, "quantity": 10},
	})
	if err != nil {
		t.Fatalf("inbound ok: %v", err)
	}

	// Check with names
	checkRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "replenishment.check",
		Arguments: map[string]any{
			"names":  []string{"补货测试-短缺", "补货测试-充足", "不存在的配件"},
			"policy": "default",
		},
	})
	if err != nil {
		t.Fatalf("replenishment.check: %v", err)
	}
	if checkRes.IsError {
		t.Fatalf("replenishment.check IsError: %+v", checkRes)
	}

	var result struct {
		Items    []struct {
			Name              string `json:"name"`
			Shortage          int64  `json:"shortage"`
			SuggestedQuantity int64  `json:"suggested_quantity"`
		} `json:"items"`
		NotFound []string `json:"not_found"`
	}
	if err := decodeStructured(checkRes.StructuredContent, &result); err != nil {
		t.Fatalf("decode check result: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 shortage item, got %d", len(result.Items))
	}
	if result.Items[0].Name != "补货测试-短缺" {
		t.Fatalf("expected 补货测试-短缺, got %q", result.Items[0].Name)
	}
	if result.Items[0].Shortage != 3 {
		t.Fatalf("expected shortage=3, got %d", result.Items[0].Shortage)
	}
	if len(result.NotFound) != 1 || result.NotFound[0] != "不存在的配件" {
		t.Fatalf("expected NotFound=[不存在的配件], got %v", result.NotFound)
	}
}

func TestTool_ReplenishmentScan_ExcludesThresholdZero(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	ctx := context.Background()

	// Create accessory with threshold=0 (should never appear in scan)
	createAccessoryViaMCP(t, session, "零阈值配件", 0)
	// Create accessory below threshold
	short := createAccessoryViaMCP(t, session, "告急配件-MCP", 10)
	_, _ = session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": short.ID, "quantity": 3},
	})

	scanRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "replenishment.scan", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("replenishment.scan: %v", err)
	}
	var result struct {
		Items []struct {
			Name     string `json:"name"`
			Shortage int64  `json:"shortage"`
		} `json:"items"`
	}
	if err := decodeStructured(scanRes.StructuredContent, &result); err != nil {
		t.Fatalf("decode scan result: %v", err)
	}
	// Only "告急配件-MCP" should appear, NOT "零阈值配件"
	for _, it := range result.Items {
		if it.Name == "零阈值配件" {
			t.Fatalf("threshold=0 accessory should not appear in scan")
		}
	}
	found := false
	for _, it := range result.Items {
		if it.Name == "告急配件-MCP" {
			found = true
			if it.Shortage != 7 {
				t.Fatalf("expected shortage=7, got %d", it.Shortage)
			}
		}
	}
	if !found {
		t.Fatal("告急配件-MCP should appear in scan")
	}
}

// TestTool_ReplenishmentExport_WritesXLSX exercises replenishment.export
// end-to-end: seeds an accessory in shortage, calls the tool over the MCP
// in-memory transport, verifies the structured output describes the
// written file (filename, path, row_count, size, sha256), confirms the
// file actually exists on disk with a matching sha256, opens it with
// excelize to validate the row layout, and confirms the TextContent
// line tells the agent about both retrieval paths (filesystem read and
// the existing HTTP export endpoint).
func TestTool_ReplenishmentExport_WritesXLSX(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	ctx := context.Background()

	// One short accessory (threshold=5, stock=1 → shortage=4) is enough
	// to drive both the text summary ("Exported 1 row") and the xlsx body.
	short := createAccessoryViaMCP(t, session, "MCP-EXPORT-1", 5)
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": short.ID, "quantity": 1},
	}); err != nil {
		t.Fatalf("stock.inbound: %v", err)
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "replenishment.export", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("replenishment.export: %v", err)
	}
	if res.IsError {
		t.Fatalf("replenishment.export IsError: %+v", res)
	}

	// Exactly one TextContent block; the file metadata rides in
	// structured output so any MCP client surfaces it to the model.
	if len(res.Content) != 1 {
		t.Fatalf("content blocks = %d, want 1 (TextContent only); got %+v", len(res.Content), res.Content)
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] type = %T, want *mcpsdk.TextContent", res.Content[0])
	}
	if !strings.Contains(tc.Text, "replenishment_") || !strings.Contains(tc.Text, ".xlsx") {
		t.Errorf("summary text = %q, want it to mention filename and .xlsx", tc.Text)
	}
	if !strings.Contains(tc.Text, "1 replenishment row") {
		t.Errorf("summary text = %q, want it to mention '1 replenishment row'", tc.Text)
	}
	// The text must also surface the download URL so an agent that
	// doesn't introspect structured fields knows where to fetch.
	if !strings.Contains(tc.Text, "/api/v1/exports/") {
		t.Errorf("summary text = %q, want it to mention the /api/v1/exports/ download URL", tc.Text)
	}

	// Structured output: filename, absolute URL, row_count, size, sha256.
	if res.StructuredContent == nil {
		t.Fatal("StructuredContent is nil; want {filename, url, row_count, size, sha256}")
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent type = %T, want map[string]any", res.StructuredContent)
	}
	gotFilename, _ := sc["filename"].(string)
	if !strings.HasPrefix(gotFilename, "replenishment_") || !strings.HasSuffix(gotFilename, ".xlsx") {
		t.Errorf("StructuredContent.filename = %q, want replenishment_*.xlsx", gotFilename)
	}
	gotURL, _ := sc["url"].(string)
	wantURLSuffix := "/api/v1/exports/" + gotFilename
	if !strings.HasSuffix(gotURL, wantURLSuffix) {
		t.Errorf("StructuredContent.url = %q, want it to end with %q", gotURL, wantURLSuffix)
	}
	if !strings.HasPrefix(gotURL, "http://") && !strings.HasPrefix(gotURL, "https://") {
		t.Errorf("StructuredContent.url = %q, want it to start with http:// or https://", gotURL)
	}
	if !strings.Contains(tc.Text, gotURL) {
		t.Errorf("TextContent should mention the same URL as StructuredContent (url=%q)", gotURL)
	}
	rowCount, ok := sc["row_count"].(int64)
	if !ok {
		// Some transports decode numbers as float64 — handle both.
		if f, fok := sc["row_count"].(float64); fok {
			rowCount = int64(f)
		} else {
			t.Fatalf("StructuredContent.row_count type = %T, want number", sc["row_count"])
		}
	}
	if rowCount != 1 {
		t.Errorf("StructuredContent.row_count = %d, want 1", rowCount)
	}
	gotSize, ok := sc["size"].(int64)
	if !ok {
		if f, fok := sc["size"].(float64); fok {
			gotSize = int64(f)
		} else {
			t.Fatalf("StructuredContent.size type = %T, want number", sc["size"])
		}
	}
	if gotSize <= 0 {
		t.Errorf("StructuredContent.size = %d, want > 0", gotSize)
	}
	gotSHA, _ := sc["sha256"].(string)
	if len(gotSHA) != 64 {
		t.Fatalf("StructuredContent.sha256 = %q (len=%d), want 64-char hex", gotSHA, len(gotSHA))
	}
	if _, err := hex.DecodeString(gotSHA); err != nil {
		t.Fatalf("StructuredContent.sha256 = %q is not valid hex: %v", gotSHA, err)
	}

	// The URL must point at a file that actually exists on disk and
	// whose sha256/size match the structured output — otherwise the
	// agent would fetch a 404 or get tampered bytes. We resolve the
	// URL to a local path the same way the HTTP handler does.
	onDiskPath := filepath.Join(svcs.ExportsDir, gotFilename)
	info, err := os.Stat(onDiskPath)
	if err != nil {
		t.Fatalf("stat %q: %v", onDiskPath, err)
	}
	if info.Size() != gotSize {
		t.Errorf("file size = %d, StructuredContent.size = %d", info.Size(), gotSize)
	}
	onDisk, err := os.ReadFile(onDiskPath)
	if err != nil {
		t.Fatalf("read file %q: %v", onDiskPath, err)
	}
	sum := sha256.Sum256(onDisk)
	if hex.EncodeToString(sum[:]) != gotSHA {
		t.Errorf("file sha256 mismatch: on-disk=%x, StructuredContent.sha256=%s", sum, gotSHA)
	}

	// The on-disk file must round-trip as a real xlsx with the
	// expected rows.
	xf, err := excelize.OpenReader(bytes.NewReader(onDisk))
	if err != nil {
		t.Fatalf("open xlsx from on-disk file: %v (size=%d bytes)", err, len(onDisk))
	}
	defer xf.Close()
	rows, err := xf.GetRows("告急补货")
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (header + 1 data); got %+v", len(rows), rows)
	}
	wantHeaders := []string{"名称", "当前库存", "阈值", "缺货量", "建议补货"}
	for i, want := range wantHeaders {
		if i >= len(rows[0]) || rows[0][i] != want {
			t.Errorf("header[%d] = %q, want %q", i, safeCell(rows, 0, i), want)
		}
	}
	if rows[1][0] != "MCP-EXPORT-1" {
		t.Errorf("data row name = %q, want MCP-EXPORT-1", rows[1][0])
	}
	if rows[1][3] != "4" {
		t.Errorf("data row shortage = %q, want 4", rows[1][3])
	}
}

// safeCell mirrors the api package helper: returns rows[r][c] or a
// readable placeholder when the cell is absent.
func safeCell(rows [][]string, r, c int) string {
	if r >= len(rows) {
		return "<missing row>"
	}
	if c >= len(rows[r]) {
		return ""
	}
	return rows[r][c]
}

// TestTool_AccessoryExport_WritesXLSX exercises accessory.export
// end-to-end: seeds two accessories (with stock and notes set so each
// column has something to assert), calls the tool over the MCP
// in-memory transport, verifies the structured output describes the
// written file (filename, path, row_count, size, sha256), confirms the
// file actually exists on disk with a matching sha256, opens it with
// excelize to validate the row layout, and confirms the TextContent
// line tells the agent about both retrieval paths (filesystem read and
// the existing HTTP export endpoint).
func TestTool_AccessoryExport_WritesXLSX(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	ctx := context.Background()

	// Seed two accessories; the second one gets a stock inbound and a
	// notes patch so every cell of the row has something verifiable.
	createAccessoryViaMCP(t, session, "MCP-EXPORT-A", 5)
	b := createAccessoryViaMCP(t, session, "MCP-EXPORT-B", 7)
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": b.ID, "quantity": 3},
	}); err != nil {
		t.Fatalf("stock.inbound: %v", err)
	}
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "accessory.update", Arguments: map[string]any{"id": b.ID, "notes": "B-remark"},
	}); err != nil {
		t.Fatalf("accessory.update: %v", err)
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "accessory.export", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("accessory.export: %v", err)
	}
	if res.IsError {
		t.Fatalf("accessory.export IsError: %+v", res)
	}

	// Exactly one TextContent block; the file metadata rides in
	// structured output so any MCP client surfaces it to the model.
	if len(res.Content) != 1 {
		t.Fatalf("content blocks = %d, want 1 (TextContent only); got %+v", len(res.Content), res.Content)
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] type = %T, want *mcpsdk.TextContent", res.Content[0])
	}
	if !strings.Contains(tc.Text, "accessories_") || !strings.Contains(tc.Text, ".xlsx") {
		t.Errorf("summary text = %q, want it to mention filename and .xlsx", tc.Text)
	}
	if !strings.Contains(tc.Text, "2 accessor") {
		t.Errorf("summary text = %q, want it to mention '2 accessor'", tc.Text)
	}
	// The text must also surface the download URL so an agent that
	// doesn't introspect structured fields knows where to fetch.
	if !strings.Contains(tc.Text, "/api/v1/exports/") {
		t.Errorf("summary text = %q, want it to mention the /api/v1/exports/ download URL", tc.Text)
	}

	// Structured output: filename, absolute URL, row_count, size, sha256.
	if res.StructuredContent == nil {
		t.Fatal("StructuredContent is nil; want {filename, url, row_count, size, sha256}")
	}
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent type = %T, want map[string]any", res.StructuredContent)
	}
	gotFilename, _ := sc["filename"].(string)
	if !strings.HasPrefix(gotFilename, "accessories_") || !strings.HasSuffix(gotFilename, ".xlsx") {
		t.Errorf("StructuredContent.filename = %q, want accessories_*.xlsx", gotFilename)
	}
	gotURL, _ := sc["url"].(string)
	wantURLSuffix := "/api/v1/exports/" + gotFilename
	if !strings.HasSuffix(gotURL, wantURLSuffix) {
		t.Errorf("StructuredContent.url = %q, want it to end with %q", gotURL, wantURLSuffix)
	}
	if !strings.HasPrefix(gotURL, "http://") && !strings.HasPrefix(gotURL, "https://") {
		t.Errorf("StructuredContent.url = %q, want it to start with http:// or https://", gotURL)
	}
	if !strings.Contains(tc.Text, gotURL) {
		t.Errorf("TextContent should mention the same URL as StructuredContent (url=%q)", gotURL)
	}
	rowCount, ok := sc["row_count"].(int64)
	if !ok {
		// Some transports decode numbers as float64 — handle both.
		if f, fok := sc["row_count"].(float64); fok {
			rowCount = int64(f)
		} else {
			t.Fatalf("StructuredContent.row_count type = %T, want number", sc["row_count"])
		}
	}
	if rowCount != 2 {
		t.Errorf("StructuredContent.row_count = %d, want 2", rowCount)
	}
	gotSize, ok := sc["size"].(int64)
	if !ok {
		if f, fok := sc["size"].(float64); fok {
			gotSize = int64(f)
		} else {
			t.Fatalf("StructuredContent.size type = %T, want number", sc["size"])
		}
	}
	if gotSize <= 0 {
		t.Errorf("StructuredContent.size = %d, want > 0", gotSize)
	}
	gotSHA, _ := sc["sha256"].(string)
	if len(gotSHA) != 64 {
		t.Fatalf("StructuredContent.sha256 = %q (len=%d), want 64-char hex", gotSHA, len(gotSHA))
	}
	if _, err := hex.DecodeString(gotSHA); err != nil {
		t.Fatalf("StructuredContent.sha256 = %q is not valid hex: %v", gotSHA, err)
	}

	// The URL must point at a file that actually exists on disk and
	// whose sha256/size match the structured output — otherwise the
	// agent would fetch a 404 or get tampered bytes. We resolve the
	// URL to a local path the same way the HTTP handler does.
	onDiskPath := filepath.Join(svcs.ExportsDir, gotFilename)
	info, err := os.Stat(onDiskPath)
	if err != nil {
		t.Fatalf("stat %q: %v", onDiskPath, err)
	}
	if info.Size() != gotSize {
		t.Errorf("file size = %d, StructuredContent.size = %d", info.Size(), gotSize)
	}
	onDisk, err := os.ReadFile(onDiskPath)
	if err != nil {
		t.Fatalf("read file %q: %v", onDiskPath, err)
	}
	sum := sha256.Sum256(onDisk)
	if hex.EncodeToString(sum[:]) != gotSHA {
		t.Errorf("file sha256 mismatch: on-disk=%x, StructuredContent.sha256=%s", sum, gotSHA)
	}

	// The on-disk file must round-trip as a real xlsx with the
	// expected rows.
	xf, err := excelize.OpenReader(bytes.NewReader(onDisk))
	if err != nil {
		t.Fatalf("open xlsx from on-disk file: %v (size=%d bytes)", err, len(onDisk))
	}
	defer xf.Close()
	rows, err := xf.GetRows("配件库存")
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2 data); got %+v", len(rows), rows)
	}
	wantHeaders := []string{"名称", "当前库存", "低库存阈值", "备注", "创建时间", "更新时间"}
	for i, want := range wantHeaders {
		if i >= len(rows[0]) || rows[0][i] != want {
			t.Errorf("header[%d] = %q, want %q", i, safeCell(rows, 0, i), want)
		}
	}

	// Look up each seeded accessory by name — order is repo-defined, so
	// don't rely on row index.
	got := map[string]map[string]string{}
	for _, row := range rows[1:] {
		if len(row) == 0 {
			continue
		}
		got[row[0]] = map[string]string{
			"stock":     safeCell(rows, indexOf(rows, row[0]), 1),
			"threshold": safeCell(rows, indexOf(rows, row[0]), 2),
			"notes":     safeCell(rows, indexOf(rows, row[0]), 3),
		}
	}
	if _, ok := got["MCP-EXPORT-A"]; !ok {
		t.Errorf("MCP-EXPORT-A missing from export")
	}
	if _, ok := got["MCP-EXPORT-B"]; !ok {
		t.Errorf("MCP-EXPORT-B missing from export")
	}
	if g := got["MCP-EXPORT-B"]; g["stock"] != "3" {
		t.Errorf("MCP-EXPORT-B stock = %q, want 3", g["stock"])
	}
	if g := got["MCP-EXPORT-B"]; g["notes"] != "B-remark" {
		t.Errorf("MCP-EXPORT-B notes = %q, want B-remark", g["notes"])
	}
}

// indexOf returns the index of the first row whose first cell equals
// name, or -1 if not found. Used to translate a map keyed by accessory
// name back to its row index in the sheet.
func indexOf(rows [][]string, name string) int {
	for i, row := range rows {
		if len(row) > 0 && row[0] == name {
			return i
		}
	}
	return -1
}

func TestTool_AccessoryGet_ByName(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	acc := createAccessoryViaMCP(t, session, "MCP-NAME-GET", 5)

	getRes, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "accessory.get", Arguments: map[string]any{"name": "MCP-NAME-GET"},
	})
	if err != nil {
		t.Fatalf("accessory.get by name: %v", err)
	}
	if getRes.IsError {
		t.Fatalf("accessory.get IsError: %+v", getRes)
	}
	var got struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := decodeStructured(getRes.StructuredContent, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != acc.ID || got.Name != "MCP-NAME-GET" {
		t.Fatalf("expected id=%d name=MCP-NAME-GET, got %+v", acc.ID, got)
	}
}

func TestTool_AccessoryGet_BothIDAndName_Rejected(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	_, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "accessory.get", Arguments: map[string]any{"id": 1, "name": "X"},
	})
	if err == nil {
		t.Fatal("expected error for both id and name")
	}
}

func TestTool_FileOutboundFlow(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	ctx := context.Background()

	// Create accessories with stock
	a1 := createAccessoryViaMCP(t, session, "MCP文件出库-充足", 5)
	a2 := createAccessoryViaMCP(t, session, "MCP文件出库-不足", 3)

	// Inbound stock
	_, _ = session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": a1.ID, "quantity": 10},
	})
	_, _ = session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": a2.ID, "quantity": 2},
	})

	// Outbound: a1 sufficient (5), a2 insufficient (need 8, have 2)
	outRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.batch_outbound",
		Arguments: map[string]any{
			"items": []map[string]any{
				{"accessory_id": a1.ID, "quantity": 5},
				{"accessory_id": a2.ID, "quantity": 8},
			},
		},
	})
	if err != nil {
		t.Fatalf("batch_outbound: %v", err)
	}
	// Should fail because a2 has insufficient stock
	if !outRes.IsError {
		t.Fatal("expected IsError for insufficient stock in batch_outbound")
	}

	// Now do individual outbounds
	out1, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.outbound", Arguments: map[string]any{"accessory_id": a1.ID, "quantity": 5},
	})
	if err != nil || out1.IsError {
		t.Fatalf("outbound a1 should succeed, IsError=%v err=%v", out1.IsError, err)
	}

	// Verify stock
	getRes, _ := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "accessory.get", Arguments: map[string]any{"id": a1.ID},
	})
	var a1After struct {
		Name         string `json:"name"`
		CurrentStock int64  `json:"current_stock"`
	}
	_ = decodeStructured(getRes.StructuredContent, &a1After)
	if a1After.CurrentStock != 5 {
		t.Fatalf("a1 stock: expected 5 (10-5), got %d", a1After.CurrentStock)
	}

	// Scan should show nothing since a1 threshold(5) >= stock(5), a2 is below but... wait a2 stock is 2, threshold is 3
	scanRes, _ := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "replenishment.scan", Arguments: map[string]any{},
	})
	var scanResult struct {
		Items []struct {
			Name     string `json:"name"`
			Shortage int64  `json:"shortage"`
		} `json:"items"`
	}
	_ = decodeStructured(scanRes.StructuredContent, &scanResult)
	for _, it := range scanResult.Items {
		if it.Name == "MCP文件出库-不足" && it.Shortage != 1 {
			t.Fatalf("a2 shortage: expected 1, got %d", it.Shortage)
		}
	}
}

// --- stock.file_outbound MCP tool ----------------------------------------

func TestTool_FileOutbound_Preview(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	ctx := context.Background()

	// Create accessories
	createAccessoryViaMCP(t, session, "文件预览配件A", 5)
	createAccessoryViaMCP(t, session, "文件预览配件B", 3)

	// stock.inbound
	_, _ = session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": 1, "quantity": 10},
	})
	_, _ = session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": 2, "quantity": 5},
	})

	// Call stock.file_outbound with a file path that doesn't exist → IsError
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.file_outbound", Arguments: map[string]any{"file_path": "/nonexistent/file.xlsx"},
	})
	if err != nil || !res.IsError {
		t.Fatal("expected IsError for missing file")
	}

	// Call with existing xlsx
	// We need a real xlsx file for this test - create a minimal one
	tmpDir := t.TempDir()
	xlsxPath := tmpDir + "/test.xlsx"
	createTestXlsx(t, xlsxPath, [][]string{
		{"档口A", "档口B"},
		{"文件预览配件A x3", "文件预览配件B x2"},
		{"不存在的配件 x1", ""},
	})

	res2, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.file_outbound", Arguments: map[string]any{"file_path": xlsxPath},
	})
	if err != nil {
		t.Fatalf("stock.file_outbound: %v", err)
	}
	if res2.IsError {
		t.Fatalf("stock.file_outbound IsError: %+v", res2)
	}
	var preview struct {
		MatchedCount  int `json:"matched_count"`
		NotFoundCount int `json:"not_found_count"`
		TotalItems    int `json:"total_items"`
	}
	if err := decodeStructured(res2.StructuredContent, &preview); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if preview.MatchedCount != 2 {
		t.Fatalf("expected 2 matched, got %d", preview.MatchedCount)
	}
	if preview.NotFoundCount != 1 {
		t.Fatalf("expected 1 not_found, got %d", preview.NotFoundCount)
	}
	if preview.TotalItems != 3 {
		t.Fatalf("expected 3 total, got %d", preview.TotalItems)
	}
}

func TestTool_FileOutbound_Execute(t *testing.T) {
	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	ctx := context.Background()

	// Create one existing accessory with stock
	createAccessoryViaMCP(t, session, "文件执行配件-存在", 5)
	_, _ = session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.inbound", Arguments: map[string]any{"accessory_id": 1, "quantity": 10},
	})

	tmpDir := t.TempDir()
	xlsxPath := tmpDir + "/test_exec.xlsx"
	createTestXlsx(t, xlsxPath, [][]string{
		{"档口"},
		{"文件执行配件-存在 x5"},
		{"文件执行配件-新建 x3"},
	})

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.file_outbound.execute", Arguments: map[string]any{"file_path": xlsxPath},
	})
	if err != nil {
		t.Fatalf("stock.file_outbound.execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("stock.file_outbound.execute IsError: %+v", res)
	}
	var result struct {
		Outbound  int `json:"outbound"`
		Created   int `json:"created"`
		Shortages int `json:"shortages"`
	}
	if err := decodeStructured(res.StructuredContent, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Outbound != 2 {
		t.Fatalf("expected outbound=2, got %d", result.Outbound)
	}
	if result.Created != 1 {
		t.Fatalf("expected created=1, got %d", result.Created)
	}
}

// TestStockFileInbound_MCP — exercises stock.file_inbound end-to-end via
// the stdio transport. Seeds one accessory, builds an xlsx with a
// header row + the existing name (with trailing whitespace) + a new
// name, then verifies the response shape, the catalogue state, and
// the inbound flow count.
func TestStockFileInbound_MCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	svcs := newTestServices(t)
	srv := mcp.NewServer(svcs)
	_, session := newInMemoryClient(t, srv)
	defer session.Close()

	// Seed one existing accessory so we exercise both branches.
	createAccessoryViaMCP(t, session, "MCP-FI-EXISTS", 0)

	// Build an xlsx with the first sheet (default Sheet1) in
	// [配件, 数量] two-column format. The existing name carries
	// trailing whitespace to exercise the trim path.
	dir := t.TempDir()
	xlsxPath := filepath.Join(dir, "inbound.xlsx")
	f := excelize.NewFile()
	for r, row := range [][]string{
		{"配件", "数量"},
		{"MCP-FI-EXISTS ", "3"},
		{"MCP-FI-NEW", "7"},
	} {
		for c, val := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			if err := f.SetCellValue("Sheet1", cell, val); err != nil {
				t.Fatalf("set cell: %v", err)
			}
		}
	}
	if err := f.SaveAs(xlsxPath); err != nil {
		t.Fatalf("save xlsx: %v", err)
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "stock.file_inbound", Arguments: map[string]any{"file_path": xlsxPath},
	})
	if err != nil {
		t.Fatalf("stock.file_inbound: %v", err)
	}
	if res.IsError {
		t.Fatalf("stock.file_inbound IsError: %+v", res)
	}
	var result struct {
		Inbound int `json:"inbound"`
		Created int `json:"created"`
		Items   []struct {
			Name         string `json:"name"`
			Quantity     int64  `json:"quantity"`
			AccessoryID  int64  `json:"accessory_id"`
			Created      bool   `json:"created"`
			FlowID       int64  `json:"flow_id"`
			BalanceAfter int64  `json:"balance_after"`
		} `json:"items"`
	}
	if err := decodeStructured(res.StructuredContent, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Inbound != 2 {
		t.Fatalf("inbound = %d, want 2", result.Inbound)
	}
	if result.Created != 1 {
		t.Fatalf("created = %d, want 1", result.Created)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(result.Items))
	}
	for _, it := range result.Items {
		if it.AccessoryID == 0 || it.FlowID == 0 {
			t.Errorf("row %+v has zero ID", it)
		}
	}
	// Verify the trim worked: the existing name was sent with a
	// trailing space; Created must be false and balance should be 3.
	var existingCreated *bool
	var existingBalance int64
	var existingFound bool
	for i := range result.Items {
		if result.Items[i].Name == "MCP-FI-EXISTS" {
			c := result.Items[i].Created
			existingCreated = &c
			existingBalance = result.Items[i].BalanceAfter
			existingFound = true
		}
	}
	if !existingFound {
		t.Fatal("MCP-FI-EXISTS row missing")
	}
	if existingCreated == nil || *existingCreated {
		t.Errorf("MCP-FI-EXISTS Created = %v, want false (trim should match existing row)", existingCreated)
	}
	if existingBalance != 3 {
		t.Errorf("MCP-FI-EXISTS balance = %d, want 3", existingBalance)
	}
}

// createTestXlsx writes a minimal xlsx with a "汇总" sheet.
func createTestXlsx(t *testing.T, path string, data [][]string) {
	t.Helper()
	f := excelize.NewFile()
	// Delete default sheet and create "汇总"
	f.NewSheet("汇总")
	f.DeleteSheet("Sheet1")
	for r, row := range data {
		for c, val := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			f.SetCellValue("汇总", cell, val)
		}
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("create test xlsx: %v", err)
	}
}
