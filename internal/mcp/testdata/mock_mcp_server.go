// Command mock_mcp_server is a minimal MCP server for testing.
// It reads JSON-RPC requests from stdin and writes responses to stdout.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	for scanner.Scan() {
		line := scanner.Text()
		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
		}

		switch req.Method {
		case "initialize":
			resp.Result = map[string]interface{}{
				"protocolVersion": "2025-03-26",
				"capabilities":   map[string]interface{}{},
				"serverInfo": map[string]string{
					"name":    "mock-mcp-server",
					"version": "1.0.0",
				},
			}

		case "notifications/initialized":
			// No response needed for notifications, but we got one anyway.
			continue

		case "tools/list":
			resp.Result = map[string]interface{}{
				"tools": []tool{
					{
						Name:        "echo",
						Description: "Echoes the input back",
						InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
					},
					{
						Name:        "add",
						Description: "Adds two numbers",
						InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}}}`),
					},
				},
			}

		case "tools/call":
			var callParams struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(toJSON(req.Params)), &callParams); err != nil {
				resp.Error = map[string]interface{}{
					"code":    -32602,
					"message": fmt.Sprintf("invalid params: %v", err),
				}
			} else {
				switch callParams.Name {
				case "echo":
					var args struct {
						Text string `json:"text"`
					}
					json.Unmarshal(callParams.Arguments, &args)
					resp.Result = toolResult{
						Content: []toolContent{
							{Type: "text", Text: args.Text},
						},
					}
				case "add":
					var args struct {
						A float64 `json:"a"`
						B float64 `json:"b"`
					}
					json.Unmarshal(callParams.Arguments, &args)
					result := fmt.Sprintf("%.0f", args.A+args.B)
					resp.Result = toolResult{
						Content: []toolContent{
							{Type: "text", Text: result},
						},
					}
				default:
					resp.Error = map[string]interface{}{
						"code":    -32601,
						"message": fmt.Sprintf("unknown tool: %s", callParams.Name),
					}
				}
			}

		default:
			resp.Error = map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("method not found: %s", req.Method),
			}
		}

		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		writer.Write(data)
		writer.Flush()
	}
}

func toJSON(v interface{}) string {
	if v == nil {
		return "null"
	}
	data, _ := json.Marshal(v)
	return string(data)
}
