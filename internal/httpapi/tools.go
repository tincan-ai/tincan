// Package httpapi contains public MCP schemas only; no hosted server implementation.
package httpapi

import (
	_ "embed"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed tools.json
var schemas []byte

func MCPTools() []*mcp.Tool {
	var tools []*mcp.Tool
	if err := json.Unmarshal(schemas, &tools); err != nil {
		panic(err)
	}
	return tools
}
