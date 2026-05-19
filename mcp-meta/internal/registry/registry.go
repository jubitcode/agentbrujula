package registry

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type Registry interface {
	LoadManifests(path string) error
	RegisterManifest(manifest *Manifest) error
	UnregisterPod(podName string) error
	AllTools() []ToolInfo
	ResolveTool(toolName string) (*PodSpec, *ToolDef, error)
	GetPodSpec(podName string) *PodSpec
	PodsToPreWarm() []*PodSpec
	AllAgentPods() []*PodSpec
	Validate() []error
}

type toolEntry struct {
	podName string
	podSpec *PodSpec
	toolDef *ToolDef
	fqn     string // fully qualified name: pod.tool
}

type registry struct {
	mu        sync.RWMutex
	manifests map[string]*Manifest
	podSpecs  map[string]*PodSpec
	tools     map[string]*toolEntry
	shortName map[string][]string
}

func New() Registry {
	return &registry{
		manifests: make(map[string]*Manifest),
		podSpecs:  make(map[string]*PodSpec),
		tools:     make(map[string]*toolEntry),
		shortName: make(map[string][]string),
	}
}

func (r *registry) LoadManifests(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat path %s: %w", path, err)
	}

	if info.IsDir() {
		return r.loadDirectory(path)
	}
	return r.loadFile(path)
}

func (r *registry) loadDirectory(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			if err := r.loadFile(path); err != nil {
				return fmt.Errorf("load %s: %w", path, err)
			}
		}

		return nil
	})
}

func (r *registry) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	manifest, err := ParseManifest(data)
	if err != nil {
		return err
	}

	if errs := ValidateManifest(manifest); len(errs) > 0 {
		return errs[0]
	}

	// Resolve relative workdir to absolute path relative to manifest file
	if manifest.Spec.Workdir != "" && !filepath.IsAbs(manifest.Spec.Workdir) {
		manifestDir := filepath.Dir(path)
		absWorkdir, err := filepath.Abs(filepath.Join(manifestDir, manifest.Spec.Workdir))
		if err != nil {
			return fmt.Errorf("resolve workdir: %w", err)
		}
		slog.Debug("resolved workdir", "pod", manifest.Metadata.Name, "original", manifest.Spec.Workdir, "resolved", absWorkdir)
		manifest.Spec.Workdir = absWorkdir
	}

	podName := manifest.Metadata.Name
	if _, exists := r.manifests[podName]; exists {
		return fmt.Errorf("duplicate pod name: %s", podName)
	}

	r.manifests[podName] = manifest
	r.podSpecs[podName] = &manifest.Spec

	for i := range manifest.Spec.Tools {
		tool := &manifest.Spec.Tools[i]
		fqn := fmt.Sprintf("%s.%s", podName, tool.Name)

		entry := &toolEntry{
			podName: podName,
			podSpec: &manifest.Spec,
			toolDef: tool,
			fqn:     fqn,
		}

		r.tools[fqn] = entry
		r.shortName[tool.Name] = append(r.shortName[tool.Name], fqn)
	}

	return nil
}

// RegisterManifest adds a manifest to the live registry at runtime (hot-reload).
func (r *registry) RegisterManifest(manifest *Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	podName := manifest.Metadata.Name

	// Remove existing if re-deploying
	r.removePodLocked(podName)

	r.manifests[podName] = manifest
	r.podSpecs[podName] = &manifest.Spec

	for i := range manifest.Spec.Tools {
		tool := &manifest.Spec.Tools[i]
		fqn := fmt.Sprintf("%s.%s", podName, tool.Name)

		entry := &toolEntry{
			podName: podName,
			podSpec: &manifest.Spec,
			toolDef: tool,
			fqn:     fqn,
		}

		r.tools[fqn] = entry
		r.shortName[tool.Name] = append(r.shortName[tool.Name], fqn)
	}

	return nil
}

// UnregisterPod removes a pod and all its tools from the registry.
func (r *registry) UnregisterPod(podName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.manifests[podName]; !exists {
		return fmt.Errorf("pod not found: %s", podName)
	}

	r.removePodLocked(podName)
	return nil
}

func (r *registry) removePodLocked(podName string) {
	manifest, exists := r.manifests[podName]
	if !exists {
		return
	}

	// Remove tool entries
	for _, tool := range manifest.Spec.Tools {
		fqn := fmt.Sprintf("%s.%s", podName, tool.Name)
		delete(r.tools, fqn)

		// Clean up short name index
		fqns := r.shortName[tool.Name]
		filtered := fqns[:0]
		for _, f := range fqns {
			if f != fqn {
				filtered = append(filtered, f)
			}
		}
		if len(filtered) == 0 {
			delete(r.shortName, tool.Name)
		} else {
			r.shortName[tool.Name] = filtered
		}
	}

	delete(r.manifests, podName)
	delete(r.podSpecs, podName)
}

func (r *registry) AllTools() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []ToolInfo
	for fqn, entry := range r.tools {
		tool := ToolInfo{
			Name:        fqn,
			Description: entry.toolDef.Description,
			InputSchema: entry.toolDef.InputSchema,
		}

		// Ensure inputSchema is valid JSON
		if len(tool.InputSchema) == 0 {
			tool.InputSchema = json.RawMessage(`{"type":"object"}`)
		}

		tools = append(tools, tool)
	}

	return tools
}

func (r *registry) ResolveTool(toolName string) (*PodSpec, *ToolDef, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try fully qualified name first
	if entry, ok := r.tools[toolName]; ok {
		return entry.podSpec, entry.toolDef, nil
	}

	fqns, ok := r.shortName[toolName]
	if !ok || len(fqns) == 0 {
		return nil, nil, fmt.Errorf("tool not found: %s", toolName)
	}

	if len(fqns) > 1 {
		return nil, nil, fmt.Errorf("ambiguous tool name %q: matches %v", toolName, fqns)
	}

	entry := r.tools[fqns[0]]
	return entry.podSpec, entry.toolDef, nil
}

func (r *registry) GetPodSpec(podName string) *PodSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.podSpecs[podName]
}

func (r *registry) PodsToPreWarm() []*PodSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*PodSpec
	for _, manifest := range r.manifests {
		if manifest.Spec.PreWarm {
			spec := manifest.Spec
			result = append(result, &spec)
		}
	}
	return result
}

func (r *registry) AllAgentPods() []*PodSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*PodSpec
	for _, manifest := range r.manifests {
		if manifest.Kind == "AgentPod" && manifest.Spec.AgentCard != nil {
			result = append(result, &manifest.Spec)
		}
	}
	return result
}

func (r *registry) Validate() []error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var allErrs []error
	for _, manifest := range r.manifests {
		errs := ValidateManifest(manifest)
		allErrs = append(allErrs, errs...)
	}
	return allErrs
}
