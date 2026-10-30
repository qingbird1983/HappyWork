package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const (
	modernVersion = "2026-07-28"
	legacyVersion = "2025-11-25"

	maxLineBytes   = 8 << 20
	maxTools       = 200
	maxPages       = 20
	maxTextResult  = 256 << 10
	maxStructured  = 64 << 10
	maxRestarts    = 3
	restartWindow  = 10 * time.Minute
	closeGrace     = 2 * time.Second
	probeTimeout   = 5 * time.Second
	startupTimeout = 10 * time.Second
)

var legacyVersions = map[string]bool{
	"2025-11-25": true, "2025-06-18": true, "2025-03-26": true, "2024-11-05": true,
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	msg := e.Message
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return fmt.Sprintf("mcp: rpc error %d: %s", e.Code, msg)
}

type inbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// ToolInfo is one discovered tool.
type ToolInfo struct {
	RawName      string
	ProviderName string
	Description  string
	Schema       json.RawMessage
	Action       string
	Destructive  bool
}

func idMatches(raw json.RawMessage, id int64) bool {
	if len(raw) == 0 {
		return false
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return false
	}
	return n.String() == strconv.FormatInt(id, 10)
}

func isMethodNotFound(err error) bool {
	var re *rpcError
	if !errorsAs(err, &re) {
		return false
	}
	return re.Code == -32601
}

// errorsAs keeps the import list small; stdlib errors.As is equivalent.
func errorsAs(err error, target **rpcError) bool {
	for err != nil {
		if e, ok := err.(*rpcError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
