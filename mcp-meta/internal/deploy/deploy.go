package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"mcp-meta/internal/registry"
)

// Deployer orchestrates the full deploy pipeline: validate → build → test → register.
type Deployer struct {
	store   *Store
	builder *Builder
	tester  *Tester
	reg     registry.Registry
	logger  *slog.Logger
}

// NewDeployer creates a deployer.
func NewDeployer(store *Store, reg registry.Registry, logger *slog.Logger) *Deployer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Deployer{
		store:   store,
		builder: NewBuilder(logger),
		tester:  NewTester(logger),
		reg:     reg,
		logger:  logger,
	}
}

// Input DeployInput describes what to deploy.
type Input struct {
	SourceDir    string // directory with manifest.yaml + code
	ManifestData []byte // raw manifest YAML (if provided directly)
}

// Result DeployResult is the full outcome of a deploy attempt.
type Result struct {
	ID         string       `json:"id"`
	Agent      string       `json:"agent"`
	Version    int          `json:"version"`
	Status     Status       `json:"status"`
	Steps      []StepResult `json:"steps"`
	FailedStep string       `json:"failed_step,omitempty"`
	Error      string       `json:"error,omitempty"`
	DurationMs int64        `json:"total_duration_ms"`
	URL        string       `json:"url,omitempty"`
	AgentCard  string       `json:"agent_card,omitempty"`
}

// Deploy runs the full pipeline.
func (d *Deployer) Deploy(ctx context.Context, input Input) *Result {
	start := time.Now()
	deployID := generateDeployID()

	result := &Result{
		ID:     deployID,
		Status: DeployStatusFailed,
	}

	// Validate manifest
	manifest, step := d.stepValidate(input)
	result.Steps = append(result.Steps, step)
	if step.Status == "fail" {
		result.FailedStep = "validate"
		result.Error = step.Error
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	result.Agent = manifest.Metadata.Name

	// Get next version
	version, _ := d.store.NextVersion(manifest.Metadata.Name)
	result.Version = version

	// Copy source to deployment directory
	deployDir := d.store.AgentDir(manifest.Metadata.Name, version)
	if err := copyDir(input.SourceDir, deployDir); err != nil {
		result.Steps = append(result.Steps, StepResult{
			Step: "prepare", Status: "fail", Error: err.Error(),
		})
		result.FailedStep = "prepare"
		result.Error = err.Error()
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	step = d.stepBuild(deployDir, &manifest.Spec, manifest.Metadata.Name, version)
	result.Steps = append(result.Steps, step)
	if step.Status == "fail" {
		result.FailedStep = "build"
		result.Error = step.Error
		result.DurationMs = time.Since(start).Milliseconds()
		d.persistResult(result, manifest, start)
		return result
	}

	// Smoke test
	step = d.stepTest(ctx, deployDir, &manifest.Spec)
	result.Steps = append(result.Steps, step)
	if step.Status == "fail" {
		result.FailedStep = "test"
		result.Error = step.Error
		result.DurationMs = time.Since(start).Milliseconds()
		d.persistResult(result, manifest, start)
		return result
	}

	// Update manifest to run from built Docker container via drun.
	// The container has all deps installed and runs in isolation.
	if manifest.Spec.Language != "" && manifest.Spec.Language != "custom" {
		imageTag := fmt.Sprintf("mcp-meta/%s:v%d", manifest.Metadata.Name, version)
		manifest.Spec.RuntimeBackend = "docker"
		manifest.Spec.Image = imageTag
		manifest.Spec.Workdir = "/app"
	}

	// Forward essential env vars to deployed pods
	if manifest.Spec.Env == nil {
		manifest.Spec.Env = make(map[string]string)
	}
	forwardEnvVars := []string{"OPENROUTER_API_KEY", "TAVILY_API_KEY", "MCP_META_URL", "EMBEDDING_URL", "LLM_BASE_URL", "LLM_MODEL"}
	for _, key := range forwardEnvVars {
		if val := os.Getenv(key); val != "" {
			if _, exists := manifest.Spec.Env[key]; !exists {
				manifest.Spec.Env[key] = val
			}
		}
	}

	//Register in live registry
	step = d.stepRegister(manifest)
	result.Steps = append(result.Steps, step)
	if step.Status == "fail" {
		result.FailedStep = "register"
		result.Error = step.Error
		result.DurationMs = time.Since(start).Milliseconds()
		d.persistResult(result, manifest, start)
		return result
	}

	result.Status = DeployStatusSuccess
	result.DurationMs = time.Since(start).Milliseconds()
	d.persistResult(result, manifest, start)

	d.logger.Info("deploy complete",
		"agent", result.Agent,
		"version", result.Version,
		"duration_ms", result.DurationMs,
	)

	return result
}

// Undeploy removes an agent from the registry and store.
func (d *Deployer) Undeploy(agentName string) error {
	if err := d.reg.UnregisterPod(agentName); err != nil {
		d.logger.Warn("unregister pod", "agent", agentName, "error", err)
	}
	return d.store.Delete(agentName)
}

func (d *Deployer) stepValidate(input Input) (*registry.Manifest, StepResult) {
	start := time.Now()

	var data []byte
	var err error

	if input.ManifestData != nil {
		data = input.ManifestData
	} else {
		manifestPath := filepath.Join(input.SourceDir, "manifest.yaml")
		data, err = os.ReadFile(manifestPath)
		if err != nil {
			return nil, StepResult{
				Step: "validate", Status: "fail",
				DurationMs: time.Since(start).Milliseconds(),
				Error:      fmt.Sprintf("read manifest: %v", err),
			}
		}
	}

	manifest, err := registry.ParseManifest(data)
	if err != nil {
		return nil, StepResult{
			Step: "validate", Status: "fail",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("parse manifest: %v", err),
		}
	}

	if errs := registry.ValidateManifest(manifest); len(errs) > 0 {
		return nil, StepResult{
			Step: "validate", Status: "fail",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("validation: %v", errs[0]),
		}
	}

	return manifest, StepResult{
		Step: "validate", Status: "pass",
		DurationMs: time.Since(start).Milliseconds(),
	}
}

func (d *Deployer) stepBuild(sourceDir string, spec *registry.PodSpec, name string, version int) StepResult {
	start := time.Now()

	if spec.Language == "" || spec.Language == "custom" {
		// No build needed — user provides pre-built code or Dockerfile
		return StepResult{
			Step: "build", Status: "pass",
			DurationMs: time.Since(start).Milliseconds(),
			Details:    "skipped (no language or custom)",
		}
	}

	imageTag := fmt.Sprintf("mcp-meta/%s:v%d", name, version)
	buildResult, err := d.builder.Build(sourceDir, spec, imageTag)

	if err != nil {
		return StepResult{
			Step: "build", Status: "fail",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		}
	}

	return StepResult{
		Step: "build", Status: "pass",
		DurationMs: time.Since(start).Milliseconds(),
		Details:    buildResult.ImageTag,
	}
}

func (d *Deployer) stepTest(ctx context.Context, sourceDir string, spec *registry.PodSpec) StepResult {
	start := time.Now()

	testResult, err := d.tester.Test(ctx, sourceDir, spec)

	step := StepResult{
		Step:       "test",
		DurationMs: time.Since(start).Milliseconds(),
	}

	if err != nil {
		step.Status = "fail"
		step.Error = err.Error()
		if testResult != nil {
			step.Details = testResult
		}
		return step
	}

	step.Status = "pass"
	step.Details = testResult
	return step
}

func (d *Deployer) stepRegister(manifest *registry.Manifest) StepResult {
	start := time.Now()

	if err := d.reg.RegisterManifest(manifest); err != nil {
		return StepResult{
			Step: "register", Status: "fail",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		}
	}

	return StepResult{
		Step: "register", Status: "pass",
		DurationMs: time.Since(start).Milliseconds(),
	}
}

func (d *Deployer) persistResult(result *Result, manifest *registry.Manifest, startTime time.Time) {
	manifestYAML := ""
	if manifest != nil {
		// Store raw manifest for history
		data, _ := os.ReadFile(filepath.Join(d.store.AgentDir(result.Agent, result.Version), "manifest.yaml"))
		manifestYAML = string(data)
	}

	record := &Record{
		ID:           result.ID,
		Agent:        result.Agent,
		Version:      result.Version,
		Status:       result.Status,
		Steps:        result.Steps,
		FailedStep:   result.FailedStep,
		Error:        result.Error,
		ManifestYAML: manifestYAML,
		CreatedAt:    startTime,
		DurationMs:   result.DurationMs,
	}

	if err := d.store.Create(record); err != nil {
		d.logger.Error("persist deploy record", "error", err)
	}
}

func generateDeployID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "deploy-" + hex.EncodeToString(b)
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}

	return nil
}
