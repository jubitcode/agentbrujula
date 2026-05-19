//go:build integration

package build

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"drun/internal/config"
	"drun/internal/runtime"
)

func TestBuildImageIntegration(t *testing.T) {
	ctx := context.Background()

	// Create runtime client
	client, err := runtime.NewClient(ctx, nil)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	// Create cache in temp dir
	cache, err := NewCache(t.TempDir())
	require.NoError(t, err)

	// Create builder with real Docker client
	builder := NewBuilder(client.Client, cache)

	// Create a simple config
	cfg := &config.Config{
		Image:   "alpine:latest",
		Workdir: "/app",
	}

	// Build with output
	var output bytes.Buffer
	opts := NewBuildOptions().
		WithOutput(&output).
		WithTags("drun-test:integration")

	result, err := builder.Build(ctx, cfg, opts)
	require.NoError(t, err, "Build failed. Output:\n%s", output.String())

	t.Logf("Build successful!")
	t.Logf("Image ID: %s", result.ImageID)
	t.Logf("Duration: %v", result.Duration)
	t.Logf("Output:\n%s", output.String())

	// Verify image ID format
	require.NotEmpty(t, result.ImageID)
	require.Contains(t, result.ImageID, "sha256:")
}

func TestBuildImageWithPackagesIntegration(t *testing.T) {
	ctx := context.Background()

	client, err := runtime.NewClient(ctx, nil)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer client.Close()

	cache, err := NewCache(t.TempDir())
	require.NoError(t, err)

	builder := NewBuilder(client.Client, cache)

	cfg := &config.Config{
		Image:   "alpine:latest",
		Workdir: "/app",
		Setup: &config.SetupConfig{
			Packages: []string{"curl", "jq"},
		},
	}

	var output bytes.Buffer
	opts := NewBuildOptions().
		WithOutput(&output).
		WithTags("drun-test:packages")

	result, err := builder.Build(ctx, cfg, opts)
	require.NoError(t, err, "Build failed. Output:\n%s", output.String())

	t.Logf("Image ID: %s", result.ImageID)
	t.Logf("Duration: %v", result.Duration)

	// Should contain package installation output
	require.Contains(t, output.String(), "apk")
}
