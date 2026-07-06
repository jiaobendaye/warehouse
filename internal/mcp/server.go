// Package mcp is the Model Context Protocol transport layer for the
// warehouse. It registers the 13 tools documented in
// changes/mobile-accessories-management/specs/mcp-server.md on top of the
// existing service layer, and exposes both a stdio transport (for AI agents
// that launch the binary as a subprocess) and an HTTP/SSE handler at /mcp
// (for the web server in Batch 9).
//
// Error mapping (TranslateError):
//
//	service.ErrNotFound                → -32004 (NOT_FOUND)
//	service.ErrNameConflict             → -32005 (CONFLICT)
//	service.ErrHasFlow                 → -32005 (CONFLICT)
//	service.ErrInsufficientStock       → -32005 (CONFLICT, "INSUFFICIENT_STOCK")
//	service.ErrInvalidInput            → -32600 (BAD_REQUEST)
//	any other error                    → -32603 (InternalError, sanitized)
//
// The function is exported so tests can pin the mapping without going through
// the SDK transport.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jiaobendaye/warehouse/internal/service"
)

// JSON-RPC error codes used by the warehouse MCP server. We use the SDK's
// standard code for parse/invalid request/method-not-found/invalid-params/
// internal-error, and pick custom codes from the JSON-RPC server-error range
// (-32000..-32099) for NOT_FOUND (-32004) and CONFLICT (-32005) to match
// the spec.
const (
	CodeNotFound        = -32004
	CodeConflict        = -32005
	CodeBadRequest      = -32600
	CodeInternalError   = -32603
	CodeInvalidParams   = -32602 // JSON-RPC standard: invalid method params
	CodeMethodNotFound  = -32601 // JSON-RPC standard
	CodeParseError      = -32700 // JSON-RPC standard
	codeInsufficientTag = "INSUFFICIENT_STOCK"
)

// Services bundles the four service handles an MCP server needs. It mirrors
// api.Services so a single struct can be passed everywhere.
type Services struct {
	Accessory     *service.AccessoryService
	Stock         *service.StockService
	Flow          *service.FlowService
	Replenishment *service.ReplenishmentService
}

// implementation is the value we pass to mcpsdk.NewServer. It identifies the
// warehouse to MCP clients during the initialize handshake.
var implementation = &mcpsdk.Implementation{
	Name:    "warehouse",
	Version: "1.0.0",
}

// NewServer builds a fully wired *mcpsdk.Server with all 16 tools registered.
// It does not start any transport — call RunStdio or Handler to expose it.
func NewServer(svcs Services) *mcpsdk.Server {
	srv := mcpsdk.NewServer(implementation, nil)

	registerAccessoryTools(srv, svcs.Accessory)
	registerStockTools(srv, svcs.Stock)
	registerFlowTools(srv, svcs.Flow)
	registerReplenishmentTools(srv, svcs.Replenishment)
	registerFileOutboundTools(srv, svcs.Stock, svcs.Accessory)
	registerFileInboundTools(srv, svcs.Stock)

	return srv
}

// RunStdio runs the server over the process's os.Stdin / os.Stdout using
// the SDK's StdioTransport. The function blocks until ctx is cancelled or
// the transport closes (e.g. EOF on stdin).
func RunStdio(ctx context.Context, srv *mcpsdk.Server) error {
	return srv.Run(ctx, &mcpsdk.StdioTransport{})
}

// RunStdioWithIO is the same as RunStdio but reads from r and writes to w
// instead of os.Stdin/os.Stdout. It exists for stdio roundtrip tests and
// for embedders who want to wire MCP over pipes they own.
func RunStdioWithIO(ctx context.Context, srv *mcpsdk.Server, r io.ReadCloser, w io.WriteCloser) error {
	return srv.Run(ctx, &mcpsdk.IOTransport{Reader: r, Writer: w})
}

// Handler returns an http.Handler that exposes the MCP server at /mcp using
// the SDK's streamable HTTP transport. The handler does NOT bind to any
// address — callers (the Batch 9 webserver) decide where to listen. By
// default that listener must be 127.0.0.1 per the spec.
func Handler(srv *mcpsdk.Server) http.Handler {
	return mcpsdk.NewStreamableHTTPHandler(func(_ *http.Request) *mcpsdk.Server {
		return srv
	}, &mcpsdk.StreamableHTTPOptions{
		JSONResponse: true, // Easier for batch roundtrip tests; still streamable.
		Logger:       slog.Default(),
	})
}

// TranslateError maps a service-layer sentinel into (JSON-RPC code, message).
//
//	sentinel                       code       message
//	--------                       ----       -------
//	ErrNotFound                    -32004     "not found"
//	ErrNameConflict                -32005     "conflict" (incl. name)
//	ErrHasFlow                     -32005     "conflict" (has flow)
//	ErrInsufficientStock           -32005     "INSUFFICIENT_STOCK"
//	ErrInvalidInput                -32600     "invalid input"
//	(wrapped sentinels: detected   via errors.Is)
//	anything else                  -32603     err.Error() (sanitized)
//
// Errors with codes outside the JSON-RPC standard range are returned as
// JSON-RPC errors (not tool errors) so the SDK propagates the code on the
// wire; otherwise the LLM agent would only see IsError=true and no code.
//
// For nil err, TranslateError returns (0, "").
func TranslateError(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	switch {
	case errors.Is(err, service.ErrNotFound):
		return CodeNotFound, "not found"
	case errors.Is(err, service.ErrNameConflict):
		return CodeConflict, "conflict: name already exists"
	case errors.Is(err, service.ErrHasFlow):
		return CodeConflict, "conflict: accessory has inventory flows"
	case errors.Is(err, service.ErrInsufficientStock):
		// Use a recognisable tag the LLM can grep for; the full error
		// (which includes have/need numbers) is appended for context.
		return CodeConflict, codeInsufficientTag + ": " + err.Error()
	case errors.Is(err, service.ErrInvalidInput):
		return CodeBadRequest, "invalid input: " + sanitize(err)
	default:
		return CodeInternalError, sanitize(err)
	}
}

// sanitize extracts a printable message for unknown errors. It deliberately
// does not include internal stack traces; internal callers should have
// already logged the raw error.
func sanitize(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 512 {
		msg = msg[:512] + "…"
	}
	return msg
}

// rpcError converts an err into the SDK's *jsonrpc.Error when err maps to a
// non-standard JSON-RPC code; otherwise it returns nil and the caller should
// surface the error as a regular Go error (the SDK will then put it on the
// tool result with IsError=true).
//
// The two-bucket split is intentional: only the documented application
// error codes (-32004, -32005, -32600) ride as JSON-RPC errors so that
// machine consumers (other MCP clients) get a stable contract. Anything
// else falls into the tool-result path with a 32603 code in the message
// text — that's the "fallback -32603 (InternalError)" path the spec calls
// out for unknown failures.
func rpcError(err error) error {
	if err == nil {
		return nil
	}
	code, msg := TranslateError(err)
	switch code {
	case CodeNotFound, CodeConflict, CodeBadRequest:
		return &wireErrorWrapper{wire: &jsonrpc.Error{Code: int64(code), Message: msg}}
	default:
		// Wrap so the SDK's IsError path still surfaces a recognisable code.
		return fmt.Errorf("internal error (code=%d): %s", code, msg)
	}
}

// wireErrorWrapper is the type returned by every tool handler. It carries a
// *jsonrpc.Error (so the SDK propagates it as a protocol error with the
// right code) but breaks errors.Is(other *WireError) so the SDK doesn't
// mistake our -32004/-32005 errors for its reserved ErrServerClosing /
// ErrRejected sentinels.
//
// Unwrap returns the underlying wire error so jsonrpc.Error.Error() (the
// message) is preserved when callers print the error.
type wireErrorWrapper struct {
	wire *jsonrpc.Error
}

func (w *wireErrorWrapper) Error() string {
	if w == nil || w.wire == nil {
		return "mcp: unknown wire error"
	}
	return w.wire.Error()
}

func (w *wireErrorWrapper) Unwrap() error { return w.wire }

// Is matches only the wrapper itself. We deliberately do NOT delegate to
// the wrapped wire error's Is method, because that would re-introduce the
// code-collision problem the wrapper exists to avoid.
func (w *wireErrorWrapper) Is(target error) bool {
	return errors.Is(target, wireErrorWrapperSentinel)
}

// wireErrorWrapperSentinel is the canonical sentinel for errors.Is. It
// exists so callers can detect "this is one of our translated errors"
// without going through the SDK's internal type hierarchy.
var wireErrorWrapperSentinel = errors.New("mcp: wireErrorWrapper")