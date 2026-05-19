package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepsManagerNew(t *testing.T) {
	dm := NewDepsManager(nil)
	assert.NotNil(t, dm)
}

func TestDepsConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *DepsConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &DepsConfig{
				Command:  "go mod download",
				Triggers: []string{"go.mod", "go.sum"},
			},
			wantErr: false,
		},
		{
			name: "empty command",
			cfg: &DepsConfig{
				Command:  "",
				Triggers: []string{"go.mod"},
			},
			wantErr: true,
		},
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDepsConfig(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDepsHashCompute(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()

	// Create go.mod file
	goModContent := []byte("module test\n\ngo 1.22\n")
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), goModContent, 0644)
	require.NoError(t, err)

	// Create go.sum file
	goSumContent := []byte("github.com/test/pkg v1.0.0 h1:abc123\n")
	err = os.WriteFile(filepath.Join(tmpDir, "go.sum"), goSumContent, 0644)
	require.NoError(t, err)

	dm := NewDepsManager(nil)

	cfg := &DepsConfig{
		Command:  "go mod download",
		Triggers: []string{"go.mod", "go.sum"},
	}

	hash, err := dm.ComputeHash(tmpDir, cfg)
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64) // SHA256 hex length
}

func TestDepsHashDeterministic(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644)
	require.NoError(t, err)

	dm := NewDepsManager(nil)
	cfg := &DepsConfig{
		Command:  "go mod download",
		Triggers: []string{"go.mod"},
	}

	hash1, err := dm.ComputeHash(tmpDir, cfg)
	assert.NoError(t, err)

	hash2, err := dm.ComputeHash(tmpDir, cfg)
	assert.NoError(t, err)

	assert.Equal(t, hash1, hash2, "same files should produce same hash")
}

func TestDepsHashChanges(t *testing.T) {
	tmpDir := t.TempDir()

	// Create initial file
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644)
	require.NoError(t, err)

	dm := NewDepsManager(nil)
	cfg := &DepsConfig{
		Command:  "go mod download",
		Triggers: []string{"go.mod"},
	}

	hash1, err := dm.ComputeHash(tmpDir, cfg)
	assert.NoError(t, err)

	// Modify file
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n\nrequire foo v1.0\n"), 0644)
	require.NoError(t, err)

	hash2, err := dm.ComputeHash(tmpDir, cfg)
	assert.NoError(t, err)

	assert.NotEqual(t, hash1, hash2, "different files should produce different hash")
}

func TestDepsHashMissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	dm := NewDepsManager(nil)
	cfg := &DepsConfig{
		Command:  "go mod download",
		Triggers: []string{"nonexistent.txt"},
	}

	// Should still work, missing files contribute empty content
	hash, err := dm.ComputeHash(tmpDir, cfg)
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestDepsHashGlobPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "go.sum"), []byte("checksum\n"), 0644)
	require.NoError(t, err)

	dm := NewDepsManager(nil)
	cfg := &DepsConfig{
		Command:  "go mod download",
		Triggers: []string{"go.*"},
	}

	hash, err := dm.ComputeHash(tmpDir, cfg)
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestDepsStateNew(t *testing.T) {
	state := NewDepsState()
	assert.NotNil(t, state)
	assert.Empty(t, state.Hashes)
}

func TestDepsStateGetSet(t *testing.T) {
	state := NewDepsState()

	state.SetHash("container1", "abc123")
	hash, ok := state.GetHash("container1")
	assert.True(t, ok)
	assert.Equal(t, "abc123", hash)
}

func TestDepsStateGetMissing(t *testing.T) {
	state := NewDepsState()

	hash, ok := state.GetHash("nonexistent")
	assert.False(t, ok)
	assert.Empty(t, hash)
}

func TestDepsChanged(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644)
	require.NoError(t, err)

	dm := NewDepsManager(nil)
	cfg := &DepsConfig{
		Command:  "go mod download",
		Triggers: []string{"go.mod"},
	}

	// First check - no previous hash
	changed, err := dm.NeedsSync(context.Background(), "container1", tmpDir, cfg)
	assert.NoError(t, err)
	assert.True(t, changed, "should need sync when no previous hash")

	// Record the hash
	hash, _ := dm.ComputeHash(tmpDir, cfg)
	dm.state.SetHash("container1", hash)

	// Second check - same hash
	changed, err = dm.NeedsSync(context.Background(), "container1", tmpDir, cfg)
	assert.NoError(t, err)
	assert.False(t, changed, "should not need sync when hash unchanged")

	// Modify file
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\nrequire foo v1.0\n"), 0644)
	require.NoError(t, err)

	// Third check - different hash
	changed, err = dm.NeedsSync(context.Background(), "container1", tmpDir, cfg)
	assert.NoError(t, err)
	assert.True(t, changed, "should need sync when hash changed")
}

func TestDepsSkip(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644)
	require.NoError(t, err)

	dm := NewDepsManager(nil)
	cfg := &DepsConfig{
		Command:  "go mod download",
		Triggers: []string{"go.mod"},
	}

	// Manually set hash to current state
	hash, _ := dm.ComputeHash(tmpDir, cfg)
	dm.state.SetHash("container1", hash)

	// Should skip sync
	changed, err := dm.NeedsSync(context.Background(), "container1", tmpDir, cfg)
	assert.NoError(t, err)
	assert.False(t, changed)
}

func TestDepsSyncResultSuccess(t *testing.T) {
	result := &DepsSyncResult{
		Success:  true,
		ExitCode: 0,
		Output:   "go: downloading...",
	}

	assert.True(t, result.Success)
	assert.Equal(t, 0, result.ExitCode)
}

func TestDepsSyncResultFailure(t *testing.T) {
	result := &DepsSyncResult{
		Success:  false,
		ExitCode: 1,
		Output:   "error: network unreachable",
		Error:    "network error",
	}

	assert.False(t, result.Success)
	assert.Equal(t, 1, result.ExitCode)
	assert.NotEmpty(t, result.Error)
}

func TestDepsStatePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "deps-state.json")

	// Create state and save
	state := NewDepsState()
	state.SetHash("container1", "hash1")
	state.SetHash("container2", "hash2")

	err := state.Save(stateFile)
	assert.NoError(t, err)

	// Load state
	loaded, err := LoadDepsState(stateFile)
	assert.NoError(t, err)

	hash1, ok := loaded.GetHash("container1")
	assert.True(t, ok)
	assert.Equal(t, "hash1", hash1)

	hash2, ok := loaded.GetHash("container2")
	assert.True(t, ok)
	assert.Equal(t, "hash2", hash2)
}

func TestDepsStateLoadMissing(t *testing.T) {
	// Loading missing file should return empty state
	state, err := LoadDepsState("/nonexistent/path/state.json")
	assert.NoError(t, err)
	assert.NotNil(t, state)
	assert.Empty(t, state.Hashes)
}

func TestDepsConfigCopy(t *testing.T) {
	cfg := &DepsConfig{
		Command:    "go mod download",
		Triggers:   []string{"go.mod", "go.sum"},
		WorkingDir: "/workspace",
	}

	copied := cfg.Copy()
	assert.Equal(t, cfg.Command, copied.Command)
	assert.Equal(t, cfg.Triggers, copied.Triggers)
	assert.Equal(t, cfg.WorkingDir, copied.WorkingDir)

	// Modifying copy should not affect original
	copied.Command = "npm install"
	assert.NotEqual(t, cfg.Command, copied.Command)
}
