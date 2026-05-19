package router

import (
	"context"
	"encoding/json"
	"testing"

	"mcp-meta/internal/jsonrpc"
	"mcp-meta/internal/registry"
)

func TestVersionMatches(t *testing.T) {
	tests := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		// Exact match
		{"exact match", "1.0.0", "1.0.0", true},
		{"exact match 2", "2.3.4", "2.3.4", true},
		{"exact mismatch", "1.0.0", "1.0.1", false},

		// Caret ^
		{"caret match same", "^1.0.0", "1.0.0", false}, // Actual can't have ^
		{"caret match minor higher", "1.2.0", "^1.0.0", true},
		{"caret match patch higher", "1.0.5", "^1.0.0", true},
		{"caret match both higher", "1.5.10", "^1.0.0", true},
		{"caret mismatch major", "2.0.0", "^1.0.0", false},
		{"caret mismatch minor lower", "1.0.0", "^1.1.0", false},
		{"caret mismatch patch lower", "1.1.0", "^1.1.1", false},

		// Tilde ~
		{"tilde match same", "1.2.0", "~1.2.0", true},
		{"tilde match patch higher", "1.2.5", "~1.2.0", true},
		{"tilde mismatch minor", "1.3.0", "~1.2.0", false},
		{"tilde mismatch major", "2.2.0", "~1.2.0", false},
		{"tilde mismatch patch lower", "1.2.0", "~1.2.1", false},

		// >=
		{"gte match same", "1.0.0", ">=1.0.0", true},
		{"gte match higher patch", "1.0.5", ">=1.0.0", true},
		{"gte match higher minor", "1.5.0", ">=1.0.0", true},
		{"gte match higher major", "2.0.0", ">=1.0.0", true},
		{"gte mismatch lower", "0.9.0", ">=1.0.0", false},

		// >
		{"gt match higher patch", "1.0.1", ">1.0.0", true},
		{"gt match higher minor", "1.1.0", ">1.0.0", true},
		{"gt mismatch same", "1.0.0", ">1.0.0", false},
		{"gt mismatch lower", "0.9.9", ">1.0.0", false},

		// Edge cases
		{"empty actual", "", "1.0.0", false},
		{"empty expected exact", "1.0.0", "", false},
		{"invalid actual version", "invalid", "1.0.0", false},
		{"invalid expected version", "1.0.0", "^invalid", false},
		{"two part version", "1.0", "1.0", true},
		{"one part version", "1", "1", true},
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

func TestParseVersion(t *testing.T) {
	tests := []struct {
		version string
		want    []int
		wantNil bool
	}{
		{"1.0.0", []int{1, 0, 0}, false},
		{"2.3.4", []int{2, 3, 4}, false},
		{"10.20.30", []int{10, 20, 30}, false},
		{"1.0", []int{1, 0, 0}, false},
		{"1", []int{1, 0, 0}, false},
		{"", nil, true},
		{"a.b.c", nil, true},
		{"1.2.3.4", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := parseVersion(tt.version)
			if tt.wantNil {
				if got != nil {
					t.Errorf("parseVersion(%q) = %v, want nil", tt.version, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseVersion(%q) = nil, want %v", tt.version, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("parseVersion(%q)[%d] = %d, want %d", tt.version, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b []int
		want int
	}{
		{[]int{1, 0, 0}, []int{1, 0, 0}, 0},
		{[]int{1, 0, 0}, []int{2, 0, 0}, -1},
		{[]int{2, 0, 0}, []int{1, 0, 0}, 1},
		{[]int{1, 1, 0}, []int{1, 0, 0}, 1},
		{[]int{1, 0, 1}, []int{1, 0, 0}, 1},
		{[]int{1, 0, 0}, []int{1, 1, 0}, -1},
		{[]int{1, 0, 0}, []int{1, 0, 1}, -1},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareVersions(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCallStack(t *testing.T) {
	ctx := context.Background()

	// Empty stack
	stack := GetCallStack(ctx)
	if stack != nil {
		t.Errorf("GetCallStack() on fresh context = %v, want nil", stack)
	}

	// Add to stack
	ctx = WithCallStack(ctx, []string{"pod1"})
	stack = GetCallStack(ctx)
	if len(stack) != 1 || stack[0] != "pod1" {
		t.Errorf("GetCallStack() = %v, want [pod1]", stack)
	}

	// Add more
	ctx = WithCallStack(ctx, []string{"pod1", "pod2"})
	stack = GetCallStack(ctx)
	if len(stack) != 2 {
		t.Errorf("GetCallStack() = %v, want [pod1, pod2]", stack)
	}
}

func TestDetectCircular(t *testing.T) {
	tests := []struct {
		name    string
		stack   []string
		podName string
		want    bool
	}{
		{"empty stack", nil, "pod1", false},
		{"not in stack", []string{"pod1", "pod2"}, "pod3", false},
		{"in stack", []string{"pod1", "pod2"}, "pod1", true},
		{"self reference", []string{"pod1"}, "pod1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCircular(tt.stack, tt.podName)
			if got != tt.want {
				t.Errorf("detectCircular(%v, %q) = %v, want %v", tt.stack, tt.podName, got, tt.want)
			}
		})
	}
}

func TestVerifyContract(t *testing.T) {
	tests := []struct {
		name    string
		tool    *registry.ToolDef
		expect  *jsonrpc.ContractExpectation
		wantErr bool
	}{
		{
			name:    "nil expectation",
			tool:    &registry.ToolDef{Name: "test"},
			expect:  nil,
			wantErr: false,
		},
		{
			name:    "no contract, no expectation",
			tool:    &registry.ToolDef{Name: "test"},
			expect:  &jsonrpc.ContractExpectation{},
			wantErr: false,
		},
		{
			name: "no contract but expected ID",
			tool: &registry.ToolDef{Name: "test"},
			expect: &jsonrpc.ContractExpectation{
				ID: "expected-id",
			},
			wantErr: true,
		},
		{
			name: "matching contract ID",
			tool: &registry.ToolDef{
				Name:     "test",
				Contract: &registry.ToolContract{ID: "test-id"},
			},
			expect: &jsonrpc.ContractExpectation{
				ID: "test-id",
			},
			wantErr: false,
		},
		{
			name: "mismatching contract ID",
			tool: &registry.ToolDef{
				Name:     "test",
				Contract: &registry.ToolContract{ID: "actual-id"},
			},
			expect: &jsonrpc.ContractExpectation{
				ID: "expected-id",
			},
			wantErr: true,
		},
		{
			name: "matching schema hash",
			tool: &registry.ToolDef{
				Name:     "test",
				Contract: &registry.ToolContract{SchemaHash: "sha256:abc123"},
			},
			expect: &jsonrpc.ContractExpectation{
				SchemaHash: "sha256:abc123",
			},
			wantErr: false,
		},
		{
			name: "mismatching schema hash",
			tool: &registry.ToolDef{
				Name:     "test",
				Contract: &registry.ToolContract{SchemaHash: "sha256:abc123"},
			},
			expect: &jsonrpc.ContractExpectation{
				SchemaHash: "sha256:different",
			},
			wantErr: true,
		},
		{
			name: "matching version exact",
			tool: &registry.ToolDef{
				Name:     "test",
				Contract: &registry.ToolContract{Version: "1.0.0"},
			},
			expect: &jsonrpc.ContractExpectation{
				Version: "1.0.0",
			},
			wantErr: false,
		},
		{
			name: "matching version caret",
			tool: &registry.ToolDef{
				Name:     "test",
				Contract: &registry.ToolContract{Version: "1.2.3"},
			},
			expect: &jsonrpc.ContractExpectation{
				Version: "^1.0.0",
			},
			wantErr: false,
		},
		{
			name: "mismatching version",
			tool: &registry.ToolDef{
				Name:     "test",
				Contract: &registry.ToolContract{Version: "1.0.0"},
			},
			expect: &jsonrpc.ContractExpectation{
				Version: "2.0.0",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyContract(tt.tool, tt.expect)
			if (err != nil) != tt.wantErr {
				t.Errorf("verifyContract() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildContractMismatchData(t *testing.T) {
	// With actual contract
	actual := &registry.ToolContract{ID: "test", Version: "1.0.0"}
	expect := &jsonrpc.ContractExpectation{ID: "expected", Version: "2.0.0"}

	data := buildContractMismatchData(actual, expect)
	if data == nil {
		t.Fatal("buildContractMismatchData() returned nil")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if _, ok := parsed["expected"]; !ok {
		t.Error("Result missing 'expected' field")
	}
	if _, ok := parsed["actual"]; !ok {
		t.Error("Result missing 'actual' field")
	}

	// Without actual contract
	data = buildContractMismatchData(nil, expect)
	var parsedWithoutActual map[string]interface{}
	if err := json.Unmarshal(data, &parsedWithoutActual); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if _, ok := parsedWithoutActual["actual"]; ok {
		t.Error("Result should not have 'actual' field when actual is nil")
	}
}

func TestRouter_WarmPoolManager(t *testing.T) {
	// Create router without warm pool manager
	r := New(nil, nil, nil)

	// Verify no warm pool manager initially
	if r.GetWarmPoolManager() != nil {
		t.Error("GetWarmPoolManager() should return nil initially")
	}

	// Verify GetWarmPoolStatus returns nil when no manager
	if r.GetWarmPoolStatus() != nil {
		t.Error("GetWarmPoolStatus() should return nil when no manager")
	}

	// SetWarmPoolManager with nil should not panic
	r.SetWarmPoolManager(nil)
	if r.GetWarmPoolManager() != nil {
		t.Error("GetWarmPoolManager() should still return nil")
	}
}

func TestNewWithWarmPool(t *testing.T) {
	// Create with nil warm pool manager
	r := NewWithWarmPool(nil, nil, nil, nil)
	if r == nil {
		t.Fatal("NewWithWarmPool() returned nil")
	}

	if r.GetWarmPoolManager() != nil {
		t.Error("GetWarmPoolManager() should return nil when created with nil")
	}
}
