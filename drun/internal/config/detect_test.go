package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestDetectGo(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test\n\ngo 1.22\n")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangGo, lang)
}

func TestDetectGoSum(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "go.sum", "github.com/something v1.0.0\n")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangGo, lang)
}

func TestDetectNode(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "package.json", `{"name": "test"}`)

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangNode, lang)
}

func TestDetectNodeYarn(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "yarn.lock", "# yarn lockfile")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangNode, lang)
}

func TestDetectNodePnpm(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "pnpm-lock.yaml", "lockfileVersion: 6.0")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangNode, lang)
}

func TestDetectPython(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "requirements.txt", "flask==2.0.0\n")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangPython, lang)
}

func TestDetectPythonPyproject(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "pyproject.toml", "[project]\nname = \"test\"")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangPython, lang)
}

func TestDetectPythonSetupPy(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "setup.py", "from setuptools import setup")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangPython, lang)
}

func TestDetectRust(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "Cargo.toml", "[package]\nname = \"test\"")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangRust, lang)
}

func TestDetectOdin(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "main.odin", "package main")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangOdin, lang)
}

func TestDetectOdinInSrc(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "src/main.odin", "package main")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangOdin, lang)
}

func TestDetectMultiple(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test")
	testutil.TempFileInDir(t, tmpDir, "package.json", `{"name": "test"}`)

	detector := NewDetector(tmpDir)

	// Detect returns first (Go has priority)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, LangGo, lang)

	// DetectAll returns both
	langs, err := detector.DetectAll()
	require.NoError(t, err)
	assert.Contains(t, langs, LangGo)
	assert.Contains(t, langs, LangNode)
}

func TestDetectNone(t *testing.T) {
	tmpDir := testutil.TempDir(t)

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)
	assert.Equal(t, Language(""), lang)
}

func TestDetectPriority(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	// Create multiple files
	testutil.TempFileInDir(t, tmpDir, "go.mod", "module test")
	testutil.TempFileInDir(t, tmpDir, "Cargo.toml", "[package]")
	testutil.TempFileInDir(t, tmpDir, "package.json", "{}")

	detector := NewDetector(tmpDir)
	lang, err := detector.Detect()
	require.NoError(t, err)

	// Go should have highest priority
	assert.Equal(t, LangGo, lang)
}

func TestLanguageString(t *testing.T) {
	assert.Equal(t, "go", string(LangGo))
	assert.Equal(t, "node", string(LangNode))
	assert.Equal(t, "python", string(LangPython))
	assert.Equal(t, "rust", string(LangRust))
	assert.Equal(t, "odin", string(LangOdin))
}
