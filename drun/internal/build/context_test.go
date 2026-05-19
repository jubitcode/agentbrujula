package build

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestCreateBuildContextDockerfileOnly(t *testing.T) {
	dockerfile := `FROM golang:1.22
WORKDIR /workspace
`
	reader, err := CreateBuildContext(dockerfile, "")
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Read and verify tar contents
	files := extractTarFiles(t, reader)

	// Should only contain Dockerfile
	require.Len(t, files, 1)
	assert.Contains(t, files, "Dockerfile")
	assert.Equal(t, dockerfile, files["Dockerfile"])
}

func TestCreateBuildContextWithFiles(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Create test files
	testutil.TempFileInDir(t, tmpDir, "main.go", "package main\n\nfunc main() {}\n")
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test\n\ngo 1.22\n")

	// Create subdirectory with file
	subDir := filepath.Join(tmpDir, "pkg")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)
	testutil.TempFileInDir(t, subDir, "util.go", "package pkg\n")

	dockerfile := "FROM golang:1.22\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	files := extractTarFiles(t, reader)

	// Should contain Dockerfile and project files
	assert.Contains(t, files, "Dockerfile")
	assert.Contains(t, files, "main.go")
	assert.Contains(t, files, "go.mod")
	assert.Contains(t, files, "pkg/util.go")
}

func TestCreateBuildContextRespectsDockerignore(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Create .dockerignore
	testutil.TempFileInDir(t, tmpDir, ".dockerignore", "node_modules\n*.log\n")

	// Create files - some should be excluded
	testutil.TempFileInDir(t, tmpDir, "main.go", "package main")
	testutil.TempFileInDir(t, tmpDir, "debug.log", "log content")

	// Create node_modules directory
	nodeModules := filepath.Join(tmpDir, "node_modules")
	err := os.MkdirAll(nodeModules, 0755)
	require.NoError(t, err)
	testutil.TempFileInDir(t, nodeModules, "package.json", "{}")

	dockerfile := "FROM node:18\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	files := extractTarFiles(t, reader)

	// Should have Dockerfile and main.go
	assert.Contains(t, files, "Dockerfile")
	assert.Contains(t, files, "main.go")
	assert.Contains(t, files, ".dockerignore") // dockerignore itself is included

	// Should NOT have excluded files
	assert.NotContains(t, files, "debug.log")
	assert.NotContains(t, files, "node_modules/package.json")
}

func TestCreateBuildContextExcludesDefaultDirs(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Create test files
	testutil.TempFileInDir(t, tmpDir, "main.go", "package main")

	// Create .git directory (should always be excluded)
	gitDir := filepath.Join(tmpDir, ".git")
	err := os.MkdirAll(gitDir, 0755)
	require.NoError(t, err)
	testutil.TempFileInDir(t, gitDir, "config", "git config")

	dockerfile := "FROM golang:1.22\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	files := extractTarFiles(t, reader)

	// Should have main.go
	assert.Contains(t, files, "main.go")

	// Should NOT have .git
	assert.NotContains(t, files, ".git/config")
}

func TestCreateBuildContextEmptyProjectDir(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Empty directory - should still work
	dockerfile := "FROM alpine:latest\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	files := extractTarFiles(t, reader)

	// Should only contain Dockerfile
	assert.Contains(t, files, "Dockerfile")
}

func TestCreateBuildContextPreservesPermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Create executable script
	scriptPath := filepath.Join(tmpDir, "run.sh")
	err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hello\n"), 0755)
	require.NoError(t, err)

	dockerfile := "FROM alpine:latest\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	// Extract and check permissions
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		if hdr.Name == "run.sh" {
			// Should preserve executable bit
			assert.True(t, hdr.Mode&0100 != 0, "run.sh should be executable")
			return
		}
	}
	t.Fatal("run.sh not found in tar")
}

func TestCreateBuildContextLargeFile(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Create a larger file (1MB)
	largeContent := bytes.Repeat([]byte("x"), 1024*1024)
	err := os.WriteFile(filepath.Join(tmpDir, "large.bin"), largeContent, 0644)
	require.NoError(t, err)

	dockerfile := "FROM alpine:latest\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	files := extractTarFiles(t, reader)

	assert.Contains(t, files, "large.bin")
	assert.Len(t, files["large.bin"], 1024*1024)
}

func TestCreateBuildContextSymlinks(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Create a file
	testutil.TempFileInDir(t, tmpDir, "original.txt", "content")

	// Create symlink to file
	linkPath := filepath.Join(tmpDir, "link.txt")
	err := os.Symlink("original.txt", linkPath)
	require.NoError(t, err)

	dockerfile := "FROM alpine:latest\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	files := extractTarFiles(t, reader)

	// Should contain both original and link
	assert.Contains(t, files, "original.txt")
	// Symlinks should be followed or preserved
	assert.Contains(t, files, "link.txt")
}

func TestCreateBuildContextNestedDirectories(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Create deeply nested structure
	deepPath := filepath.Join(tmpDir, "a", "b", "c", "d")
	err := os.MkdirAll(deepPath, 0755)
	require.NoError(t, err)
	testutil.TempFileInDir(t, deepPath, "deep.txt", "deep content")

	dockerfile := "FROM alpine:latest\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	files := extractTarFiles(t, reader)

	assert.Contains(t, files, "a/b/c/d/deep.txt")
	assert.Equal(t, "deep content", files["a/b/c/d/deep.txt"])
}

func TestCreateBuildContextWithDrunDir(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// Create .drun directory (should be excluded by default)
	drunDir := filepath.Join(tmpDir, ".drun")
	err := os.MkdirAll(drunDir, 0755)
	require.NoError(t, err)
	testutil.TempFileInDir(t, drunDir, "cache.json", "{}")

	testutil.TempFileInDir(t, tmpDir, "main.go", "package main")

	dockerfile := "FROM golang:1.22\n"
	reader, err := CreateBuildContext(dockerfile, tmpDir)
	require.NoError(t, err)

	files := extractTarFiles(t, reader)

	assert.Contains(t, files, "main.go")
	assert.NotContains(t, files, ".drun/cache.json")
}

// Helper function to extract tar files into a map
func extractTarFiles(t *testing.T, reader io.Reader) map[string]string {
	t.Helper()

	files := make(map[string]string)
	tr := tar.NewReader(reader)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeSymlink {
			var content bytes.Buffer
			if hdr.Typeflag == tar.TypeReg {
				_, err := io.Copy(&content, tr)
				require.NoError(t, err)
			} else {
				// For symlinks, store the link target
				content.WriteString(hdr.Linkname)
			}
			files[hdr.Name] = content.String()
		}
	}

	return files
}
