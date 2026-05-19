package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptValidationCommand(t *testing.T) {
	script := Script{
		Command:     "go test ./...",
		Description: "Run tests",
	}

	err := script.Validate("test")
	assert.NoError(t, err)
}

func TestScriptValidationComposite(t *testing.T) {
	script := Script{
		Composite:   []string{"lint", "test", "build"},
		Description: "Full CI",
	}

	err := script.Validate("ci")
	assert.NoError(t, err)
}

func TestScriptValidationNeitherCommandNorComposite(t *testing.T) {
	script := Script{
		Description: "Empty script",
	}

	err := script.Validate("empty")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must have either command or composite")
}

func TestScriptValidationBothCommandAndComposite(t *testing.T) {
	script := Script{
		Command:   "go test",
		Composite: []string{"lint", "build"},
	}

	err := script.Validate("invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot have both command and composite")
}

func TestScriptValidationInvalidPort(t *testing.T) {
	script := Script{
		Command: "go run .",
		Ports:   []string{"invalid"},
	}

	err := script.Validate("dev")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ports")
}

func TestScriptIsComposite(t *testing.T) {
	assert.False(t, Script{Command: "echo"}.IsComposite())
	assert.True(t, Script{Composite: []string{"a", "b"}}.IsComposite())
}

func TestScriptGetCommand(t *testing.T) {
	script := Script{Command: "go test"}

	assert.Equal(t, "go test", script.GetCommand(nil))
	assert.Equal(t, "go test", script.GetCommand([]string{}))
	assert.Equal(t, "go test -v", script.GetCommand([]string{"-v"}))
	assert.Equal(t, "go test -v ./...", script.GetCommand([]string{"-v", "./..."}))
}

func TestScriptGetCommandComposite(t *testing.T) {
	script := Script{Composite: []string{"a", "b"}}

	assert.Equal(t, "", script.GetCommand(nil))
}

func TestResolveCompositeSimple(t *testing.T) {
	scripts := map[string]Script{
		"test": {Command: "go test ./..."},
	}

	execs, err := ResolveComposite(scripts, "test", nil)
	require.NoError(t, err)

	assert.Len(t, execs, 1)
	assert.Equal(t, "test", execs[0].Name)
	assert.Equal(t, "go test ./...", execs[0].Command)
}

func TestResolveCompositeWithArgs(t *testing.T) {
	scripts := map[string]Script{
		"test": {Command: "go test"},
	}

	execs, err := ResolveComposite(scripts, "test", []string{"-v", "./..."})
	require.NoError(t, err)

	assert.Equal(t, "go test -v ./...", execs[0].Command)
}

func TestResolveCompositeMultiple(t *testing.T) {
	scripts := map[string]Script{
		"lint":  {Command: "golangci-lint run"},
		"test":  {Command: "go test ./..."},
		"build": {Command: "go build ./..."},
		"ci":    {Composite: []string{"lint", "test", "build"}},
	}

	execs, err := ResolveComposite(scripts, "ci", nil)
	require.NoError(t, err)

	assert.Len(t, execs, 3)
	assert.Equal(t, "lint", execs[0].Name)
	assert.Equal(t, "test", execs[1].Name)
	assert.Equal(t, "build", execs[2].Name)
}

func TestResolveCompositeArgsOnlyToLast(t *testing.T) {
	scripts := map[string]Script{
		"lint":  {Command: "golangci-lint run"},
		"test":  {Command: "go test"},
		"ci":    {Composite: []string{"lint", "test"}},
	}

	execs, err := ResolveComposite(scripts, "ci", []string{"-v"})
	require.NoError(t, err)

	assert.Equal(t, "golangci-lint run", execs[0].Command) // No args
	assert.Equal(t, "go test -v", execs[1].Command)        // Has args
}

func TestResolveCompositeUnknownScript(t *testing.T) {
	scripts := map[string]Script{
		"ci": {Composite: []string{"nonexistent"}},
	}

	_, err := ResolveComposite(scripts, "ci", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown script")
}

func TestResolveCompositeCircularReference(t *testing.T) {
	scripts := map[string]Script{
		"a": {Composite: []string{"b"}},
		"b": {Composite: []string{"a"}},
	}

	_, err := ResolveComposite(scripts, "a", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference")
}

func TestResolveCompositeNested(t *testing.T) {
	scripts := map[string]Script{
		"unit":        {Command: "go test -short ./..."},
		"integration": {Command: "go test -tags=integration ./..."},
		"test":        {Composite: []string{"unit", "integration"}},
		"lint":        {Command: "golangci-lint run"},
		"ci":          {Composite: []string{"lint", "test"}},
	}

	execs, err := ResolveComposite(scripts, "ci", nil)
	require.NoError(t, err)

	assert.Len(t, execs, 3)
	assert.Equal(t, "lint", execs[0].Name)
	assert.Equal(t, "unit", execs[1].Name)
	assert.Equal(t, "integration", execs[2].Name)
}

func TestResolveCompositePreservesMetadata(t *testing.T) {
	scripts := map[string]Script{
		"dev": {
			Command: "go run .",
			Ports:   []string{"8080:8080"},
			Env:     []string{"DEBUG=true"},
			Workdir: "/app",
		},
	}

	execs, err := ResolveComposite(scripts, "dev", nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"8080:8080"}, execs[0].Ports)
	assert.Equal(t, []string{"DEBUG=true"}, execs[0].Env)
	assert.Equal(t, "/app", execs[0].Workdir)
}
