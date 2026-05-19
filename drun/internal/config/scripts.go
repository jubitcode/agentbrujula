package config

import (
	"fmt"
	"strings"
)

type Script struct {
	Command     string   `yaml:"command" mapstructure:"command"`
	Description string   `yaml:"description" mapstructure:"description"`
	Ports       []string `yaml:"ports" mapstructure:"ports"`
	Env         []string `yaml:"env" mapstructure:"env"`
	Composite   []string `yaml:"composite" mapstructure:"composite"`
	Workdir     string   `yaml:"workdir" mapstructure:"workdir"`
}

type ScriptExecution struct {
	Name    string
	Command string
	Ports   []string
	Env     []string
	Workdir string
}

func (s Script) Validate(name string) error {
	hasCommand := s.Command != ""
	hasComposite := len(s.Composite) > 0

	if !hasCommand && !hasComposite {
		return &ConfigError{
			Field:   fmt.Sprintf("scripts.%s", name),
			Message: "script must have either command or composite",
		}
	}

	if hasCommand && hasComposite {
		return &ConfigError{
			Field:   fmt.Sprintf("scripts.%s", name),
			Message: "script cannot have both command and composite",
		}
	}

	for _, port := range s.Ports {
		if _, err := ParsePortBinding(port); err != nil {
			return &ConfigError{
				Field:   fmt.Sprintf("scripts.%s.ports", name),
				Message: err.Error(),
			}
		}
	}

	return nil
}

func (s Script) IsComposite() bool {
	return len(s.Composite) > 0
}

func (s Script) GetCommand(args []string) string {
	if s.IsComposite() {
		return ""
	}
	if len(args) == 0 {
		return s.Command
	}
	return s.Command + " " + strings.Join(args, " ")
}

func ResolveComposite(scripts map[string]Script, name string, args []string) ([]ScriptExecution, error) {
	return resolveCompositeInternal(scripts, name, args, make(map[string]bool))
}

func resolveCompositeInternal(scripts map[string]Script, name string, args []string, visited map[string]bool) ([]ScriptExecution, error) {
	script, ok := scripts[name]
	if !ok {
		return nil, &ConfigError{
			Field:   "scripts",
			Message: fmt.Sprintf("unknown script: %s", name),
		}
	}

	if visited[name] {
		return nil, &ConfigError{
			Field:   "scripts",
			Message: fmt.Sprintf("circular reference detected in script: %s", name),
		}
	}
	visited[name] = true
	defer func() { delete(visited, name) }()

	if !script.IsComposite() {
		return []ScriptExecution{{
			Name:    name,
			Command: script.GetCommand(args),
			Ports:   script.Ports,
			Env:     script.Env,
			Workdir: script.Workdir,
		}}, nil
	}

	var result []ScriptExecution
	for i, childName := range script.Composite {
		var childArgs []string
		if i == len(script.Composite)-1 {
			childArgs = args
		}

		childExecs, err := resolveCompositeInternal(scripts, childName, childArgs, visited)
		if err != nil {
			return nil, err
		}

		result = append(result, childExecs...)
	}

	return result, nil
}
