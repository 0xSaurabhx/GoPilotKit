//go:build ignore

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

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		var resp jsonRPCResponse
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "initialize":
			resp.Result = map[string]interface{}{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]interface{}{},
				"serverInfo":      map[string]string{"name": "test-wasm", "version": "1.0.0"},
			}
		case "tools/list":
			resp.Result = map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "echo",
						"description": "Echoes the input back",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"message": map[string]string{"type": "string"},
							},
						},
					},
					{
						"name":        "add",
						"description": "Adds two numbers",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"a": map[string]string{"type": "number"},
								"b": map[string]string{"type": "number"},
							},
						},
					},
				},
			}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			paramsData, _ := json.Marshal(req.Params)
			json.Unmarshal(paramsData, &params)

			switch params.Name {
			case "echo":
				var args struct {
					Message string `json:"message"`
				}
				json.Unmarshal(params.Arguments, &args)
				resp.Result = map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": fmt.Sprintf("Echo: %s", args.Message)},
					},
				}
			case "add":
				var args struct {
					A float64 `json:"a"`
					B float64 `json:"b"`
				}
				json.Unmarshal(params.Arguments, &args)
				resp.Result = map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": fmt.Sprintf("%.0f", args.A+args.B)},
					},
				}
			default:
				resp.Error = map[string]interface{}{
					"code":    -32601,
					"message": fmt.Sprintf("unknown tool: %s", params.Name),
				}
			}
		case "notifications/initialized":
			continue
		default:
			resp.Error = map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("method not found: %s", req.Method),
			}
		}

		data, _ := json.Marshal(resp)
		fmt.Println(string(data))
	}
}
