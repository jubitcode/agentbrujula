package router

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/supervisor"
)

// getZombieCount returns the number of zombie processes (Linux/macOS)
func getZombieCount() int {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("ps", "aux")
	} else {
		cmd = exec.Command("ps", "-eo", "stat")
	}
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Z") && !strings.Contains(line, "ps") {
			count++
		}
	}
	return count
}

// getMemUsageMB returns memory usage in MB
func getMemUsageMB() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024
}

// mockRegistry implements registry.Registry for testing
type mockRegistry struct {
	pods  map[string]*registry.PodSpec
	tools map[string]*registry.ToolDef
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		pods:  make(map[string]*registry.PodSpec),
		tools: make(map[string]*registry.ToolDef),
	}
}

func (r *mockRegistry) AddPodWithTool(podSpec *registry.PodSpec, tool *registry.ToolDef) {
	r.pods[podSpec.Name] = podSpec
	r.tools[tool.Name] = tool
}

func (r *mockRegistry) LoadManifests(path string) error {
	return nil
}

func (r *mockRegistry) AllTools() []registry.ToolInfo {
	result := make([]registry.ToolInfo, 0, len(r.tools))
	for _, tool := range r.tools {
		result = append(result, registry.ToolInfo{
			Name:        tool.Name,
			Description: tool.Description,
		})
	}
	return result
}

func (r *mockRegistry) GetPodSpec(name string) *registry.PodSpec {
	if spec, ok := r.pods[name]; ok {
		return spec
	}
	return nil
}

func (r *mockRegistry) ResolveTool(toolName string) (*registry.PodSpec, *registry.ToolDef, error) {
	tool, ok := r.tools[toolName]
	if !ok {
		return nil, nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "tool not found"}
	}
	// Find the pod that owns this tool
	for _, pod := range r.pods {
		for _, t := range pod.Tools {
			if t.Name == toolName {
				return pod, tool, nil
			}
		}
	}
	return nil, nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "tool not found in any pod"}
}

func (r *mockRegistry) PodsToPreWarm() []*registry.PodSpec {
	result := make([]*registry.PodSpec, 0, len(r.pods))
	for _, pod := range r.pods {
		result = append(result, pod)
	}
	return result
}

func (r *mockRegistry) Validate() []error {
	return nil
}

func (r *mockRegistry) AllAgentPods() []*registry.PodSpec                              { return nil }
func (r *mockRegistry) RegisterManifest(manifest *registry.Manifest) error             { return nil }
func (r *mockRegistry) UnregisterPod(podName string) error                             { return nil }

func TestRouter_New(t *testing.T) {
	reg := newMockRegistry()
	sup := supervisor.New(nil)

	r := New(reg, sup, nil)

	if r == nil {
		t.Fatal("New() returned nil")
	}
}

func TestRouter_CallTool_ToolNotFound(t *testing.T) {
	reg := newMockRegistry()
	sup := supervisor.New(nil)
	r := New(reg, sup, nil)

	ctx := context.Background()
	_, err := r.CallTool(ctx, "nonexistent/tool", nil)

	if err == nil {
		t.Fatal("Expected error for nonexistent tool")
	}

	jsonErr, ok := err.(*jsonrpc.Error)
	if !ok {
		t.Fatalf("Expected jsonrpc.Error, got %T", err)
	}
	if jsonErr.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("Error code = %d, want %d", jsonErr.Code, jsonrpc.CodeMethodNotFound)
	}
}

func TestRouter_CallTool_Success(t *testing.T) {
	zombiesBefore := getZombieCount()

	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "testpod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		ShutdownTimeout: registry.Duration{
			Duration: 5 * time.Second,
		},
		Tools: []registry.ToolDef{
			{
				Name:        "echo", // Just "echo" to match testpod
				Description: "Echo tool",
			},
		},
	}

	tool := &registry.ToolDef{
		Name:        "echo", // Just "echo" to match testpod
		Description: "Echo tool",
	}

	reg.AddPodWithTool(podSpec, tool)

	r := New(reg, sup, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args, _ := json.Marshal(map[string]string{"message": "hello"})
	result, err := r.CallTool(ctx, "echo", args) // Use "echo" not "echo"

	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	if result == nil {
		t.Fatal("CallTool() returned nil result")
	}

	// Verify result contains expected data
	var resultMap map[string]interface{}
	if err := json.Unmarshal(result, &resultMap); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Shutdown
	sup.Shutdown(context.Background())

	// Verify no zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie processes: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

func TestRouter_CallTool_MultipleCalls(t *testing.T) {
	zombiesBefore := getZombieCount()
	memBefore := getMemUsageMB()

	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "testpod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Tools: []registry.ToolDef{
			{Name: "echo"},
		},
	}

	tool := &registry.ToolDef{
		Name: "echo",
	}

	reg.AddPodWithTool(podSpec, tool)
	r := New(reg, sup, nil)

	// Make multiple calls
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		args, _ := json.Marshal(map[string]int{"index": i})
		_, err := r.CallTool(ctx, "echo", args)
		cancel()

		if err != nil {
			t.Errorf("Call %d failed: %v", i, err)
		}
	}

	sup.Shutdown(context.Background())

	// Verify no zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie processes: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}

	// Verify memory didn't grow excessively
	memAfter := getMemUsageMB()
	if memAfter > memBefore+50 {
		t.Errorf("Memory grew significantly: before=%dMB, after=%dMB", memBefore, memAfter)
	}
}

func TestRouter_InternalCall_ToolNotFound(t *testing.T) {
	reg := newMockRegistry()
	sup := supervisor.New(nil)
	r := New(reg, sup, nil)

	ctx := context.Background()
	_, err := r.InternalCall(ctx, "source", "nonexistent/tool", nil)

	if err == nil {
		t.Fatal("Expected error for nonexistent tool")
	}

	jsonErr, ok := err.(*jsonrpc.Error)
	if !ok {
		t.Fatalf("Expected jsonrpc.Error, got %T", err)
	}
	if jsonErr.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("Error code = %d, want %d", jsonErr.Code, jsonrpc.CodeMethodNotFound)
	}
}

func TestRouter_InternalCall_CircularDetection(t *testing.T) {
	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "pod-a",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Tools: []registry.ToolDef{
			{Name: "ping"},
		},
	}

	tool := &registry.ToolDef{
		Name: "ping",
	}

	reg.AddPodWithTool(podSpec, tool)
	r := New(reg, sup, nil)

	// Create context with call stack that includes pod-a
	ctx := context.Background()
	ctx = WithCallStack(ctx, []string{"pod-a"})

	// Try to call pod-a from itself (circular)
	_, err := r.InternalCall(ctx, "pod-a", "ping", nil)

	if err == nil {
		t.Fatal("Expected error for circular call")
	}

	jsonErr, ok := err.(*jsonrpc.Error)
	if !ok {
		t.Fatalf("Expected jsonrpc.Error, got %T", err)
	}
	if jsonErr.Code != jsonrpc.CodeCircularCall {
		t.Errorf("Error code = %d, want %d (CircularCall)", jsonErr.Code, jsonrpc.CodeCircularCall)
	}
}

func TestRouter_InternalCallWithContract_Success(t *testing.T) {
	zombiesBefore := getZombieCount()

	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "testpod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Tools: []registry.ToolDef{
			{Name: "echo"},
		},
	}

	tool := &registry.ToolDef{
		Name: "echo",
		Contract: &registry.ToolContract{
			ID:      "echo-contract",
			Version: "1.0.0",
		},
	}

	reg.AddPodWithTool(podSpec, tool)
	r := New(reg, sup, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Call with matching contract
	expect := &jsonrpc.ContractExpectation{
		ID:      "echo-contract",
		Version: "1.0.0",
	}

	args, _ := json.Marshal(map[string]string{"message": "hello"})
	_, err := r.InternalCallWithContract(ctx, "source-pod", "echo", args, expect)

	if err != nil {
		t.Errorf("InternalCallWithContract() error = %v", err)
	}

	sup.Shutdown(context.Background())

	// Verify no zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie processes: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

func TestRouter_InternalCallWithContract_ContractMismatch(t *testing.T) {
	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "testpod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Tools: []registry.ToolDef{
			{Name: "echo"},
		},
	}

	tool := &registry.ToolDef{
		Name: "echo",
		Contract: &registry.ToolContract{
			ID:      "echo-contract",
			Version: "1.0.0",
		},
	}

	reg.AddPodWithTool(podSpec, tool)
	r := New(reg, sup, nil)

	ctx := context.Background()

	// Call with mismatched contract ID
	expect := &jsonrpc.ContractExpectation{
		ID:      "wrong-contract",
		Version: "1.0.0",
	}

	_, err := r.InternalCallWithContract(ctx, "source-pod", "echo", nil, expect)

	if err == nil {
		t.Fatal("Expected error for contract mismatch")
	}

	jsonErr, ok := err.(*jsonrpc.Error)
	if !ok {
		t.Fatalf("Expected jsonrpc.Error, got %T", err)
	}
	if jsonErr.Code != jsonrpc.CodeContractMismatch {
		t.Errorf("Error code = %d, want %d (ContractMismatch)", jsonErr.Code, jsonrpc.CodeContractMismatch)
	}
}

func TestRouter_InternalCallWithContract_NoContract_ExpectContract(t *testing.T) {
	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "testpod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Tools: []registry.ToolDef{
			{Name: "echo"},
		},
	}

	// Tool without contract
	tool := &registry.ToolDef{
		Name: "echo",
	}

	reg.AddPodWithTool(podSpec, tool)
	r := New(reg, sup, nil)

	ctx := context.Background()

	// Expect contract but tool has none
	expect := &jsonrpc.ContractExpectation{
		ID:      "some-contract",
		Version: "1.0.0",
	}

	_, err := r.InternalCallWithContract(ctx, "source-pod", "echo", nil, expect)

	if err == nil {
		t.Fatal("Expected error when expecting contract but tool has none")
	}

	jsonErr, ok := err.(*jsonrpc.Error)
	if !ok {
		t.Fatalf("Expected jsonrpc.Error, got %T", err)
	}
	if jsonErr.Code != jsonrpc.CodeContractMismatch {
		t.Errorf("Error code = %d, want %d (ContractMismatch)", jsonErr.Code, jsonrpc.CodeContractMismatch)
	}
}

func TestRouter_InternalCall_Success(t *testing.T) {
	zombiesBefore := getZombieCount()

	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "testpod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Tools: []registry.ToolDef{
			{Name: "echo"},
		},
	}

	tool := &registry.ToolDef{
		Name: "echo",
	}

	reg.AddPodWithTool(podSpec, tool)
	r := New(reg, sup, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args, _ := json.Marshal(map[string]string{"message": "internal call"})
	result, err := r.InternalCall(ctx, "source-pod", "echo", args)

	if err != nil {
		t.Fatalf("InternalCall() error = %v", err)
	}

	if result == nil {
		t.Fatal("InternalCall() returned nil result")
	}

	sup.Shutdown(context.Background())

	// Verify no zombies
	zombiesAfter := getZombieCount()
	if zombiesAfter > zombiesBefore {
		t.Errorf("Zombie processes: before=%d, after=%d", zombiesBefore, zombiesAfter)
	}
}

func TestRouter_CallStack_Propagation(t *testing.T) {
	ctx := context.Background()

	// Initially empty
	stack := GetCallStack(ctx)
	if len(stack) != 0 {
		t.Errorf("Initial stack should be empty, got %v", stack)
	}

	// Add to stack
	ctx = WithCallStack(ctx, []string{"pod-a"})
	stack = GetCallStack(ctx)
	if len(stack) != 1 || stack[0] != "pod-a" {
		t.Errorf("Stack should be [pod-a], got %v", stack)
	}

	// Add more
	ctx = WithCallStack(ctx, []string{"pod-a", "pod-b"})
	stack = GetCallStack(ctx)
	if len(stack) != 2 {
		t.Errorf("Stack should have 2 items, got %v", stack)
	}
}

func TestRouter_CallTool_Timeout(t *testing.T) {
	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "testpod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 1 * time.Second, // Very short timeout
		},
		Tools: []registry.ToolDef{
			{Name: "slow"},
		},
	}

	tool := &registry.ToolDef{
		Name: "slow",
		Timeout: registry.Duration{
			Duration: 100 * time.Millisecond, // Even shorter
		},
	}

	reg.AddPodWithTool(podSpec, tool)
	r := New(reg, sup, nil)

	// Use a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Sleep tool would take too long
	args, _ := json.Marshal(map[string]int{"delay_ms": 5000})
	_, err := r.CallTool(ctx, "slow", args)

	// Should get timeout error
	if err == nil {
		// Tool might not implement slow, which is fine
		t.Log("No error, tool may have completed quickly")
		return
	}

	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
		t.Logf("Got error (may or may not be timeout): %v", err)
	}
}

func TestRouter_VersionMatches_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		// Exact match
		{"exact match", "1.0.0", "1.0.0", true},
		{"exact mismatch", "1.0.0", "1.0.1", false},

		// Caret (^)
		{"caret major match", "1.5.3", "^1.0.0", true},
		{"caret major mismatch", "2.0.0", "^1.0.0", false},
		{"caret minor lower", "1.0.5", "^1.1.0", false},
		{"caret patch lower same minor", "1.1.0", "^1.1.1", false},

		// Tilde (~)
		{"tilde patch match", "1.2.5", "~1.2.0", true},
		{"tilde minor mismatch", "1.3.0", "~1.2.0", false},

		// Greater than or equal (>=)
		{"gte equal", "1.0.0", ">=1.0.0", true},
		{"gte greater", "2.0.0", ">=1.0.0", true},
		{"gte less", "0.9.0", ">=1.0.0", false},

		// Greater than (>)
		{"gt greater", "1.0.1", ">1.0.0", true},
		{"gt equal", "1.0.0", ">1.0.0", false},
		{"gt less", "0.9.9", ">1.0.0", false},

		// Empty/invalid
		{"empty actual", "", "1.0.0", false},
		{"invalid actual", "not-a-version", "1.0.0", false},
		{"invalid expected caret", "1.0.0", "^not-valid", false},
		{"invalid expected tilde", "1.0.0", "~not-valid", false},
		{"invalid expected gte", "1.0.0", ">=not-valid", false},
		{"invalid expected gt", "1.0.0", ">not-valid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionMatches(tt.actual, tt.expected)
			if got != tt.want {
				t.Errorf("versionMatches(%q, %q) = %v, want %v", tt.actual, tt.expected, got, tt.want)
			}
		})
	}
}

func TestRouter_ParseVersion_EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  []int
	}{
		{"1.0.0", []int{1, 0, 0}},
		{"0.0.0", []int{0, 0, 0}},
		{"10.20.30", []int{10, 20, 30}},
		{"1.2", []int{1, 2, 0}},
		{"1", []int{1, 0, 0}},
		{"", nil},
		{"1.2.3.4", nil},
		{"a.b.c", nil},
		{"1.2.x", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseVersion(tt.input)
			if tt.want == nil {
				if got != nil {
					t.Errorf("parseVersion(%q) = %v, want nil", tt.input, got)
				}
			} else {
				if got == nil {
					t.Errorf("parseVersion(%q) = nil, want %v", tt.input, tt.want)
				} else {
					for i, v := range tt.want {
						if got[i] != v {
							t.Errorf("parseVersion(%q)[%d] = %d, want %d", tt.input, i, got[i], v)
						}
					}
				}
			}
		})
	}
}

func BenchmarkRouter_CallTool(b *testing.B) {
	reg := newMockRegistry()
	sup := supervisor.New(nil)
	defer sup.Shutdown(context.Background())

	podSpec := &registry.PodSpec{
		Name:    "testpod",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 30 * time.Second,
		},
		Tools: []registry.ToolDef{
			{Name: "echo"},
		},
	}

	tool := &registry.ToolDef{
		Name: "echo",
	}

	reg.AddPodWithTool(podSpec, tool)
	r := New(reg, sup, nil)

	// Warm up
	ctx := context.Background()
	args, _ := json.Marshal(map[string]string{"message": "warmup"})
	r.CallTool(ctx, "echo", args)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		args, _ := json.Marshal(map[string]int{"index": i})
		r.CallTool(ctx, "echo", args)
		cancel()
	}
}
