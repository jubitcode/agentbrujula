package build

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/config"
	"drun/internal/testutil"
)

func TestBuilderNew(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	builder := NewBuilder(nil, cache)
	assert.NotNil(t, builder)
}

func TestBuilderGenerateDockerfile(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	builder := NewBuilder(nil, cache)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
	}

	dockerfile, err := builder.GenerateDockerfile(cfg)
	require.NoError(t, err)

	assert.Contains(t, dockerfile, "FROM golang:1.22")
	assert.Contains(t, dockerfile, "WORKDIR /workspace")
}

func TestBuilderGenerateBuildKey(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	builder := NewBuilder(nil, cache)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
	}

	key1 := builder.GenerateBuildKey(cfg)
	assert.NotEmpty(t, key1)

	// Same config should produce same key
	key2 := builder.GenerateBuildKey(cfg)
	assert.Equal(t, key1, key2)

	// Different config should produce different key
	cfg.Image = "golang:1.23"
	key3 := builder.GenerateBuildKey(cfg)
	assert.NotEqual(t, key1, key3)
}

func TestBuilderDryRun(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	builder := NewBuilder(nil, cache)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Setup: &config.SetupConfig{
			Packages: []string{"git"},
		},
	}

	result, err := builder.DryRun(context.Background(), cfg)
	require.NoError(t, err)

	assert.Contains(t, result.Dockerfile, "FROM golang:1.22")
	assert.Contains(t, result.Dockerfile, "apt-get")
	assert.NotEmpty(t, result.BuildKey)
	assert.False(t, result.Cached)
}

func TestBuilderDryRunCached(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	builder := NewBuilder(nil, cache)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
	}

	// First dry run - not cached
	result1, err := builder.DryRun(ctx, cfg)
	require.NoError(t, err)
	assert.False(t, result1.Cached)

	// Simulate that the image was built by adding to cache
	err = cache.Put(ctx, &CacheEntry{
		Key:     result1.BuildKey,
		ImageID: "sha256:abc123",
	})
	require.NoError(t, err)

	// Second dry run - should be cached
	result2, err := builder.DryRun(ctx, cfg)
	require.NoError(t, err)
	assert.True(t, result2.Cached)
	assert.Equal(t, "sha256:abc123", result2.ImageID)
}

func TestBuildOptionsStruct(t *testing.T) {
	opts := &BuildOptions{
		NoCache:    true,
		ForceBuild: true,
	}

	assert.True(t, opts.NoCache)
	assert.True(t, opts.ForceBuild)
}

func TestBuildOptionsWithOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	opts := &BuildOptions{Output: buf}
	assert.Equal(t, buf, opts.Output)
}

func TestBuildResultStruct(t *testing.T) {
	result := &BuildResult{
		ImageID:  "sha256:abc123def456",
		BuildKey: "key123",
		Cached:   false,
	}

	assert.Equal(t, "sha256:abc123def456", result.ImageID)
	assert.Equal(t, "key123", result.BuildKey)
	assert.False(t, result.Cached)

	result.Cached = true
	assert.True(t, result.Cached)
}

func TestDryRunResultStruct(t *testing.T) {
	result := &DryRunResult{
		Dockerfile: "FROM golang:1.22",
		BuildKey:   "key123",
		Cached:     false,
	}

	assert.Contains(t, result.Dockerfile, "FROM golang:1.22")
	assert.Equal(t, "key123", result.BuildKey)
	assert.False(t, result.Cached)

	result.Cached = true
	result.ImageID = "sha256:cached123"
	assert.True(t, result.Cached)
	assert.Equal(t, "sha256:cached123", result.ImageID)
}

func TestBuilderGenerateDockerfileWithSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	builder := NewBuilder(nil, cache)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
		Setup: &config.SetupConfig{
			Steps: []config.SetupStep{
				{Name: "tools", Run: "go install tool@latest"},
				{Name: "gen", Run: "go generate ./...", DependsOn: []string{"tools"}},
			},
		},
	}

	dockerfile, err := builder.GenerateDockerfile(cfg)
	require.NoError(t, err)

	// Steps should be in dependency order
	toolsIdx := bytes.Index([]byte(dockerfile), []byte("tool@latest"))
	genIdx := bytes.Index([]byte(dockerfile), []byte("go generate"))
	assert.Less(t, toolsIdx, genIdx)
}

func TestBuilderIsCached(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	builder := NewBuilder(nil, cache)

	cfg := &config.Config{
		Image:   "golang:1.22",
		Workdir: "/workspace",
	}

	// Not cached initially
	cached, imageID := builder.IsCached(ctx, cfg)
	assert.False(t, cached)
	assert.Empty(t, imageID)

	// Add to cache
	buildKey := builder.GenerateBuildKey(cfg)
	err = cache.Put(ctx, &CacheEntry{
		Key:     buildKey,
		ImageID: "sha256:test123",
	})
	require.NoError(t, err)

	// Now should be cached
	cached, imageID = builder.IsCached(ctx, cfg)
	assert.True(t, cached)
	assert.Equal(t, "sha256:test123", imageID)
}

func TestParseBuildOutputSuccess(t *testing.T) {
	// Simulate Docker build JSON stream with successful build
	input := `{"stream":"Step 1/3 : FROM golang:1.22\n"}
{"stream":" ---\u003e abc123\n"}
{"stream":"Step 2/3 : WORKDIR /workspace\n"}
{"stream":" ---\u003e def456\n"}
{"stream":"Successfully built abc123def456\n"}
{"aux":{"ID":"sha256:abc123def456789"}}
`
	var output bytes.Buffer
	reader := bytes.NewReader([]byte(input))

	imageID, err := parseBuildOutput(reader, &output)
	require.NoError(t, err)
	assert.Equal(t, "sha256:abc123def456789", imageID)

	// Output should contain the stream content
	assert.Contains(t, output.String(), "Step 1/3")
	assert.Contains(t, output.String(), "Successfully built")
}

func TestParseBuildOutputError(t *testing.T) {
	// Simulate Docker build JSON stream with error
	input := `{"stream":"Step 1/2 : FROM nonexistent:image\n"}
{"errorDetail":{"message":"pull access denied for nonexistent"},"error":"pull access denied for nonexistent"}
`
	var output bytes.Buffer
	reader := bytes.NewReader([]byte(input))

	imageID, err := parseBuildOutput(reader, &output)
	assert.Error(t, err)
	assert.Empty(t, imageID)
	assert.Contains(t, err.Error(), "pull access denied")
}

func TestParseBuildOutputStreamsToWriter(t *testing.T) {
	input := `{"stream":"Building layer 1\n"}
{"stream":"Building layer 2\n"}
{"stream":"Building layer 3\n"}
{"aux":{"ID":"sha256:final123"}}
`
	var output bytes.Buffer
	reader := bytes.NewReader([]byte(input))

	_, err := parseBuildOutput(reader, &output)
	require.NoError(t, err)

	// All stream content should be in output
	assert.Contains(t, output.String(), "Building layer 1")
	assert.Contains(t, output.String(), "Building layer 2")
	assert.Contains(t, output.String(), "Building layer 3")
}

func TestParseBuildOutputNilWriter(t *testing.T) {
	input := `{"stream":"Some output\n"}
{"aux":{"ID":"sha256:test123"}}
`
	reader := bytes.NewReader([]byte(input))

	// Should work with nil output writer
	imageID, err := parseBuildOutput(reader, nil)
	require.NoError(t, err)
	assert.Equal(t, "sha256:test123", imageID)
}

func TestParseBuildOutputMalformedJSON(t *testing.T) {
	input := `{"stream":"Valid line\n"}
not valid json
{"aux":{"ID":"sha256:test123"}}
`
	var output bytes.Buffer
	reader := bytes.NewReader([]byte(input))

	// Should skip malformed lines and still succeed
	imageID, err := parseBuildOutput(reader, &output)
	require.NoError(t, err)
	assert.Equal(t, "sha256:test123", imageID)
	assert.Contains(t, output.String(), "Valid line")
}

func TestParseBuildOutputNoImageID(t *testing.T) {
	input := `{"stream":"Step 1/1 : FROM alpine\n"}
{"stream":"Successfully built but missing aux\n"}
`
	var output bytes.Buffer
	reader := bytes.NewReader([]byte(input))

	imageID, err := parseBuildOutput(reader, &output)
	assert.Error(t, err)
	assert.Empty(t, imageID)
	assert.Contains(t, err.Error(), "no image ID found")
}

func TestParseBuildOutputWithStatus(t *testing.T) {
	// Status messages are used during pull operations
	input := `{"status":"Pulling from library/golang"}
{"status":"Digest: sha256:abc123"}
{"status":"Status: Downloaded newer image"}
{"stream":"Step 1/1 : FROM golang:1.22\n"}
{"aux":{"ID":"sha256:built123"}}
`
	var output bytes.Buffer
	reader := bytes.NewReader([]byte(input))

	imageID, err := parseBuildOutput(reader, &output)
	require.NoError(t, err)
	assert.Equal(t, "sha256:built123", imageID)

	// Status messages should be in output
	assert.Contains(t, output.String(), "Pulling from library/golang")
}
