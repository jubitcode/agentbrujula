package build

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestParseDockerignoreEmpty(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	// No .dockerignore file exists
	patterns, err := ParseDockerignore(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, patterns)
}

func TestParseDockerignoreComments(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".dockerignore", `# This is a comment
node_modules
# Another comment
.git
`)

	patterns, err := ParseDockerignore(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, []string{"node_modules", ".git"}, patterns)
}

func TestParseDockerignoreEmptyLines(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".dockerignore", `node_modules

.git

vendor
`)

	patterns, err := ParseDockerignore(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, []string{"node_modules", ".git", "vendor"}, patterns)
}

func TestParseDockerignoreWhitespace(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".dockerignore", `  node_modules
	.git
vendor
`)

	patterns, err := ParseDockerignore(tmpDir)
	require.NoError(t, err)

	// Patterns should be trimmed
	assert.Equal(t, []string{"node_modules", ".git", "vendor"}, patterns)
}

func TestParseDockerignoreNegation(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".dockerignore", `*.log
!important.log
`)

	patterns, err := ParseDockerignore(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, []string{"*.log", "!important.log"}, patterns)
}

func TestMatchesPatternExact(t *testing.T) {
	patterns := []string{"node_modules", ".git", "vendor"}

	assert.True(t, MatchesPattern("node_modules", patterns))
	assert.True(t, MatchesPattern(".git", patterns))
	assert.True(t, MatchesPattern("vendor", patterns))
	assert.False(t, MatchesPattern("src", patterns))
	assert.False(t, MatchesPattern("main.go", patterns))
}

func TestMatchesPatternWildcardStar(t *testing.T) {
	patterns := []string{"*.log", "*.tmp"}

	assert.True(t, MatchesPattern("debug.log", patterns))
	assert.True(t, MatchesPattern("error.log", patterns))
	assert.True(t, MatchesPattern("cache.tmp", patterns))
	assert.False(t, MatchesPattern("main.go", patterns))
	assert.False(t, MatchesPattern("README.md", patterns))
}

func TestMatchesPatternDoubleStar(t *testing.T) {
	patterns := []string{"**/cache", "**/*.log"}

	// Matches at any depth
	assert.True(t, MatchesPattern("cache", patterns))
	assert.True(t, MatchesPattern("tmp/cache", patterns))
	assert.True(t, MatchesPattern("a/b/c/cache", patterns))
	assert.True(t, MatchesPattern("debug.log", patterns))
	assert.True(t, MatchesPattern("logs/debug.log", patterns))
	assert.True(t, MatchesPattern("a/b/debug.log", patterns))
}

func TestMatchesPatternDirectory(t *testing.T) {
	patterns := []string{"build/", "dist/"}

	// Directory patterns match directories
	assert.True(t, MatchesPattern("build", patterns))
	assert.True(t, MatchesPattern("dist", patterns))
	// Also matches with trailing slash
	assert.True(t, MatchesPattern("build/", patterns))
}

func TestMatchesPatternNested(t *testing.T) {
	patterns := []string{"vendor/github.com"}

	assert.True(t, MatchesPattern("vendor/github.com", patterns))
	assert.False(t, MatchesPattern("vendor", patterns))
	assert.False(t, MatchesPattern("github.com", patterns))
}

func TestMatchesPatternNegation(t *testing.T) {
	patterns := []string{"*.log", "!important.log"}

	// Negation pattern excludes from previous match
	assert.True(t, MatchesPattern("debug.log", patterns))
	assert.True(t, MatchesPattern("error.log", patterns))
	assert.False(t, MatchesPattern("important.log", patterns)) // negated
}

func TestMatchesPatternQuestionMark(t *testing.T) {
	patterns := []string{"file?.txt"}

	assert.True(t, MatchesPattern("file1.txt", patterns))
	assert.True(t, MatchesPattern("fileA.txt", patterns))
	assert.False(t, MatchesPattern("file12.txt", patterns)) // ? matches single char
	assert.False(t, MatchesPattern("file.txt", patterns))
}

func TestMatchesPatternSubdirectory(t *testing.T) {
	patterns := []string{"node_modules"}

	// Should match node_modules anywhere
	assert.True(t, MatchesPattern("node_modules", patterns))
	assert.True(t, MatchesPattern("frontend/node_modules", patterns))
	assert.True(t, MatchesPattern("packages/ui/node_modules", patterns))
}

func TestMatchesPatternEmpty(t *testing.T) {
	patterns := []string{}

	// Empty patterns should match nothing
	assert.False(t, MatchesPattern("anything", patterns))
	assert.False(t, MatchesPattern(".git", patterns))
}

func TestMatchesPatternRelativePath(t *testing.T) {
	patterns := []string{"./local"}

	// Relative paths with ./ prefix
	assert.True(t, MatchesPattern("local", patterns))
}

func TestMatchesPatternComplexNegation(t *testing.T) {
	patterns := []string{
		"*.log",
		"!important.log",
		"debug.log",
	}

	assert.True(t, MatchesPattern("error.log", patterns))      // matched by *.log
	assert.False(t, MatchesPattern("important.log", patterns)) // negated
	assert.True(t, MatchesPattern("debug.log", patterns))      // re-excluded explicitly
}

func TestIsExcludedByDockerignore(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".dockerignore", `node_modules
.git
*.log
!keep.log
`)

	patterns, err := ParseDockerignore(tmpDir)
	require.NoError(t, err)

	// Create some test files/dirs
	testutil.TempFileInDir(t, tmpDir, "main.go", "package main")
	testutil.TempFileInDir(t, tmpDir, "debug.log", "log content")
	testutil.TempFileInDir(t, tmpDir, "keep.log", "important log")

	// Test exclusions
	assert.True(t, MatchesPattern("node_modules", patterns))
	assert.True(t, MatchesPattern(".git", patterns))
	assert.True(t, MatchesPattern("debug.log", patterns))
	assert.False(t, MatchesPattern("keep.log", patterns))
	assert.False(t, MatchesPattern("main.go", patterns))
}

func TestParseDockerignorePath(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, ".dockerignore", "node_modules\n")

	// Verify file was created at correct path
	ignorePath := filepath.Join(tmpDir, ".dockerignore")
	assert.FileExists(t, ignorePath)

	patterns, err := ParseDockerignore(tmpDir)
	require.NoError(t, err)
	assert.Contains(t, patterns, "node_modules")
}
