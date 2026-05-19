package registry

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    func(*Manifest) bool
		wantErr bool
	}{
		{
			name: "minimal valid manifest",
			yaml: `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: test-pod
spec:
  command: ["python", "main.py"]
  tools:
    - name: hello
      description: Says hello
`,
			want: func(m *Manifest) bool {
				return m.APIVersion == "mcp-meta/v1" &&
					m.Kind == "ToolPod" &&
					m.Metadata.Name == "test-pod" &&
					len(m.Spec.Command) == 2 &&
					m.Spec.Command[0] == "python" &&
					len(m.Spec.Tools) == 1 &&
					m.Spec.Tools[0].Name == "hello"
			},
		},
		{
			name: "manifest with marketplace fields",
			yaml: `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: linter
  namespace: acme
  version: "1.0.0"
  description: Python linter tool
  license: MIT
  homepage: https://example.com
  keywords:
    - python
    - linting
  authors:
    - name: John Doe
      email: john@example.com
spec:
  command: ["python", "main.py"]
  tools:
    - name: lint
      description: Lint code
marketplace:
  visibility: public
  categories:
    - utilities
    - code-quality
  pricing:
    model: per-call
    price:
      amount: "0.001"
      currency: USD
    freeQuota: 100
`,
			want: func(m *Manifest) bool {
				return m.Metadata.Namespace == "acme" &&
					m.Metadata.Version == "1.0.0" &&
					m.Metadata.License == "MIT" &&
					len(m.Metadata.Keywords) == 2 &&
					len(m.Metadata.Authors) == 1 &&
					m.Metadata.Authors[0].Name == "John Doe" &&
					m.Marketplace != nil &&
					m.Marketplace.Visibility == "public" &&
					m.Marketplace.Pricing.Model == "per-call" &&
					m.Marketplace.Pricing.Price.Amount == "0.001" &&
					m.Marketplace.Pricing.FreeQuota == 100
			},
		},
		{
			name: "manifest with build and resources",
			yaml: `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: worker
spec:
  command: ["go", "run", "."]
  build:
    runtime: go:1.22
    context: ./src
  resources:
    cpu: "0.5"
    memory: 256Mi
    timeout: 5m
  tools:
    - name: process
      description: Process data
`,
			want: func(m *Manifest) bool {
				return m.Spec.Build != nil &&
					m.Spec.Build.Runtime == "go:1.22" &&
					m.Spec.Build.Context == "./src" &&
					m.Spec.Resources != nil &&
					m.Spec.Resources.CPU == "0.5" &&
					m.Spec.Resources.Memory == "256Mi" &&
					m.Spec.Resources.Timeout.Duration == 5*time.Minute
			},
		},
		{
			name: "manifest with scaling",
			yaml: `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: scaled-worker
spec:
  command: ["node", "index.js"]
  scaling:
    minReplicas: 2
    maxReplicas: 10
    targetPending: 5
    scaleDownDelay: 30s
  tools:
    - name: work
      description: Do work
`,
			want: func(m *Manifest) bool {
				return m.Spec.Scaling != nil &&
					m.Spec.Scaling.MinReplicas == 2 &&
					m.Spec.Scaling.MaxReplicas == 10 &&
					m.Spec.Scaling.TargetPending == 5 &&
					m.Spec.Scaling.ScaleDownDelay.Duration == 30*time.Second
			},
		},
		{
			name: "manifest with tool contract",
			yaml: `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: contract-pod
spec:
  command: ["python", "main.py"]
  tools:
    - name: greet
      description: Greet user
      contract:
        id: greet-v1
        version: "1.0"
      inputSchema:
        type: object
        properties:
          name:
            type: string
        required:
          - name
`,
			want: func(m *Manifest) bool {
				if len(m.Spec.Tools) != 1 {
					return false
				}
				tool := m.Spec.Tools[0]
				return tool.Contract != nil &&
					tool.Contract.ID == "greet-v1" &&
					tool.Contract.Version == "1.0" &&
					tool.Contract.SchemaHash != "" && // Should be auto-computed
					len(tool.InputSchema) > 0
			},
		},
		{
			name: "manifest with pre-built image",
			yaml: `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: prebuilt
spec:
  image: ghcr.io/acme/tool:v1
  tools:
    - name: run
      description: Run tool
`,
			want: func(m *Manifest) bool {
				return m.Spec.Image == "ghcr.io/acme/tool:v1"
			},
		},
		{
			name: "invalid yaml",
			yaml: `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata: [invalid
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseManifest([]byte(tt.yaml))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseManifest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil && !tt.want(got) {
				t.Errorf("ParseManifest() returned unexpected result")
			}
		})
	}
}

func TestParseManifest_AppliesDefaults(t *testing.T) {
	yaml := `
apiVersion: mcp-meta/v1
kind: ToolPod
metadata:
  name: defaults-test
spec:
  command: ["echo", "hello"]
  tools: []
`
	m, err := ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	if m.Spec.StartupTimeout.Duration != DefaultStartupTimeout {
		t.Errorf("StartupTimeout = %v, want %v", m.Spec.StartupTimeout.Duration, DefaultStartupTimeout)
	}
	if m.Spec.CallTimeout.Duration != DefaultCallTimeout {
		t.Errorf("CallTimeout = %v, want %v", m.Spec.CallTimeout.Duration, DefaultCallTimeout)
	}
	if m.Spec.ShutdownTimeout.Duration != DefaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", m.Spec.ShutdownTimeout.Duration, DefaultShutdownTimeout)
	}
}

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name       string
		manifest   *Manifest
		wantErrors int
	}{
		{
			name: "valid manifest",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec: PodSpec{
					Command: []string{"python", "main.py"},
					Tools:   []ToolDef{{Name: "tool1"}},
				},
			},
			wantErrors: 0,
		},
		{
			name: "invalid apiVersion",
			manifest: &Manifest{
				APIVersion: "invalid/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{Command: []string{"echo"}},
			},
			wantErrors: 1,
		},
		{
			name: "invalid kind",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "InvalidKind",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{Command: []string{"echo"}},
			},
			wantErrors: 1,
		},
		{
			name: "missing name",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{},
				Spec:       PodSpec{Command: []string{"echo"}},
			},
			wantErrors: 1,
		},
		{
			name: "missing command and image",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{},
			},
			wantErrors: 1,
		},
		{
			name: "image without command is valid",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{Image: "test:latest"},
			},
			wantErrors: 0,
		},
		{
			name: "duplicate tool names",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec: PodSpec{
					Command: []string{"echo"},
					Tools: []ToolDef{
						{Name: "dup"},
						{Name: "dup"},
					},
				},
			},
			wantErrors: 1,
		},
		{
			name: "empty tool name",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec: PodSpec{
					Command: []string{"echo"},
					Tools:   []ToolDef{{Name: ""}},
				},
			},
			wantErrors: 1,
		},
		{
			name: "invalid visibility",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Visibility: "invalid",
				},
			},
			wantErrors: 1,
		},
		{
			name: "invalid pricing model",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Pricing: &PricingSpec{Model: "invalid"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "per-call without price",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Pricing: &PricingSpec{Model: "per-call"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "subscription without tiers",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Pricing: &PricingSpec{Model: "subscription"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "valid per-call pricing",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test"},
				Spec:       PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Pricing: &PricingSpec{
						Model: "per-call",
						Price: &PriceAmount{Amount: "0.01", Currency: "USD"},
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "invalid semver",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test", Version: "invalid"},
				Spec:       PodSpec{Command: []string{"echo"}},
			},
			wantErrors: 1,
		},
		{
			name: "valid semver",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test", Version: "1.2.3"},
				Spec:       PodSpec{Command: []string{"echo"}},
			},
			wantErrors: 0,
		},
		{
			name: "semver with prerelease",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata:   Metadata{Name: "test", Version: "1.0.0-beta.1"},
				Spec:       PodSpec{Command: []string{"echo"}},
			},
			wantErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateManifest(tt.manifest)
			if len(errs) != tt.wantErrors {
				t.Errorf("ValidateManifest() returned %d errors, want %d: %v", len(errs), tt.wantErrors, errs)
			}
		})
	}
}

func TestValidateForPublish(t *testing.T) {
	tests := []struct {
		name       string
		manifest   *Manifest
		wantErrors int
	}{
		{
			name: "valid for publish",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata: Metadata{
					Name:        "test",
					Namespace:   "acme",
					Version:     "1.0.0",
					Description: "A test tool",
				},
				Spec: PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Pricing: &PricingSpec{Model: "free"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "missing namespace",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata: Metadata{
					Name:        "test",
					Version:     "1.0.0",
					Description: "A test tool",
				},
				Spec: PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Pricing: &PricingSpec{Model: "free"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "missing version",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata: Metadata{
					Name:        "test",
					Namespace:   "acme",
					Description: "A test tool",
				},
				Spec: PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Pricing: &PricingSpec{Model: "free"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "missing description",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata: Metadata{
					Name:      "test",
					Namespace: "acme",
					Version:   "1.0.0",
				},
				Spec: PodSpec{Command: []string{"echo"}},
				Marketplace: &MarketplaceSpec{
					Pricing: &PricingSpec{Model: "free"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "missing pricing",
			manifest: &Manifest{
				APIVersion: "mcp-meta/v1",
				Kind:       "ToolPod",
				Metadata: Metadata{
					Name:        "test",
					Namespace:   "acme",
					Version:     "1.0.0",
					Description: "A test tool",
				},
				Spec: PodSpec{Command: []string{"echo"}},
			},
			wantErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateForPublish(tt.manifest)
			if len(errs) != tt.wantErrors {
				t.Errorf("ValidateForPublish() returned %d errors, want %d: %v", len(errs), tt.wantErrors, errs)
			}
		})
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    time.Duration
		wantErr bool
	}{
		{"seconds", "10s", 10 * time.Second, false},
		{"minutes", "5m", 5 * time.Minute, false},
		{"milliseconds", "500ms", 500 * time.Millisecond, false},
		{"hours", "2h", 2 * time.Hour, false},
		{"complex", "1h30m", 90 * time.Minute, false},
		{"empty", "", 0, false},
		{"invalid", "invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			unmarshal := func(out any) error {
				*(out.(*string)) = tt.yaml
				return nil
			}
			err := d.UnmarshalYAML(unmarshal)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if d.Duration != tt.want {
				t.Errorf("UnmarshalYAML() = %v, want %v", d.Duration, tt.want)
			}
		})
	}
}

func TestDuration_MarshalYAML(t *testing.T) {
	d := Duration{Duration: 30 * time.Second}
	got, err := d.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML() error = %v", err)
	}
	if got != "30s" {
		t.Errorf("MarshalYAML() = %v, want 30s", got)
	}
}

func TestComputeSchemaHash(t *testing.T) {
	tests := []struct {
		name   string
		schema json.RawMessage
		want   string
	}{
		{
			name:   "empty schema",
			schema: nil,
			want:   "",
		},
		{
			name:   "empty bytes",
			schema: json.RawMessage{},
			want:   "",
		},
		{
			name:   "simple schema",
			schema: json.RawMessage(`{"type":"object"}`),
			want:   "sha256:", // Check prefix
		},
		{
			name:   "invalid json",
			schema: json.RawMessage(`{invalid}`),
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeSchemaHash(tt.schema)
			if tt.want == "sha256:" {
				if len(got) <= len("sha256:") || got[:7] != "sha256:" {
					t.Errorf("ComputeSchemaHash() = %q, want prefix sha256:", got)
				}
			} else if got != tt.want {
				t.Errorf("ComputeSchemaHash() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComputeSchemaHash_Deterministic(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"name":{"type":"string"}},"type":"object"}`)

	hash1 := ComputeSchemaHash(schema)
	hash2 := ComputeSchemaHash(schema)

	if hash1 != hash2 {
		t.Errorf("ComputeSchemaHash() not deterministic: %q != %q", hash1, hash2)
	}
}

func TestComputeSchemaHash_NormalizesJSON(t *testing.T) {
	// Different formatting, same content
	schema1 := json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`)
	schema2 := json.RawMessage(`{"properties":{"name":{"type":"string"}},"type":"object"}`)

	hash1 := ComputeSchemaHash(schema1)
	hash2 := ComputeSchemaHash(schema2)

	if hash1 != hash2 {
		t.Errorf("ComputeSchemaHash() should normalize JSON: %q != %q", hash1, hash2)
	}
}

func TestManifest_Slug(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		podName   string
		want      string
	}{
		{"with namespace", "acme", "linter", "acme/linter"},
		{"without namespace", "", "linter", "linter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manifest{
				Metadata: Metadata{
					Name:      tt.podName,
					Namespace: tt.namespace,
				},
			}
			if got := m.Slug(); got != tt.want {
				t.Errorf("Slug() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestManifest_VersionedSlug(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		podName   string
		version   string
		want      string
	}{
		{"full", "acme", "linter", "1.0.0", "acme/linter@1.0.0"},
		{"no version", "acme", "linter", "", "acme/linter"},
		{"no namespace", "", "linter", "1.0.0", "linter@1.0.0"},
		{"minimal", "", "linter", "", "linter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manifest{
				Metadata: Metadata{
					Name:      tt.podName,
					Namespace: tt.namespace,
					Version:   tt.version,
				},
			}
			if got := m.VersionedSlug(); got != tt.want {
				t.Errorf("VersionedSlug() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsValidSemver(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"1.0.0", true},
		{"1.0", true},
		{"0.1.0", true},
		{"1.0.0-alpha", true},
		{"1.0.0-beta.1", true},
		{"10.20.30", true},
		{"", false},
		{"invalid", false},
		{"v1.0.0", false}, // starts with v, not digit
		{"1", false},      // no dot
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := isValidSemver(tt.version); got != tt.want {
				t.Errorf("isValidSemver(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
