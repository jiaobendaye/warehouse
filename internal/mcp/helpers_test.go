package mcp_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// testCallToolParams is a tiny builder so the call sites read clearly.
// We intentionally use the SDK type by constructing it in toParams.
type testCallToolParams struct {
	Name      string
	Arguments map[string]any
}

func (p testCallToolParams) toParams() *mcp.CallToolParams {
	return &mcp.CallToolParams{
		Name:      p.Name,
		Arguments: p.Arguments,
	}
}

// newInMemoryClient dials an SDK InMemoryTransport against the given server
// and returns both the *mcp.Client and the *mcp.ClientSession so the caller
// can make assertions on the client side.
func newInMemoryClient(t *testing.T, srv *mcp.Server) (*mcp.Client, *mcp.ClientSession) {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), st, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	return client, session
}

// readLine reads a single newline-delimited JSON message from r.
func readLine(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if err == io.EOF && len(line) > 0 {
				return bytes.TrimRight(line, "\r\n"), nil
			}
			return nil, err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		return bytes.TrimRight(line, "\r\n"), nil
	}
}

// decodedAccessory is the slim shape we read back from accessory.create /
// accessory.get tool calls. Kept here so multiple tests share it.
type decodedAccessory struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	LowStockThreshold int64  `json:"low_stock_threshold"`
}

// createAccessoryViaMCP issues an accessory.create tool call and decodes the
// structured content.
func createAccessoryViaMCP(t *testing.T, session *mcp.ClientSession, name string, threshold int64) decodedAccessory {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "accessory.create",
		Arguments: map[string]any{
			"name":                name,
			"low_stock_threshold": threshold,
		},
	})
	if err != nil {
		t.Fatalf("accessory.create %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("accessory.create %s IsError: %+v", name, res)
	}
	var out decodedAccessory
	if err := decodeStructured(res.StructuredContent, &out); err != nil {
		t.Fatalf("decode accessory.create %s: %v", name, err)
	}
	if out.ID == 0 || out.Name != name {
		t.Fatalf("unexpected create result for %s: %+v", name, out)
	}
	return out
}

// decodeStructured unmarshals the SDK's CallToolResult.StructuredContent
// into v. The SDK stores the JSON value as either json.RawMessage or as
// the typed output value depending on the call path; we normalise both
// into a JSON decode here so tests don't need to type-assert.
func decodeStructured(raw any, v any) error {
	switch x := raw.(type) {
	case json.RawMessage:
		return json.Unmarshal(x, v)
	case []byte:
		return json.Unmarshal(x, v)
	case string:
		return json.Unmarshal([]byte(x), v)
	default:
		// Fall back to round-trip marshalling — works when the SDK
		// stored the typed value directly.
		b, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, v)
	}
}