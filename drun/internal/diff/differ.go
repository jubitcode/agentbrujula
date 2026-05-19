package diff

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/client"
)

type ChangeType string

const (
	ChangeTypeAdded    ChangeType = "added"
	ChangeTypeRemoved  ChangeType = "removed"
	ChangeTypeModified ChangeType = "modified"
)

type Differ struct {
	client *client.Client
}

func NewDiffer(dockerClient *client.Client) *Differ {
	return &Differ{
		client: dockerClient,
	}
}

type DiffResult struct {
	PackageChanges []*PackageChange
	EnvChanges     []*EnvChange
	FileChanges    []*FileChange
	ToolChanges    []*ToolChange
}

func NewDiffResult() *DiffResult {
	return &DiffResult{
		PackageChanges: make([]*PackageChange, 0),
		EnvChanges:     make([]*EnvChange, 0),
		FileChanges:    make([]*FileChange, 0),
		ToolChanges:    make([]*ToolChange, 0),
	}
}

func (r *DiffResult) HasChanges() bool {
	return len(r.PackageChanges) > 0 ||
		len(r.EnvChanges) > 0 ||
		len(r.FileChanges) > 0 ||
		len(r.ToolChanges) > 0
}

func (r *DiffResult) AddPackageChange(change *PackageChange) {
	r.PackageChanges = append(r.PackageChanges, change)
}

func (r *DiffResult) AddEnvChange(change *EnvChange) {
	r.EnvChanges = append(r.EnvChanges, change)
}

func (r *DiffResult) AddFileChange(change *FileChange) {
	r.FileChanges = append(r.FileChanges, change)
}

func (r *DiffResult) AddToolChange(change *ToolChange) {
	r.ToolChanges = append(r.ToolChanges, change)
}

func (r *DiffResult) Summary() string {
	if !r.HasChanges() {
		return "No changes detected"
	}

	var parts []string

	if len(r.PackageChanges) > 0 {
		added, removed, modified := countChanges(r.PackageChanges)
		parts = append(parts, fmt.Sprintf("%d package changes (+%d/-%d/~%d)", len(r.PackageChanges), added, removed, modified))
	}

	if len(r.EnvChanges) > 0 {
		added, removed, modified := countChanges(r.EnvChanges)
		parts = append(parts, fmt.Sprintf("%d environment changes (+%d/-%d/~%d)", len(r.EnvChanges), added, removed, modified))
	}

	if len(r.FileChanges) > 0 {
		added, removed, modified := countChanges(r.FileChanges)
		parts = append(parts, fmt.Sprintf("%d file changes (+%d/-%d/~%d)", len(r.FileChanges), added, removed, modified))
	}

	if len(r.ToolChanges) > 0 {
		added, removed, modified := countChanges(r.ToolChanges)
		parts = append(parts, fmt.Sprintf("%d tool changes (+%d/-%d/~%d)", len(r.ToolChanges), added, removed, modified))
	}

	return strings.Join(parts, ", ")
}

func countChanges[T interface{ GetType() ChangeType }](changes []T) (added, removed, modified int) {
	for _, c := range changes {
		switch c.GetType() {
		case ChangeTypeAdded:
			added++
		case ChangeTypeRemoved:
			removed++
		case ChangeTypeModified:
			modified++
		}
	}
	return
}

type PackageChange struct {
	Name       string
	Type       ChangeType
	OldVersion string
	NewVersion string
}

func (c *PackageChange) GetType() ChangeType {
	return c.Type
}

func (c *PackageChange) String() string {
	switch c.Type {
	case ChangeTypeAdded:
		return fmt.Sprintf("+ %s (%s)", c.Name, c.NewVersion)
	case ChangeTypeRemoved:
		return fmt.Sprintf("- %s (%s)", c.Name, c.OldVersion)
	case ChangeTypeModified:
		return fmt.Sprintf("~ %s (%s -> %s)", c.Name, c.OldVersion, c.NewVersion)
	}
	return c.Name
}

type EnvChange struct {
	Name     string
	Type     ChangeType
	OldValue string
	NewValue string
}

func (c *EnvChange) GetType() ChangeType {
	return c.Type
}

func (c *EnvChange) String() string {
	switch c.Type {
	case ChangeTypeAdded:
		return fmt.Sprintf("+ %s=%s", c.Name, c.NewValue)
	case ChangeTypeRemoved:
		return fmt.Sprintf("- %s=%s", c.Name, c.OldValue)
	case ChangeTypeModified:
		return fmt.Sprintf("~ %s: %s -> %s", c.Name, c.OldValue, c.NewValue)
	}
	return c.Name
}

type FileChange struct {
	Path    string
	Type    ChangeType
	OldHash string
	NewHash string
}

func (c *FileChange) GetType() ChangeType {
	return c.Type
}

func (c *FileChange) String() string {
	switch c.Type {
	case ChangeTypeAdded:
		return fmt.Sprintf("+ %s", c.Path)
	case ChangeTypeRemoved:
		return fmt.Sprintf("- %s", c.Path)
	case ChangeTypeModified:
		return fmt.Sprintf("~ %s", c.Path)
	}
	return c.Path
}

type ToolChange struct {
	Name       string
	Type       ChangeType
	OldVersion string
	NewVersion string
	Path       string
}

func (c *ToolChange) GetType() ChangeType {
	return c.Type
}

func (c *ToolChange) String() string {
	switch c.Type {
	case ChangeTypeAdded:
		return fmt.Sprintf("+ %s (%s)", c.Name, c.NewVersion)
	case ChangeTypeRemoved:
		return fmt.Sprintf("- %s (%s)", c.Name, c.OldVersion)
	case ChangeTypeModified:
		return fmt.Sprintf("~ %s (%s -> %s)", c.Name, c.OldVersion, c.NewVersion)
	}
	return c.Name
}

type PackageInfo struct {
	Name    string
	Version string
}

func (p PackageInfo) String() string {
	return fmt.Sprintf("%s (%s)", p.Name, p.Version)
}

type ToolInfo struct {
	Name    string
	Version string
	Path    string
}

func (t ToolInfo) String() string {
	return fmt.Sprintf("%s (%s)", t.Name, t.Version)
}

type FileInfo struct {
	Path string
	Hash string
	Size int64
	Mode string
}

func (f FileInfo) String() string {
	return fmt.Sprintf("%s [%s]", f.Path, f.Hash)
}

type Baseline struct {
	Packages []PackageInfo
	EnvVars  map[string]string
	Files    []FileInfo
	Tools    []ToolInfo
}

func NewBaseline() *Baseline {
	return &Baseline{
		Packages: make([]PackageInfo, 0),
		EnvVars:  make(map[string]string),
		Files:    make([]FileInfo, 0),
		Tools:    make([]ToolInfo, 0),
	}
}

func DiffPackages(baseline, current []PackageInfo) []*PackageChange {
	changes := make([]*PackageChange, 0)

	baselineMap := make(map[string]string)
	for _, p := range baseline {
		baselineMap[p.Name] = p.Version
	}

	currentMap := make(map[string]string)
	for _, p := range current {
		currentMap[p.Name] = p.Version
	}

	for name, oldVersion := range baselineMap {
		if newVersion, exists := currentMap[name]; exists {
			if newVersion != oldVersion {
				changes = append(changes, &PackageChange{
					Name:       name,
					Type:       ChangeTypeModified,
					OldVersion: oldVersion,
					NewVersion: newVersion,
				})
			}
		} else {
			changes = append(changes, &PackageChange{
				Name:       name,
				Type:       ChangeTypeRemoved,
				OldVersion: oldVersion,
			})
		}
	}

	for name, newVersion := range currentMap {
		if _, exists := baselineMap[name]; !exists {
			changes = append(changes, &PackageChange{
				Name:       name,
				Type:       ChangeTypeAdded,
				NewVersion: newVersion,
			})
		}
	}

	return changes
}

func DiffEnvVars(baseline, current map[string]string) []*EnvChange {
	changes := make([]*EnvChange, 0)

	for name, oldValue := range baseline {
		if newValue, exists := current[name]; exists {
			if newValue != oldValue {
				changes = append(changes, &EnvChange{
					Name:     name,
					Type:     ChangeTypeModified,
					OldValue: oldValue,
					NewValue: newValue,
				})
			}
		} else {
			changes = append(changes, &EnvChange{
				Name:     name,
				Type:     ChangeTypeRemoved,
				OldValue: oldValue,
			})
		}
	}

	for name, newValue := range current {
		if _, exists := baseline[name]; !exists {
			changes = append(changes, &EnvChange{
				Name:     name,
				Type:     ChangeTypeAdded,
				NewValue: newValue,
			})
		}
	}

	return changes
}

func DiffFiles(baseline, current []FileInfo) []*FileChange {
	changes := make([]*FileChange, 0)

	baselineMap := make(map[string]string)
	for _, f := range baseline {
		baselineMap[f.Path] = f.Hash
	}

	currentMap := make(map[string]string)
	for _, f := range current {
		currentMap[f.Path] = f.Hash
	}

	for path, oldHash := range baselineMap {
		if newHash, exists := currentMap[path]; exists {
			if newHash != oldHash {
				changes = append(changes, &FileChange{
					Path:    path,
					Type:    ChangeTypeModified,
					OldHash: oldHash,
					NewHash: newHash,
				})
			}
		} else {
			changes = append(changes, &FileChange{
				Path:    path,
				Type:    ChangeTypeRemoved,
				OldHash: oldHash,
			})
		}
	}

	for path, newHash := range currentMap {
		if _, exists := baselineMap[path]; !exists {
			changes = append(changes, &FileChange{
				Path:    path,
				Type:    ChangeTypeAdded,
				NewHash: newHash,
			})
		}
	}

	return changes
}

func DiffTools(baseline, current []ToolInfo) []*ToolChange {
	changes := make([]*ToolChange, 0)

	baselineMap := make(map[string]string)
	for _, t := range baseline {
		baselineMap[t.Name] = t.Version
	}

	currentMap := make(map[string]string)
	for _, t := range current {
		currentMap[t.Name] = t.Version
	}

	for name, oldVersion := range baselineMap {
		if newVersion, exists := currentMap[name]; exists {
			if newVersion != oldVersion {
				changes = append(changes, &ToolChange{
					Name:       name,
					Type:       ChangeTypeModified,
					OldVersion: oldVersion,
					NewVersion: newVersion,
				})
			}
		} else {
			changes = append(changes, &ToolChange{
				Name:       name,
				Type:       ChangeTypeRemoved,
				OldVersion: oldVersion,
			})
		}
	}

	for name, newVersion := range currentMap {
		if _, exists := baselineMap[name]; !exists {
			changes = append(changes, &ToolChange{
				Name:       name,
				Type:       ChangeTypeAdded,
				NewVersion: newVersion,
			})
		}
	}

	return changes
}

func (d *Differ) Diff(ctx context.Context, containerID string, baseline *Baseline) (*DiffResult, error) {
	if d.client == nil {
		return nil, fmt.Errorf("no Docker client available")
	}

	result := NewDiffResult()

	return result, nil
}

type DiffDisplay struct{}

func NewDiffDisplay() *DiffDisplay {
	return &DiffDisplay{}
}

func (d *DiffDisplay) Format(result *DiffResult) string {
	if !result.HasChanges() {
		return "No changes detected\n"
	}

	var sb strings.Builder

	if len(result.PackageChanges) > 0 {
		sb.WriteString("=== Packages ===\n")
		for _, c := range result.PackageChanges {
			sb.WriteString(c.String())
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(result.EnvChanges) > 0 {
		sb.WriteString("=== Environment ===\n")
		for _, c := range result.EnvChanges {
			sb.WriteString(c.String())
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(result.FileChanges) > 0 {
		sb.WriteString("=== Files ===\n")
		for _, c := range result.FileChanges {
			sb.WriteString(c.String())
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(result.ToolChanges) > 0 {
		sb.WriteString("=== Tools ===\n")
		for _, c := range result.ToolChanges {
			sb.WriteString(c.String())
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
