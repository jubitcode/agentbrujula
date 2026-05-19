package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestSetupConfigHasContent(t *testing.T) {
	tests := []struct {
		name     string
		setup    *SetupConfig
		expected bool
	}{
		{"empty", &SetupConfig{}, false},
		{"packages", &SetupConfig{Packages: []string{"git"}}, true},
		{"run", &SetupConfig{Run: []string{"echo hello"}}, true},
		{"steps", &SetupConfig{Steps: []SetupStep{{Name: "test", Run: "echo"}}}, true},
		{"deps_only", &SetupConfig{Deps: &DepsConfig{Command: "npm install"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.setup.HasContent())
		})
	}
}

func TestSetupValidation(t *testing.T) {
	setup := &SetupConfig{
		Packages: []string{"git", "make"},
		Run:      []string{"go install something"},
	}

	err := setup.Validate()
	assert.NoError(t, err)
}

func TestSetupValidationDuplicateStepNames(t *testing.T) {
	setup := &SetupConfig{
		Steps: []SetupStep{
			{Name: "test", Run: "echo 1"},
			{Name: "test", Run: "echo 2"},
		},
	}

	err := setup.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate step name")
}

func TestSetupValidationMissingStepName(t *testing.T) {
	setup := &SetupConfig{
		Steps: []SetupStep{
			{Run: "echo hello"},
		},
	}

	err := setup.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestSetupValidationUnknownDependency(t *testing.T) {
	setup := &SetupConfig{
		Steps: []SetupStep{
			{Name: "test", Run: "echo", DependsOn: []string{"nonexistent"}},
		},
	}

	err := setup.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown step")
}

func TestSetupHash(t *testing.T) {
	setup := &SetupConfig{
		Packages: []string{"git", "make"},
		Run:      []string{"echo hello"},
	}

	hash1 := setup.Hash()
	hash2 := setup.Hash()

	assert.Equal(t, hash1, hash2)
	assert.Len(t, hash1, 16)
}

func TestSetupHashDifferent(t *testing.T) {
	setup1 := &SetupConfig{
		Packages: []string{"git"},
	}

	setup2 := &SetupConfig{
		Packages: []string{"make"},
	}

	assert.NotEqual(t, setup1.Hash(), setup2.Hash())
}

func TestSetupHashPackageOrderIndependent(t *testing.T) {
	setup1 := &SetupConfig{
		Packages: []string{"git", "make"},
	}

	setup2 := &SetupConfig{
		Packages: []string{"make", "git"},
	}

	assert.Equal(t, setup1.Hash(), setup2.Hash())
}

func TestSetupDepsHash(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test\n")
	testutil.TempFileInDir(t, tmpDir, "go.sum", "checksum\n")

	setup := &SetupConfig{
		Deps: &DepsConfig{
			Command:  "go mod download",
			Triggers: []string{"go.mod", "go.sum"},
		},
	}

	hash, err := setup.DepsHash(tmpDir)
	require.NoError(t, err)
	assert.Len(t, hash, 16)
}

func TestSetupDepsHashMissingFile(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test\n")

	setup := &SetupConfig{
		Deps: &DepsConfig{
			Command:  "go mod download",
			Triggers: []string{"go.mod", "go.sum"}, // go.sum doesn't exist
		},
	}

	hash, err := setup.DepsHash(tmpDir)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestSetupDepsHashChanges(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	goModPath := filepath.Join(tmpDir, "go.mod")

	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test v1\n")

	setup := &SetupConfig{
		Deps: &DepsConfig{
			Command:  "go mod download",
			Triggers: []string{"go.mod"},
		},
	}

	hash1, err := setup.DepsHash(tmpDir)
	require.NoError(t, err)

	// Change file content
	require.NoError(t, testutil.WriteFile(goModPath, "module test v2\n"))

	hash2, err := setup.DepsHash(tmpDir)
	require.NoError(t, err)

	assert.NotEqual(t, hash1, hash2)
}

func TestSetupGetStepsInOrder(t *testing.T) {
	setup := &SetupConfig{
		Steps: []SetupStep{
			{Name: "a", Run: "echo a", DependsOn: []string{"b"}},
			{Name: "b", Run: "echo b", DependsOn: []string{"c"}},
			{Name: "c", Run: "echo c"},
		},
	}

	steps, err := setup.GetStepsInOrder()
	require.NoError(t, err)

	// c should come before b, b before a
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name
	}

	assert.Equal(t, []string{"c", "b", "a"}, names)
}

func TestSetupGetStepsInOrderNoDeps(t *testing.T) {
	setup := &SetupConfig{
		Steps: []SetupStep{
			{Name: "a", Run: "echo a"},
			{Name: "b", Run: "echo b"},
		},
	}

	steps, err := setup.GetStepsInOrder()
	require.NoError(t, err)
	assert.Len(t, steps, 2)
}

func TestSetupGetStepsInOrderCircular(t *testing.T) {
	setup := &SetupConfig{
		Steps: []SetupStep{
			{Name: "a", Run: "echo a", DependsOn: []string{"b"}},
			{Name: "b", Run: "echo b", DependsOn: []string{"a"}},
		},
	}

	_, err := setup.GetStepsInOrder()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestSetupGetStepsInOrderEmpty(t *testing.T) {
	setup := &SetupConfig{}

	steps, err := setup.GetStepsInOrder()
	require.NoError(t, err)
	assert.Nil(t, steps)
}

func TestSetupGetStepsInOrderDiamond(t *testing.T) {
	// Diamond dependency: D depends on B and C, B and C depend on A
	setup := &SetupConfig{
		Steps: []SetupStep{
			{Name: "a", Run: "echo a"},
			{Name: "b", Run: "echo b", DependsOn: []string{"a"}},
			{Name: "c", Run: "echo c", DependsOn: []string{"a"}},
			{Name: "d", Run: "echo d", DependsOn: []string{"b", "c"}},
		},
	}

	steps, err := setup.GetStepsInOrder()
	require.NoError(t, err)

	// A must come first, D must come last
	assert.Equal(t, "a", steps[0].Name)
	assert.Equal(t, "d", steps[len(steps)-1].Name)
}
