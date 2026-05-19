package pathmap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestMapWorkingDir(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	mapper := NewMapper([]Mount{
		{Source: tmpDir, Target: "/workspace"},
	})

	containerPath, err := mapper.ToContainer(filepath.Join(tmpDir, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, "/workspace/main.go", containerPath)
}

func TestMapSubdirectory(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	})

	containerPath, err := mapper.ToContainer("/Users/dev/project/cmd/app/main.go")
	require.NoError(t, err)
	assert.Equal(t, "/workspace/cmd/app/main.go", containerPath)
}

func TestMapOutsideMount(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	})

	_, err := mapper.ToContainer("/Users/dev/other/file.go")
	assert.ErrorIs(t, err, ErrPathNotMapped)
}

func TestMapWithMultipleMounts(t *testing.T) {
	home, _ := os.UserHomeDir()
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
		{Source: filepath.Join(home, ".cache/go"), Target: "/go/pkg/mod"},
	})

	// Test project path
	containerPath, err := mapper.ToContainer("/Users/dev/project/main.go")
	require.NoError(t, err)
	assert.Equal(t, "/workspace/main.go", containerPath)

	// Test cache path
	containerPath, err = mapper.ToContainer(filepath.Join(home, ".cache/go/pkg/mod/github.com"))
	require.NoError(t, err)
	assert.Equal(t, "/go/pkg/mod/pkg/mod/github.com", containerPath)
}

func TestMapMountPriority(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev", Target: "/home/dev"},
		{Source: "/Users/dev/project", Target: "/workspace"},
	})

	// More specific mount should be used
	containerPath, err := mapper.ToContainer("/Users/dev/project/main.go")
	require.NoError(t, err)
	assert.Equal(t, "/workspace/main.go", containerPath)

	// Less specific path uses less specific mount
	containerPath, err = mapper.ToContainer("/Users/dev/other/file.go")
	require.NoError(t, err)
	assert.Equal(t, "/home/dev/other/file.go", containerPath)
}

func TestToHost(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	})

	hostPath, err := mapper.ToHost("/workspace/cmd/main.go")
	require.NoError(t, err)
	assert.Equal(t, "/Users/dev/project/cmd/main.go", hostPath)
}

func TestToHostOutsideMount(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	})

	_, err := mapper.ToHost("/etc/passwd")
	assert.ErrorIs(t, err, ErrPathNotMapped)
}

func TestIsInMount(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	})

	assert.True(t, mapper.IsInMount("/Users/dev/project/main.go"))
	assert.True(t, mapper.IsInMount("/Users/dev/project"))
	assert.False(t, mapper.IsInMount("/Users/dev/other/main.go"))
}

func TestRelativeToWorkdir(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	}, WithWorkdir("/workspace"))

	relPath, err := mapper.RelativeToWorkdir("/workspace/cmd/main.go")
	require.NoError(t, err)
	assert.Equal(t, "cmd/main.go", relPath)
}

func TestMapTildeExpansion(t *testing.T) {
	home, _ := os.UserHomeDir()

	mapper := NewMapper([]Mount{
		{Source: "~/.cache/go", Target: "/go/pkg/mod"},
	})

	containerPath, err := mapper.ToContainer(filepath.Join(home, ".cache/go/module"))
	require.NoError(t, err)
	assert.Equal(t, "/go/pkg/mod/module", containerPath)
}

func TestMapDotExpansion(t *testing.T) {
	cwd, _ := os.Getwd()

	mapper := NewMapper([]Mount{
		{Source: ".", Target: "/workspace"},
	})

	containerPath, err := mapper.ToContainer(filepath.Join(cwd, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, "/workspace/main.go", containerPath)
}

func TestMapperWithWorkdir(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	}, WithWorkdir("/app"))

	assert.Equal(t, "/app", mapper.Workdir())
}

func TestMapperWithHostRoot(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	}, WithHostRoot("/Users/dev/project"))

	// Relative paths should resolve against host root
	containerPath, err := mapper.ToContainer("cmd/main.go")
	require.NoError(t, err)
	assert.Equal(t, "/workspace/cmd/main.go", containerPath)
}

func TestMapRootMount(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	})

	// Mapping the exact mount point should work
	containerPath, err := mapper.ToContainer("/Users/dev/project")
	require.NoError(t, err)
	assert.Equal(t, "/workspace", containerPath)
}

func TestToHostRootMount(t *testing.T) {
	mapper := NewMapper([]Mount{
		{Source: "/Users/dev/project", Target: "/workspace"},
	})

	// Mapping the exact mount point should work
	hostPath, err := mapper.ToHost("/workspace")
	require.NoError(t, err)
	assert.Equal(t, "/Users/dev/project", hostPath)
}

func TestMapperNoMounts(t *testing.T) {
	mapper := NewMapper(nil)

	_, err := mapper.ToContainer("/some/path")
	assert.ErrorIs(t, err, ErrPathNotMapped)
}

func TestMapperEmptyMounts(t *testing.T) {
	mapper := NewMapper([]Mount{})

	_, err := mapper.ToContainer("/some/path")
	assert.ErrorIs(t, err, ErrPathNotMapped)
}
