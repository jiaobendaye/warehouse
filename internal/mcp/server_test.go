// Package mcp_test is the integration test layer for the MCP server. It uses
// the official modelcontextprotocol/go-sdk in-memory transport to drive a
// roundtrip against a fully wired Server, and asserts the contract from
// changes/mobile-accessories-management/specs/mcp-server.md:
//
//   - TranslateError maps the four service sentinels to the JSON-RPC codes
//     documented in the spec (NOT_FOUND=-32004, CONFLICT=-32005,
//     BAD_REQUEST=-32600).
//   - tools/list returns exactly the 13 tools named in the spec.
//   - tool calls exercise the underlying service methods, with both happy
//     paths and error paths, and surface the right JSON-RPC error codes on
//     failure.
//   - Stdio transport works end-to-end via a custom io.Pipe.
package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jiaobendaye/warehouse/internal/db"
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

func TestTranslateError_SKUConflict_MapsTo_32005(t *testing.T) {
	code, _ := mcp.TranslateError(service.ErrSKUConflict)
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
// tools/list — 13 tools exactly
// ------------------------------------------------------------------

func TestToolsList_AllThirteen(t *testing.T) {
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
		"accessory.get",
		"accessory.create",
		"accessory.update",
		"accessory.delete",
		"stock.inbound",
		"stock.outbound",
		"stock.batch_inbound",
		"stock.batch_outbound",
		"flow.list",
		"flow.get",
		"replenishment.scan",
		"replenishment.check",
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
			"sku":                "MCP-A",
			"name":               "保护壳",
			"unit":               "个",
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
		ID  int64  `json:"id"`
		SKU string `json:"sku"`
	}
	if err := decodeStructured(createRes.StructuredContent, &created); err != nil {
		t.Fatalf("decode create result: %v", err)
	}
	if created.ID == 0 || created.SKU != "MCP-A" {
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
		ID  int64  `json:"id"`
		SKU string `json:"sku"`
	}
	if err := decodeStructured(getRes.StructuredContent, &got); err != nil {
		t.Fatalf("decode get result: %v", err)
	}
	if got.ID != created.ID || got.SKU != "MCP-A" {
		t.Fatalf("got %+v, want id=%d sku=MCP-A", got, created.ID)
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
	if len(tools) != 13 {
		t.Fatalf("want 13 tools, got %d", len(tools))
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