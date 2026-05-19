package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// SorobanReadonlyRuntime handles read-only Soroban contract calls via simulateTransaction.
type SorobanReadonlyRuntime struct {
	logger    *slog.Logger
	instances sync.Map
}

// NewSorobanReadonlyRuntime creates a new Soroban read-only runtime.
func NewSorobanReadonlyRuntime(logger *slog.Logger) *SorobanReadonlyRuntime {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return &SorobanReadonlyRuntime{logger: logger}
}

func (r *SorobanReadonlyRuntime) Name() string {
	return "soroban"
}

func (r *SorobanReadonlyRuntime) Spawn(ctx context.Context, spec *InstanceSpec) (Instance, error) {
	if spec.PodSpec == nil {
		return nil, fmt.Errorf("PodSpec is required")
	}

	rpcURL := spec.Env["STELLAR_RPC_URL"]
	if rpcURL == "" {
		rpcURL = os.Getenv("STELLAR_RPC_URL")
	}
	if rpcURL == "" {
		return nil, fmt.Errorf("STELLAR_RPC_URL not set")
	}

	contractID := spec.Env["TOOL_REGISTRY_CONTRACT_ID"]
	if contractID == "" {
		contractID = os.Getenv("TOOL_REGISTRY_CONTRACT_ID")
	}
	if contractID == "" {
		return nil, fmt.Errorf("TOOL_REGISTRY_CONTRACT_ID not set")
	}

	inst := &sorobanInstance{
		id:         fmt.Sprintf("soroban-%s-%d", spec.Name, time.Now().UnixNano()),
		name:       spec.Name,
		rpcURL:     rpcURL,
		contractID: contractID,
		startedAt:  time.Now(),
		client:     &http.Client{Timeout: 30 * time.Second},
		logger:     r.logger.With("instance", spec.Name, "contract", contractID),
	}
	inst.state.Store(StateRunning)
	inst.lastError.Store("")

	r.instances.Store(inst.id, inst)
	r.logger.Info("soroban instance spawned", "name", spec.Name, "contract", contractID)

	return inst, nil
}

func (r *SorobanReadonlyRuntime) Close() error {
	r.instances.Range(func(key, value any) bool {
		inst := value.(*sorobanInstance)
		inst.state.Store(StateStopped)
		return true
	})
	return nil
}

// sorobanInstance makes read-only Soroban contract calls via JSON-RPC.
// It uses the raw JSON-RPC interface rather than building XDR envelopes,
// keeping the Go dependency surface minimal (no stellar/go SDK needed).
type sorobanInstance struct {
	id         string
	name       string
	rpcURL     string
	contractID string
	startedAt  time.Time
	client     *http.Client
	logger     *slog.Logger

	state     atomic.Value
	callCount atomic.Int64
	lastError atomic.Value

	interPodHandler InterPodHandler
}

func (s *sorobanInstance) ID() string   { return s.id }
func (s *sorobanInstance) Name() string { return s.name }

func (s *sorobanInstance) SetInterPodHandler(handler InterPodHandler) {
	s.interPodHandler = handler
}

func (s *sorobanInstance) Status() InstanceStatus {
	state, _ := s.state.Load().(InstanceState)
	lastErr, _ := s.lastError.Load().(string)
	return InstanceStatus{
		ID:        s.id,
		Name:      s.name,
		State:     state,
		StartedAt: s.startedAt,
		CallCount: s.callCount.Load(),
		LastError: lastErr,
	}
}

func (s *sorobanInstance) Stop(_ context.Context) error {
	s.state.Store(StateStopped)
	return nil
}

// Call routes tool method names to Soroban contract function invocations.
// It uses the Soroban RPC getContractData or simulateTransaction endpoints.
func (s *sorobanInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	s.callCount.Add(1)

	// Parse params
	var args map[string]json.RawMessage
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			// Might be empty or non-object — that's fine for no-arg functions
			args = nil
		}
	}

	s.logger.Debug("soroban call", "method", method, "args", string(params))

	// Build the Soroban RPC request based on method name
	var result json.RawMessage
	var err error

	switch method {
	case "list_active", "list_active_full":
		result, err = s.invokeContract(ctx, method, nil)
	case "get_tool":
		var handle string
		if h, ok := args["handle"]; ok {
			json.Unmarshal(h, &handle)
		}
		if handle == "" {
			return nil, fmt.Errorf("get_tool requires 'handle' argument")
		}
		fnArgs := []sorobanArg{{Type: "symbol", Value: handle}}
		result, err = s.invokeContract(ctx, method, fnArgs)
	case "get_rep":
		var agent string
		if a, ok := args["agent"]; ok {
			json.Unmarshal(a, &agent)
		}
		if agent == "" {
			return nil, fmt.Errorf("get_rep requires 'agent' argument")
		}
		fnArgs := []sorobanArg{{Type: "address", Value: agent}}
		result, err = s.invokeContract(ctx, method, fnArgs)
	default:
		return nil, fmt.Errorf("unknown soroban method: %s", method)
	}

	if err != nil {
		s.lastError.Store(err.Error())
		return nil, err
	}

	return result, nil
}

type sorobanArg struct {
	Type  string // "symbol", "address", "string"
	Value string
}

// invokeContract calls the Soroban RPC simulateTransaction endpoint.
// We build a minimal invocation using the contract's XDR-encoded function call.
func (s *sorobanInstance) invokeContract(ctx context.Context, fnName string, args []sorobanArg) (json.RawMessage, error) {
	// Build ScVal args for the function
	scArgs := make([]map[string]any, 0, len(args))
	for _, arg := range args {
		switch arg.Type {
		case "symbol":
			scArgs = append(scArgs, map[string]any{
				"type":  "symbol",
				"value": arg.Value,
			})
		case "address":
			scArgs = append(scArgs, map[string]any{
				"type":  "address",
				"value": arg.Value,
			})
		case "string":
			scArgs = append(scArgs, map[string]any{
				"type":  "string",
				"value": arg.Value,
			})
		}
	}

	// Use the Soroban RPC "simulateTransaction" approach
	// For simplicity, we use a direct JSON-RPC call with the contract invocation
	rpcReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  "simulateTransaction",
		"params": map[string]any{
			"transaction": buildMinimalInvokeTx(s.contractID, fnName, scArgs),
		},
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("soroban rpc call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Parse the JSON-RPC response
	var rpcResp struct {
		Result struct {
			Results []struct {
				XDR string `json:"xdr"`
			} `json:"results"`
			Error string `json:"error"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error: %s", rpcResp.Error.Message)
	}

	if rpcResp.Result.Error != "" {
		return nil, fmt.Errorf("simulate error: %s", rpcResp.Result.Error)
	}

	// Return the raw result — the orchestrator's Python soroban_client
	// handles XDR decoding. The Go adapter returns the simulate response as-is.
	return respBody, nil
}

// buildMinimalInvokeTx creates a base64-encoded transaction envelope for
// simulateTransaction. This is a placeholder — a full implementation would
// use the stellar/go SDK to build proper XDR. For now, the Python client
// handles Soroban calls directly, and this Go adapter serves as the MCP-Meta
// runtime hook for future use.
func buildMinimalInvokeTx(contractID, fnName string, args []map[string]any) string {
	// TODO: Build proper XDR envelope using github.com/stellar/go SDK.
	// For Phase 1, the Python soroban_client.py handles direct Soroban calls.
	// This placeholder returns an empty string — the adapter will be completed
	// when MCP-Meta becomes the primary call path.
	return ""
}
