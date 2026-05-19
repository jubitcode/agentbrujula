package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientWithRuntime(t *testing.T) {
	// Skip if no Docker runtime available
	detector := NewDetector(nil)
	rt, err := detector.Detect(context.Background())
	if err != nil {
		t.Skip("No Docker runtime available")
	}

	client, err := NewClient(context.Background(), &ClientOptions{
		Runtime: rt,
	})
	require.NoError(t, err)
	defer client.Close()

	assert.NotNil(t, client.Client)
	assert.Equal(t, rt.Type, client.Runtime.Type)
}

func TestNewClientAutoDetect(t *testing.T) {
	// Skip if no Docker runtime available
	detector := NewDetector(nil)
	if _, err := detector.Detect(context.Background()); err != nil {
		t.Skip("No Docker runtime available")
	}

	client, err := NewClient(context.Background(), nil)
	require.NoError(t, err)
	defer client.Close()

	assert.NotNil(t, client.Client)
	assert.NotNil(t, client.Runtime)
}

func TestClientPing(t *testing.T) {
	// Skip if no Docker runtime available
	detector := NewDetector(nil)
	if _, err := detector.Detect(context.Background()); err != nil {
		t.Skip("No Docker runtime available")
	}

	client, err := NewClient(context.Background(), nil)
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Ping(ctx)
	assert.NoError(t, err)
}

func TestNewClientWithInvalidRuntime(t *testing.T) {
	rt := &Runtime{
		Type:       RuntimeCustom,
		SocketPath: "/nonexistent/socket.sock",
		Host:       "unix:///nonexistent/socket.sock",
	}

	_, err := NewClient(context.Background(), &ClientOptions{
		Runtime: rt,
	})

	assert.Error(t, err)
}

func TestClientAPIVersionNegotiation(t *testing.T) {
	// Skip if no Docker runtime available
	detector := NewDetector(nil)
	if _, err := detector.Detect(context.Background()); err != nil {
		t.Skip("No Docker runtime available")
	}

	client, err := NewClient(context.Background(), nil)
	require.NoError(t, err)
	defer client.Close()

	// API version should be negotiated (not empty)
	version := client.ClientVersion()
	assert.NotEmpty(t, version)
}

func TestClientClose(t *testing.T) {
	// Skip if no Docker runtime available
	detector := NewDetector(nil)
	if _, err := detector.Detect(context.Background()); err != nil {
		t.Skip("No Docker runtime available")
	}

	client, err := NewClient(context.Background(), nil)
	require.NoError(t, err)

	err = client.Close()
	assert.NoError(t, err)
}

func TestNewClientNoRuntime(t *testing.T) {
	// Test with options that will fail to find a runtime
	opts := &ClientOptions{
		DetectOptions: &DetectOptions{
			SkipEnv:             true,
			OrbStackSocket:      "/nonexistent/orbstack.sock",
			ColimaSocket:        "/nonexistent/colima.sock",
			DockerDesktopSocket: "/nonexistent/docker.sock",
		},
	}

	_, err := NewClient(context.Background(), opts)
	assert.ErrorIs(t, err, ErrRuntimeNotFound)
}
