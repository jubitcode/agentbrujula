package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultStartupTimeout  = 10 * time.Second
	DefaultCallTimeout     = 30 * time.Second
	DefaultShutdownTimeout = 5 * time.Second
)

type Manifest struct {
	APIVersion  string           `yaml:"apiVersion"`
	Kind        string           `yaml:"kind"`
	Metadata    Metadata         `yaml:"metadata"`
	Spec        PodSpec          `yaml:"spec"`
	Marketplace *MarketplaceSpec `yaml:"marketplace,omitempty"`
}

type Metadata struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Labels      map[string]string `yaml:"labels"`

	// Marketplace fields
	Namespace   string   `yaml:"namespace,omitempty"`   // Publisher namespace (e.g., "acme-corp")
	Version     string   `yaml:"version,omitempty"`     // Semantic version (e.g., "1.0.0")
	DisplayName string   `yaml:"displayName,omitempty"` // Human-readable name
	License     string   `yaml:"license,omitempty"`     // SPDX license identifier
	Homepage    string   `yaml:"homepage,omitempty"`    // Project homepage URL
	Repository  string   `yaml:"repository,omitempty"`  // Source repository URL
	Keywords    []string `yaml:"keywords,omitempty"`    // Search keywords
	Authors     []Author `yaml:"authors,omitempty"`     // Tool authors/maintainers
}

// Author represents a tool author or maintainer.
type Author struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email,omitempty"`
	URL   string `yaml:"url,omitempty"`
}

// MarketplaceSpec contains marketplace-specific configuration.
type MarketplaceSpec struct {
	Pricing    *PricingSpec `yaml:"pricing,omitempty"`
	Visibility string       `yaml:"visibility,omitempty"` // public, private, unlisted
	Categories []string     `yaml:"categories,omitempty"` // Tool categories
	Support    *SupportSpec `yaml:"support,omitempty"`
	Verified   bool         `yaml:"verified,omitempty"` // Platform-verified badge
}

// PricingSpec defines how the tool is monetized.
type PricingSpec struct {
	Model     string        `yaml:"model"`               // free, per-call, subscription
	Price     *PriceAmount  `yaml:"price,omitempty"`     // For per-call pricing
	FreeQuota int           `yaml:"freeQuota,omitempty"` // Free calls per month
	Tiers     []PricingTier `yaml:"tiers,omitempty"`     // For subscription pricing
}

// PriceAmount represents a monetary amount.
type PriceAmount struct {
	Amount   string `yaml:"amount"`   // Decimal string (e.g., "0.001")
	Currency string `yaml:"currency"` // ISO 4217 code (e.g., "USD")
}

// PricingTier for subscription-based pricing.
type PricingTier struct {
	Name   string       `yaml:"name"`
	Price  *PriceAmount `yaml:"price"`
	Period string       `yaml:"period,omitempty"` // month, year
	Limits *TierLimits  `yaml:"limits,omitempty"`
}

// TierLimits for subscription tiers.
type TierLimits struct {
	Calls    int      `yaml:"calls,omitempty"`    // Max calls per period (-1 for unlimited)
	Features []string `yaml:"features,omitempty"` // Enabled features
}

// SupportSpec contains support contact information.
type SupportSpec struct {
	Email         string `yaml:"email,omitempty"`
	Documentation string `yaml:"documentation,omitempty"`
	Issues        string `yaml:"issues,omitempty"` // Issue tracker URL
}

type PodSpec struct {
	Name            string            `yaml:"-"` // Set from metadata.name
	Runtime         string            `yaml:"runtime"`
	Command         []string          `yaml:"command"`
	Workdir         string            `yaml:"workdir"`
	Env             map[string]string `yaml:"env"`
	StartupTimeout  Duration          `yaml:"startupTimeout"`
	CallTimeout     Duration          `yaml:"callTimeout"`
	ShutdownTimeout Duration          `yaml:"shutdownTimeout"`
	HealthCheck     *HealthCheck      `yaml:"healthCheck"`
	PreWarm         bool              `yaml:"preWarm"`
	Scaling         *ScalingSpec      `yaml:"scaling,omitempty"`
	Tools           []ToolDef         `yaml:"tools"`

	// Language specifies the agent/tool implementation language for build pipeline.
	// Valid values: "python", "go", "node", "rust", "custom"
	Language string `yaml:"language,omitempty"`

	// Marketplace container build configuration
	Build     *BuildSpec    `yaml:"build,omitempty"`
	Resources *ResourceSpec `yaml:"resources,omitempty"`
	Image     string        `yaml:"image,omitempty"` // Pre-built container image

	// RuntimeBackend specifies which execution backend to use.
	// Valid values: "subprocess" (default), "docker", "firecracker", "vz", "sandbox"
	// - subprocess: Run as a local subprocess (fastest, no isolation)
	// - docker: Run in Docker container
	// - firecracker: Run in Firecracker microVM (Linux only)
	// - vz: Run in Apple Virtualization.framework VM (macOS only)
	// - sandbox: Run in sandboxed subprocess
	RuntimeBackend string `yaml:"runtimeBackend,omitempty"`

	// Resilience configuration
	CircuitBreaker  *CircuitBreakerSpec `yaml:"circuitBreaker,omitempty"`
	Retry           *RetrySpec          `yaml:"retry,omitempty"`
	MaxResponseSize int                 `yaml:"maxResponseSize,omitempty"` // bytes, default 1MB

	// A2A agent configuration (only for Kind: AgentPod)
	AgentCard *AgentCardSpec `yaml:"agentCard,omitempty"`
	Skills    []SkillDef     `yaml:"skills,omitempty"`
}

// AgentCardSpec configures the A2A agent card served at /.well-known/agent-card.json.
type AgentCardSpec struct {
	Name               string           `yaml:"name"`
	Description        string           `yaml:"description"`
	URL                string           `yaml:"url,omitempty"`
	Version            string           `yaml:"version,omitempty"`
	Provider           *ProviderSpec    `yaml:"provider,omitempty"`
	Capabilities       *A2ACapabilities `yaml:"capabilities,omitempty"`
	Authentication     *A2AAuthSpec     `yaml:"authentication,omitempty"`
	DefaultInputModes  []string         `yaml:"defaultInputModes,omitempty"`
	DefaultOutputModes []string         `yaml:"defaultOutputModes,omitempty"`
}

// ProviderSpec identifies the agent provider.
type ProviderSpec struct {
	Organization string `yaml:"organization"`
	URL          string `yaml:"url,omitempty"`
}

// A2ACapabilities declares which A2A features the agent supports.
type A2ACapabilities struct {
	Streaming         bool `yaml:"streaming"`
	PushNotifications bool `yaml:"pushNotifications"`
}

// A2AAuthSpec declares authentication schemes for the agent.
type A2AAuthSpec struct {
	Schemes []AuthScheme `yaml:"schemes"`
}

// AuthScheme describes a single authentication method.
type AuthScheme struct {
	Scheme       string `yaml:"scheme"`
	BearerFormat string `yaml:"bearerFormat,omitempty"`
}

// SkillDef defines an A2A skill that maps to one or more tools.
type SkillDef struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags,omitempty"`
	Examples    []string `yaml:"examples,omitempty"`
	MapToTools  []string `yaml:"mapToTools"`
}

// BuildSpec configures how to build the container image.
type BuildSpec struct {
	Runtime    string `yaml:"runtime,omitempty"`    // Base runtime (e.g., "python:3.12", "go:1.22", "node:20")
	Dockerfile string `yaml:"dockerfile,omitempty"` // Path to custom Dockerfile
	Context    string `yaml:"context,omitempty"`    // Build context directory
}

// ResourceSpec defines resource limits for the pod.
type ResourceSpec struct {
	CPU              string   `yaml:"cpu,omitempty"`              // CPU cores (e.g., "0.5", "1")
	Memory           string   `yaml:"memory,omitempty"`           // Memory limit (e.g., "256Mi", "1Gi")
	EphemeralStorage string   `yaml:"ephemeralStorage,omitempty"` // Disk limit (e.g., "1Gi")
	Timeout          Duration `yaml:"timeout,omitempty"`          // Max execution time
}

type HealthCheck struct {
	Enabled  bool     `yaml:"enabled"`
	Interval Duration `yaml:"interval"`
}

// ScalingSpec configures pod replica scaling for high-concurrency workloads.
type ScalingSpec struct {
	MinReplicas    int      `yaml:"minReplicas"`    // Minimum number of pod replicas (default: 1)
	MaxReplicas    int      `yaml:"maxReplicas"`    // Maximum number of pod replicas (default: 1)
	TargetPending  int      `yaml:"targetPending"`  // Scale up when pending requests exceed this per replica
	ScaleDownDelay Duration `yaml:"scaleDownDelay"` // Wait before scaling down

	// Latency-based scaling configuration
	Latency *LatencyScalingSpec `yaml:"latency,omitempty"`

	// Warm pool configuration
	WarmPool *WarmPoolSpec `yaml:"warmPool,omitempty"`
}

// LatencyScalingSpec configures latency-based auto-scaling.
type LatencyScalingSpec struct {
	// TargetP50 is the target 50th percentile latency in milliseconds.
	// Scale up if P50 exceeds this value.
	TargetP50 int `yaml:"targetP50,omitempty"`

	// TargetP95 is the target 95th percentile latency in milliseconds.
	// Scale up if P95 exceeds this value.
	TargetP95 int `yaml:"targetP95,omitempty"`

	// TargetP99 is the target 99th percentile latency in milliseconds.
	// Scale up if P99 exceeds this value.
	TargetP99 int `yaml:"targetP99,omitempty"`

	// WindowSize is the number of recent requests to consider for latency calculation.
	// Default: 100
	WindowSize int `yaml:"windowSize,omitempty"`

	// ScaleUpThreshold is the number of consecutive violations before scaling up.
	// Prevents scaling on transient spikes. Default: 3
	ScaleUpThreshold int `yaml:"scaleUpThreshold,omitempty"`

	// ScaleDownThreshold is the number of consecutive good readings before scaling down.
	// Default: 10
	ScaleDownThreshold int `yaml:"scaleDownThreshold,omitempty"`
}

// WarmPoolSpec configures warm pool behavior for a pod.
type WarmPoolSpec struct {
	// ScaleToZero allows the pool to scale to zero replicas when idle.
	// When enabled, MinReplicas can be set to 0.
	ScaleToZero bool `yaml:"scaleToZero"`

	// IdleTimeout is how long the pool can be idle before scaling to zero.
	// Default: 5m
	IdleTimeout Duration `yaml:"idleTimeout"`

	// MinWarmPods is the minimum number of warm pods to maintain even during idle.
	// This is a floor that overrides MinReplicas when ScaleToZero is true.
	// Default: 0 (full scale-to-zero)
	MinWarmPods int `yaml:"minWarmPods"`

	// ColdStartTarget is the target cold start latency for monitoring.
	// Default: 500ms
	ColdStartTarget Duration `yaml:"coldStartTarget"`

	// PredictiveScaling enables pre-warming based on historical usage patterns.
	// Default: false
	PredictiveScaling bool `yaml:"predictiveScaling"`

	// WarmupLeadTime is how far in advance to pre-warm before predicted demand.
	// Default: 30s
	WarmupLeadTime Duration `yaml:"warmupLeadTime"`
}

type ToolDef struct {
	Name            string          `yaml:"name"`
	Description     string          `yaml:"description"`
	Timeout         Duration        `yaml:"timeout"`
	InputSchema     json.RawMessage `yaml:"inputSchema"`
	Contract        *ToolContract   `yaml:"contract"`
	MaxRetries      int             `yaml:"maxRetries,omitempty"`
	RetryBaseDelay  Duration        `yaml:"retryBaseDelay,omitempty"`
	MaxResponseSize int             `yaml:"maxResponseSize,omitempty"` // bytes, overrides pod default
}

// CircuitBreakerSpec configures circuit breaker behavior for a pod.
type CircuitBreakerSpec struct {
	FailureThreshold int      `yaml:"failureThreshold"` // default: 5
	ResetTimeout     Duration `yaml:"resetTimeout"`     // default: 30s
	SuccessThreshold int      `yaml:"successThreshold"` // default: 2
}

// RetrySpec configures retry behavior for pod calls.
type RetrySpec struct {
	MaxRetries int      `yaml:"maxRetries"` // default: 0 (no retry)
	BaseDelay  Duration `yaml:"baseDelay"`  // default: 100ms
	MaxDelay   Duration `yaml:"maxDelay"`   // default: 5s
}

// ToolContract Contracts enable callers to verify they're calling the expected tool with the expected schema.
type ToolContract struct {
	ID      string `yaml:"id" json:"id"`
	Version string `yaml:"version" json:"version"`
	// Auto-generated at manifest load time, not specified in YAML
	SchemaHash string `yaml:"-" json:"schema_hash,omitempty"`
}

func ComputeSchemaHash(schema json.RawMessage) string {
	if len(schema) == 0 {
		return ""
	}

	var normalized any
	if err := json.Unmarshal(schema, &normalized); err != nil {
		return ""
	}

	canonicalJSON, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}

	hash := sha256.Sum256(canonicalJSON)
	return "sha256:" + hex.EncodeToString(hash[:])
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		d.Duration = 0
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

func (t *ToolDef) UnmarshalYAML(node *yaml.Node) error {
	// Create a temporary struct without InputSchema to avoid recursion
	type toolDefRaw struct {
		Name            string        `yaml:"name"`
		Description     string        `yaml:"description"`
		Timeout         Duration      `yaml:"timeout"`
		Contract        *ToolContract `yaml:"contract"`
		MaxRetries      int           `yaml:"maxRetries"`
		RetryBaseDelay  Duration      `yaml:"retryBaseDelay"`
		MaxResponseSize int           `yaml:"maxResponseSize"`
	}

	var raw toolDefRaw
	if err := node.Decode(&raw); err != nil {
		return err
	}

	t.Name = raw.Name
	t.Description = raw.Description
	t.Timeout = raw.Timeout
	t.Contract = raw.Contract
	t.MaxRetries = raw.MaxRetries
	t.RetryBaseDelay = raw.RetryBaseDelay
	t.MaxResponseSize = raw.MaxResponseSize

	for i := 0; i < len(node.Content)-1; i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		if key.Value == "inputSchema" {
			var schemaData any
			if err := value.Decode(&schemaData); err != nil {
				return fmt.Errorf("decode inputSchema: %w", err)
			}

			jsonBytes, err := json.Marshal(schemaData)
			if err != nil {
				return fmt.Errorf("marshal inputSchema to JSON: %w", err)
			}
			t.InputSchema = jsonBytes
			break
		}
	}

	if t.Contract != nil && len(t.InputSchema) > 0 {
		t.Contract.SchemaHash = ComputeSchemaHash(t.InputSchema)
	}

	return nil
}

func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	manifest.Spec.Name = manifest.Metadata.Name
	applyDefaults(&manifest)

	return &manifest, nil
}

func applyDefaults(m *Manifest) {
	if m.Spec.StartupTimeout.Duration == 0 {
		m.Spec.StartupTimeout.Duration = DefaultStartupTimeout
	}
	if m.Spec.CallTimeout.Duration == 0 {
		m.Spec.CallTimeout.Duration = DefaultCallTimeout
	}
	if m.Spec.ShutdownTimeout.Duration == 0 {
		m.Spec.ShutdownTimeout.Duration = DefaultShutdownTimeout
	}
}

func ValidateManifest(m *Manifest) []error {
	var errs []error

	if m.APIVersion != "mcp-meta/v1" {
		errs = append(errs, fmt.Errorf("invalid apiVersion: %q (expected mcp-meta/v1)", m.APIVersion))
	}

	if m.Kind != "ToolPod" && m.Kind != "AgentPod" {
		errs = append(errs, fmt.Errorf("invalid kind: %q (expected ToolPod or AgentPod)", m.Kind))
	}

	if m.Metadata.Name == "" {
		errs = append(errs, fmt.Errorf("metadata.name is required"))
	}

	if len(m.Spec.Command) == 0 && m.Spec.Image == "" && m.Spec.RuntimeBackend == "" {
		errs = append(errs, fmt.Errorf("spec.command, spec.image, or spec.runtimeBackend is required"))
	}

	// Validate language field if present
	if m.Spec.Language != "" {
		validLanguages := map[string]bool{"python": true, "go": true, "node": true, "rust": true, "custom": true}
		if !validLanguages[m.Spec.Language] {
			errs = append(errs, fmt.Errorf("invalid language: %q (expected python, go, node, rust, or custom)", m.Spec.Language))
		}
	}

	// Validate tool names
	toolNames := make(map[string]bool)
	for _, tool := range m.Spec.Tools {
		if tool.Name == "" {
			errs = append(errs, fmt.Errorf("tool name is required"))
		}
		if toolNames[tool.Name] {
			errs = append(errs, fmt.Errorf("duplicate tool name within pod: %s", tool.Name))
		}
		toolNames[tool.Name] = true
	}

	// Validate marketplace fields if present
	errs = append(errs, validateMarketplace(m)...)

	// Validate AgentPod-specific fields
	if m.Kind == "AgentPod" {
		if m.Spec.AgentCard == nil {
			errs = append(errs, fmt.Errorf("agentCard is required for AgentPod"))
		} else {
			if m.Spec.AgentCard.Name == "" {
				errs = append(errs, fmt.Errorf("agentCard.name is required"))
			}
			if m.Spec.AgentCard.Description == "" {
				errs = append(errs, fmt.Errorf("agentCard.description is required"))
			}
		}
		if len(m.Spec.Skills) == 0 {
			errs = append(errs, fmt.Errorf("at least one skill is required for AgentPod"))
		}
		for _, skill := range m.Spec.Skills {
			if skill.ID == "" {
				errs = append(errs, fmt.Errorf("skill id is required"))
			}
			if skill.Name == "" {
				errs = append(errs, fmt.Errorf("skill name is required"))
			}
		}
	}

	return errs
}

// ValidateForPublish validates the manifest has all required fields for marketplace publishing.
func ValidateForPublish(m *Manifest) []error {
	errs := ValidateManifest(m)

	if m.Metadata.Namespace == "" {
		errs = append(errs, fmt.Errorf("metadata.namespace is required for publishing"))
	}
	if m.Metadata.Version == "" {
		errs = append(errs, fmt.Errorf("metadata.version is required for publishing"))
	}
	if m.Metadata.Description == "" {
		errs = append(errs, fmt.Errorf("metadata.description is required for publishing"))
	}
	if m.Marketplace == nil || m.Marketplace.Pricing == nil {
		errs = append(errs, fmt.Errorf("marketplace.pricing is required for publishing"))
	}

	return errs
}

func validateMarketplace(m *Manifest) []error {
	var errs []error

	// Validate version format if provided (basic semver check)
	if m.Metadata.Version != "" {
		if !isValidSemver(m.Metadata.Version) {
			errs = append(errs, fmt.Errorf("metadata.version must be valid semver: %q", m.Metadata.Version))
		}
	}

	// Validate visibility
	if m.Marketplace != nil && m.Marketplace.Visibility != "" {
		validVisibility := map[string]bool{"public": true, "private": true, "unlisted": true}
		if !validVisibility[m.Marketplace.Visibility] {
			errs = append(errs, fmt.Errorf("marketplace.visibility must be one of: public, private, unlisted"))
		}
	}

	// Validate pricing model
	if m.Marketplace != nil && m.Marketplace.Pricing != nil {
		p := m.Marketplace.Pricing
		validModels := map[string]bool{"free": true, "per-call": true, "subscription": true}
		if !validModels[p.Model] {
			errs = append(errs, fmt.Errorf("marketplace.pricing.model must be one of: free, per-call, subscription"))
		}
		if p.Model == "per-call" && p.Price == nil {
			errs = append(errs, fmt.Errorf("marketplace.pricing.price is required for per-call model"))
		}
		if p.Model == "subscription" && len(p.Tiers) == 0 {
			errs = append(errs, fmt.Errorf("marketplace.pricing.tiers is required for subscription model"))
		}
	}

	return errs
}

// isValidSemver performs basic semver validation (major.minor.patch with optional prerelease).
func isValidSemver(version string) bool {
	// Basic pattern: major.minor.patch or major.minor.patch-prerelease
	// This is a simplified check, not full semver spec compliance
	if version == "" {
		return false
	}
	// Must start with a digit
	if version[0] < '0' || version[0] > '9' {
		return false
	}
	// Must contain at least one dot
	dotCount := 0
	for _, c := range version {
		if c == '.' {
			dotCount++
		}
	}
	return dotCount >= 1
}

// Slug returns the fully qualified tool identifier (namespace/name).
func (m *Manifest) Slug() string {
	if m.Metadata.Namespace != "" {
		return m.Metadata.Namespace + "/" + m.Metadata.Name
	}
	return m.Metadata.Name
}

// VersionedSlug returns the fully qualified versioned identifier (namespace/name@version).
func (m *Manifest) VersionedSlug() string {
	slug := m.Slug()
	if m.Metadata.Version != "" {
		return slug + "@" + m.Metadata.Version
	}
	return slug
}
