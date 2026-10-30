package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/happywork/happywork/internal/tool"
)

// rpcTool adapts one discovered MCP tool to tool.Tool.
type rpcTool struct {
	client *Client
	info   ToolInfo
}

func (t *rpcTool) Meta() tool.Meta {
	return tool.Meta{
		Name:        t.info.ProviderName,
		Ref:         "mcp://" + t.client.name + "/" + t.info.RawName,
		Description: t.info.Description,
		Parameters:  t.info.Schema,
		Action:      t.info.Action,
		Source:      "mcp:" + t.client.name,
		Trust:       "untrusted",
		Destructive: t.info.Destructive,
		Reversible:  false,
		RetrySafe:   false,
	}
}

// Exec implements tool.Tool. Arguments are passed through unchanged; the agent
// has already asked the user for every MCP call.
func (t *rpcTool) Exec(ctx context.Context, args json.RawMessage) (any, error) {
	var parsed map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsed); err != nil {
			return nil, fmt.Errorf("mcp: tool arguments must be an object: %w", err)
		}
	}
	return t.client.CallTool(ctx, t.info.RawName, parsed)
}

func decodeBase64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
