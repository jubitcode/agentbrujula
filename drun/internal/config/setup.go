package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type SetupConfig struct {
	Packages []string    `yaml:"packages" mapstructure:"packages"`
	Run      []string    `yaml:"run" mapstructure:"run"`
	Deps     *DepsConfig `yaml:"deps" mapstructure:"deps"`
	Steps    []SetupStep `yaml:"steps" mapstructure:"steps"`
}

type DepsConfig struct {
	Command  string   `yaml:"command" mapstructure:"command"`
	Triggers []string `yaml:"triggers" mapstructure:"triggers"`
}

type SetupStep struct {
	Name         string   `yaml:"name" mapstructure:"name"`
	Run          string   `yaml:"run" mapstructure:"run"`
	DependsOn    []string `yaml:"depends_on" mapstructure:"depends_on"`
	InvalidateOn []string `yaml:"invalidate_on" mapstructure:"invalidate_on"`
	Cache        bool     `yaml:"cache" mapstructure:"cache"`
}

func (s *SetupConfig) HasContent() bool {
	return len(s.Packages) > 0 || len(s.Run) > 0 || len(s.Steps) > 0
}

func (s *SetupConfig) Validate() error {
	if len(s.Steps) == 0 {
		return nil
	}

	names := make(map[string]bool)
	for _, step := range s.Steps {
		if step.Name == "" {
			return &ConfigError{
				Field:   "setup.steps",
				Message: "step name is required",
			}
		}
		if names[step.Name] {
			return &ConfigError{
				Field:   "setup.steps",
				Message: fmt.Sprintf("duplicate step name: %s", step.Name),
			}
		}
		names[step.Name] = true
	}

	for _, step := range s.Steps {
		for _, dep := range step.DependsOn {
			if !names[dep] {
				return &ConfigError{
					Field:   "setup.steps",
					Message: fmt.Sprintf("step %q depends on unknown step %q", step.Name, dep),
				}
			}
		}
	}

	if _, err := s.GetStepsInOrder(); err != nil {
		return err
	}

	return nil
}

func (s *SetupConfig) Hash() string {
	h := sha256.New()

	pkgs := make([]string, len(s.Packages))
	copy(pkgs, s.Packages)
	sort.Strings(pkgs)
	for _, p := range pkgs {
		h.Write([]byte(p))
	}

	for _, r := range s.Run {
		h.Write([]byte(r))
	}

	for _, step := range s.Steps {
		h.Write([]byte(step.Name))
		h.Write([]byte(step.Run))
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (s *SetupConfig) DepsHash(projectDir string) (string, error) {
	if s.Deps == nil || len(s.Deps.Triggers) == 0 {
		return "", nil
	}

	h := sha256.New()

	triggers := make([]string, len(s.Deps.Triggers))
	copy(triggers, s.Deps.Triggers)
	sort.Strings(triggers)

	for _, trigger := range triggers {
		path := filepath.Join(projectDir, trigger)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("failed to read trigger file %s: %w", trigger, err)
		}
		h.Write([]byte(trigger))
		h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func (s *SetupConfig) GetStepsInOrder() ([]SetupStep, error) {
	if len(s.Steps) == 0 {
		return nil, nil
	}

	graph := make(map[string][]string)
	inDegree := make(map[string]int)
	stepMap := make(map[string]SetupStep)

	for _, step := range s.Steps {
		graph[step.Name] = []string{}
		inDegree[step.Name] = 0
		stepMap[step.Name] = step
	}

	for _, step := range s.Steps {
		for _, dep := range step.DependsOn {
			graph[dep] = append(graph[dep], step.Name)
			inDegree[step.Name]++
		}
	}

	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	sort.Strings(queue)

	var result []SetupStep
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, stepMap[name])

		dependents := graph[name]
		sort.Strings(dependents)

		for _, next := range dependents {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
				sort.Strings(queue)
			}
		}
	}

	if len(result) != len(s.Steps) {
		return nil, &ConfigError{
			Field:   "setup.steps",
			Message: "circular dependency detected",
		}
	}

	return result, nil
}
