package main

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var podID = os.Getenv("POD_REPLICA_ID")

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		var resp Response
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "ping":
			resp.Result = map[string]string{"status": "ok", "replica": podID}

		case "echo":
			var params map[string]interface{}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				params = make(map[string]interface{})
			}
			params["handled_by"] = podID
			resp.Result = params

		case "slow":
			time.Sleep(100 * time.Millisecond)
			resp.Result = map[string]string{"status": "ok", "replica": podID}

		case "very_slow":
			time.Sleep(500 * time.Millisecond)
			resp.Result = map[string]string{"status": "ok", "replica": podID}

		case "slow_call":
			// Configurable slow call for auto-scaler testing
			duration := 2 * time.Second
			var params map[string]interface{}
			if err := json.Unmarshal(req.Params, &params); err == nil {
				if d, ok := params["duration_ms"].(float64); ok {
					duration = time.Duration(d) * time.Millisecond
				}
			}
			time.Sleep(duration)
			resp.Result = map[string]interface{}{
				"status":      "ok",
				"duration_ms": duration.Milliseconds(),
				"replica":     podID,
			}

		case "crash":
			os.Exit(1)

		case "shutdown":
			resp.Result = map[string]string{"status": "ok"}
			json.NewEncoder(os.Stdout).Encode(resp)
			os.Exit(0)

		case "call_external":
			// Trigger an inter-pod call back to the host
			var params map[string]interface{}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				params = map[string]interface{}{"tool": "test_tool"}
			}
			tool, _ := params["tool"].(string)
			if tool == "" {
				tool = "test_tool"
			}
			args, _ := json.Marshal(params["args"])

			interPodReq := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID * 1000,
				"method":  "__mcp_meta_call__",
				"params": map[string]interface{}{
					"tool": tool,
					"args": json.RawMessage(args),
				},
			}
			json.NewEncoder(os.Stdout).Encode(interPodReq)
			resp.Result = map[string]interface{}{
				"status":         "inter_pod_call_sent",
				"requested_tool": tool,
			}

		case "call_unknown_interpod":
			// Trigger an inter-pod call with unknown method
			unknownReq := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID * 1000,
				"method":  "unknown_interpod_method",
				"params":  map[string]interface{}{},
			}
			json.NewEncoder(os.Stdout).Encode(unknownReq)
			resp.Result = map[string]string{"status": "unknown_method_sent"}

		default:
			resp.Error = &Error{Code: -32601, Message: "method not found: " + req.Method}
		}

		json.NewEncoder(os.Stdout).Encode(resp)
	}
}
