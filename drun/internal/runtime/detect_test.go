package runtime

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestDetectFromEnv(t *testing.T) {
	testutil.SetEnv(t, "DOCKER_HOST", "unix:///custom/docker.sock")

	detector := NewDetector(nil)
	runtime, err := detector.Detect(context.Background())

	require.NoError(t, err)
	assert.Equal(t, RuntimeCustom, runtime.Type)
	assert.Equal(t, "unix:///custom/docker.sock", runtime.Host)
}

func TestDetectFromEnvTCP(t *testing.T) {
	testutil.SetEnv(t, "DOCKER_HOST", "tcp://192.168.1.100:2375")

	detector := NewDetector(nil)
	runtime, err := detector.Detect(context.Background())

	require.NoError(t, err)
	assert.Equal(t, RuntimeCustom, runtime.Type)
	assert.Equal(t, "tcp://192.168.1.100:2375", runtime.Host)
}

func TestDetectOrbStack(t *testing.T) {
	testutil.UnsetEnv(t, "DOCKER_HOST")

	tmpDir := testutil.TempDir(t)
	socketPath := filepath.Join(tmpDir, ".orbstack", "run", "docker.sock")

	// Create a real Unix socket
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0755))
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	detector := NewDetector(&DetectOptions{
		OrbStackSocket: socketPath,
	})
	runtime, err := detector.Detect(context.Background())

	require.NoError(t, err)
	assert.Equal(t, RuntimeOrbStack, runtime.Type)
	assert.Equal(t, socketPath, runtime.SocketPath)
	assert.Equal(t, "unix://"+socketPath, runtime.Host)
}

func TestDetectColima(t *testing.T) {
	testutil.UnsetEnv(t, "DOCKER_HOST")

	tmpDir := testutil.TempDir(t)
	socketPath := filepath.Join(tmpDir, ".colima", "default", "docker.sock")

	// Create a real Unix socket
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0755))
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	detector := NewDetector(&DetectOptions{
		OrbStackSocket:      "/nonexistent/orbstack.sock", // Disable OrbStack
		ColimaSocket:        socketPath,
		DockerDesktopSocket: "/nonexistent/docker.sock",
	})
	runtime, err := detector.Detect(context.Background())

	require.NoError(t, err)
	assert.Equal(t, RuntimeColima, runtime.Type)
	assert.Equal(t, socketPath, runtime.SocketPath)
}

func TestDetectDockerDesktop(t *testing.T) {
	testutil.UnsetEnv(t, "DOCKER_HOST")

	tmpDir := testutil.TempDir(t)
	socketPath := filepath.Join(tmpDir, "docker.sock")

	// Create a real Unix socket
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	detector := NewDetector(&DetectOptions{
		OrbStackSocket:      "/nonexistent/orbstack.sock", // Disable OrbStack
		ColimaSocket:        "/nonexistent/colima.sock",   // Disable Colima
		DockerDesktopSocket: socketPath,
	})
	runtime, err := detector.Detect(context.Background())

	require.NoError(t, err)
	assert.Equal(t, RuntimeDockerDesktop, runtime.Type)
	assert.Equal(t, socketPath, runtime.SocketPath)
}

func TestDetectPriority(t *testing.T) {
	// When DOCKER_HOST is set, it takes priority over all sockets
	testutil.SetEnv(t, "DOCKER_HOST", "tcp://localhost:2375")

	tmpDir := testutil.TempDir(t)
	orbSocket := filepath.Join(tmpDir, "orbstack.sock")
	colimaSocket := filepath.Join(tmpDir, "colima.sock")

	// Create sockets
	orbListener, err := net.Listen("unix", orbSocket)
	require.NoError(t, err)
	defer orbListener.Close()

	colimaListener, err := net.Listen("unix", colimaSocket)
	require.NoError(t, err)
	defer colimaListener.Close()

	detector := NewDetector(&DetectOptions{
		OrbStackSocket: orbSocket,
		ColimaSocket:   colimaSocket,
	})
	runtime, err := detector.Detect(context.Background())

	require.NoError(t, err)
	assert.Equal(t, RuntimeCustom, runtime.Type, "DOCKER_HOST should take priority")
}

func TestDetectPriorityOrbStackOverColima(t *testing.T) {
	testutil.UnsetEnv(t, "DOCKER_HOST")

	tmpDir := testutil.TempDir(t)
	orbSocket := filepath.Join(tmpDir, "orbstack.sock")
	colimaSocket := filepath.Join(tmpDir, "colima.sock")

	// Create both sockets
	orbListener, err := net.Listen("unix", orbSocket)
	require.NoError(t, err)
	defer orbListener.Close()

	colimaListener, err := net.Listen("unix", colimaSocket)
	require.NoError(t, err)
	defer colimaListener.Close()

	detector := NewDetector(&DetectOptions{
		OrbStackSocket: orbSocket,
		ColimaSocket:   colimaSocket,
	})
	runtime, err := detector.Detect(context.Background())

	require.NoError(t, err)
	assert.Equal(t, RuntimeOrbStack, runtime.Type, "OrbStack should take priority over Colima")
}

func TestDetectNone(t *testing.T) {
	testutil.UnsetEnv(t, "DOCKER_HOST")

	detector := NewDetector(&DetectOptions{
		OrbStackSocket:      "/nonexistent/orbstack.sock",
		ColimaSocket:        "/nonexistent/colima.sock",
		DockerDesktopSocket: "/nonexistent/docker.sock",
	})
	_, err := detector.Detect(context.Background())

	assert.ErrorIs(t, err, ErrRuntimeNotFound)
}

func TestDetectAll(t *testing.T) {
	testutil.UnsetEnv(t, "DOCKER_HOST")

	tmpDir := testutil.TempDir(t)
	orbSocket := filepath.Join(tmpDir, "orbstack.sock")
	colimaSocket := filepath.Join(tmpDir, "colima.sock")

	// Create both sockets
	orbListener, err := net.Listen("unix", orbSocket)
	require.NoError(t, err)
	defer orbListener.Close()

	colimaListener, err := net.Listen("unix", colimaSocket)
	require.NoError(t, err)
	defer colimaListener.Close()

	detector := NewDetector(&DetectOptions{
		OrbStackSocket: orbSocket,
		ColimaSocket:   colimaSocket,
	})
	runtimes, err := detector.DetectAll(context.Background())

	require.NoError(t, err)
	assert.Len(t, runtimes, 2)

	types := make([]RuntimeType, len(runtimes))
	for i, r := range runtimes {
		types[i] = r.Type
	}
	assert.Contains(t, types, RuntimeOrbStack)
	assert.Contains(t, types, RuntimeColima)
}

func TestDetectSkipEnv(t *testing.T) {
	testutil.SetEnv(t, "DOCKER_HOST", "tcp://localhost:2375")

	tmpDir := testutil.TempDir(t)
	orbSocket := filepath.Join(tmpDir, "orbstack.sock")

	listener, err := net.Listen("unix", orbSocket)
	require.NoError(t, err)
	defer listener.Close()

	detector := NewDetector(&DetectOptions{
		SkipEnv:        true,
		OrbStackSocket: orbSocket,
	})
	runtime, err := detector.Detect(context.Background())

	require.NoError(t, err)
	assert.Equal(t, RuntimeOrbStack, runtime.Type, "should skip DOCKER_HOST when SkipEnv is true")
}

func TestDetectSocketNotAccessible(t *testing.T) {
	testutil.UnsetEnv(t, "DOCKER_HOST")

	tmpDir := testutil.TempDir(t)
	// Create a regular file, not a socket
	socketPath := filepath.Join(tmpDir, "not-a-socket")
	require.NoError(t, os.WriteFile(socketPath, []byte{}, 0644))

	detector := NewDetector(&DetectOptions{
		OrbStackSocket:      socketPath,
		ColimaSocket:        "/nonexistent/colima.sock",
		DockerDesktopSocket: "/nonexistent/docker.sock",
	})
	_, err := detector.Detect(context.Background())

	// Should fail because the file is not a socket
	assert.ErrorIs(t, err, ErrRuntimeNotFound)
}

func TestRuntimeString(t *testing.T) {
	tests := []struct {
		rt       RuntimeType
		expected string
	}{
		{RuntimeOrbStack, "orbstack"},
		{RuntimeColima, "colima"},
		{RuntimeDockerDesktop, "docker-desktop"},
		{RuntimeCustom, "custom"},
		{RuntimeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.rt), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.rt))
		})
	}
}
