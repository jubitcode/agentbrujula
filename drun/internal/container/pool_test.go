package container

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/config"
	"drun/internal/testutil"
)

func TestPoolNew(t *testing.T) {
	pool := NewPool(nil)
	assert.NotNil(t, pool)
}

func TestPoolKeyGeneration(t *testing.T) {
	pool := NewPool(nil)

	cfg := &config.Config{
		Image:      "golang:1.22",
		ProjectDir: "/home/user/myproject",
	}

	key := pool.GenerateKey(cfg, "main")
	assert.NotEmpty(t, key)
	assert.Contains(t, key, "drun-")
}

func TestPoolKeyDifferentProjects(t *testing.T) {
	pool := NewPool(nil)

	cfg1 := &config.Config{
		Image:      "golang:1.22",
		ProjectDir: "/home/user/project1",
	}

	cfg2 := &config.Config{
		Image:      "golang:1.22",
		ProjectDir: "/home/user/project2",
	}

	key1 := pool.GenerateKey(cfg1, "main")
	key2 := pool.GenerateKey(cfg2, "main")

	assert.NotEqual(t, key1, key2)
}

func TestPoolKeyBranchIsolation(t *testing.T) {
	pool := NewPool(nil)

	cfg := &config.Config{
		Image:      "golang:1.22",
		ProjectDir: "/home/user/myproject",
	}

	keyMain := pool.GenerateKey(cfg, "main")
	keyFeature := pool.GenerateKey(cfg, "feature-branch")

	assert.NotEqual(t, keyMain, keyFeature)
}

func TestPoolKeySameBranch(t *testing.T) {
	pool := NewPool(nil)

	cfg := &config.Config{
		Image:      "golang:1.22",
		ProjectDir: "/home/user/myproject",
	}

	key1 := pool.GenerateKey(cfg, "main")
	key2 := pool.GenerateKey(cfg, "main")

	assert.Equal(t, key1, key2)
}

func TestPoolEntryAdd(t *testing.T) {
	pool := NewPool(nil)

	entry := &PoolEntry{
		ContainerID: "abc123",
		Key:         "drun-test-key",
		ImageID:     "sha256:image123",
		Status:      ContainerStatusRunning,
	}

	pool.Add(entry)
	assert.Equal(t, 1, pool.Size())
}

func TestPoolEntryGet(t *testing.T) {
	pool := NewPool(nil)

	entry := &PoolEntry{
		ContainerID: "abc123",
		Key:         "drun-test-key",
		ImageID:     "sha256:image123",
		Status:      ContainerStatusRunning,
	}

	pool.Add(entry)

	got := pool.Get("drun-test-key")
	assert.NotNil(t, got)
	assert.Equal(t, "abc123", got.ContainerID)
}

func TestPoolEntryGetNotFound(t *testing.T) {
	pool := NewPool(nil)

	got := pool.Get("nonexistent")
	assert.Nil(t, got)
}

func TestPoolEntryRemove(t *testing.T) {
	pool := NewPool(nil)

	entry := &PoolEntry{
		ContainerID: "abc123",
		Key:         "drun-test-key",
		ImageID:     "sha256:image123",
		Status:      ContainerStatusRunning,
	}

	pool.Add(entry)
	pool.Remove("drun-test-key")

	assert.Equal(t, 0, pool.Size())
	assert.Nil(t, pool.Get("drun-test-key"))
}

func TestPoolList(t *testing.T) {
	pool := NewPool(nil)

	entries := []*PoolEntry{
		{ContainerID: "abc123", Key: "key1", Status: ContainerStatusRunning},
		{ContainerID: "def456", Key: "key2", Status: ContainerStatusStopped},
		{ContainerID: "ghi789", Key: "key3", Status: ContainerStatusRunning},
	}

	for _, e := range entries {
		pool.Add(e)
	}

	list := pool.List()
	assert.Len(t, list, 3)
}

func TestPoolListRunning(t *testing.T) {
	pool := NewPool(nil)

	entries := []*PoolEntry{
		{ContainerID: "abc123", Key: "key1", Status: ContainerStatusRunning},
		{ContainerID: "def456", Key: "key2", Status: ContainerStatusStopped},
		{ContainerID: "ghi789", Key: "key3", Status: ContainerStatusRunning},
	}

	for _, e := range entries {
		pool.Add(e)
	}

	running := pool.ListRunning()
	assert.Len(t, running, 2)
}

func TestPoolEntryIsRunning(t *testing.T) {
	running := &PoolEntry{Status: ContainerStatusRunning}
	stopped := &PoolEntry{Status: ContainerStatusStopped}
	created := &PoolEntry{Status: ContainerStatusCreated}

	assert.True(t, running.IsRunning())
	assert.False(t, stopped.IsRunning())
	assert.False(t, created.IsRunning())
}

func TestPoolClear(t *testing.T) {
	pool := NewPool(nil)

	entries := []*PoolEntry{
		{ContainerID: "abc123", Key: "key1", Status: ContainerStatusRunning},
		{ContainerID: "def456", Key: "key2", Status: ContainerStatusStopped},
	}

	for _, e := range entries {
		pool.Add(e)
	}

	pool.Clear()
	assert.Equal(t, 0, pool.Size())
}

func TestPoolGetByContainerID(t *testing.T) {
	pool := NewPool(nil)

	entry := &PoolEntry{
		ContainerID: "abc123def456",
		Key:         "drun-test-key",
		Status:      ContainerStatusRunning,
	}

	pool.Add(entry)

	got := pool.GetByContainerID("abc123def456")
	assert.NotNil(t, got)
	assert.Equal(t, "drun-test-key", got.Key)
}

func TestPoolGetByContainerIDNotFound(t *testing.T) {
	pool := NewPool(nil)

	got := pool.GetByContainerID("nonexistent")
	assert.Nil(t, got)
}

func TestPoolUpdateStatus(t *testing.T) {
	pool := NewPool(nil)

	entry := &PoolEntry{
		ContainerID: "abc123",
		Key:         "drun-test-key",
		Status:      ContainerStatusRunning,
	}

	pool.Add(entry)
	pool.UpdateStatus("drun-test-key", ContainerStatusStopped)

	got := pool.Get("drun-test-key")
	assert.Equal(t, ContainerStatusStopped, got.Status)
}

func TestPoolPersistence(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)

	// Create pool and add entry
	pool1 := NewPool(nil)
	pool1.SetStateDir(tmpDir)

	entry := &PoolEntry{
		ContainerID: "persist123",
		Key:         "drun-persist-key",
		ImageID:     "sha256:persist456",
		Status:      ContainerStatusRunning,
	}

	pool1.Add(entry)
	err := pool1.Save(ctx)
	require.NoError(t, err)

	// Create new pool and load
	pool2 := NewPool(nil)
	pool2.SetStateDir(tmpDir)
	err = pool2.Load(ctx)
	require.NoError(t, err)

	got := pool2.Get("drun-persist-key")
	assert.NotNil(t, got)
	assert.Equal(t, "persist123", got.ContainerID)
}

func TestContainerStatusString(t *testing.T) {
	tests := []struct {
		status   ContainerStatus
		expected string
	}{
		{ContainerStatusCreated, "created"},
		{ContainerStatusRunning, "running"},
		{ContainerStatusStopped, "stopped"},
		{ContainerStatusPaused, "paused"},
		{ContainerStatusDead, "dead"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

func TestPoolEntryLabels(t *testing.T) {
	pool := NewPool(nil)

	cfg := &config.Config{
		Image:      "golang:1.22",
		ProjectDir: "/home/user/myproject",
	}

	labels := pool.GenerateLabels(cfg, "main")

	assert.Equal(t, "true", labels["drun.managed"])
	assert.NotEmpty(t, labels["drun.project"])
	assert.Equal(t, "main", labels["drun.branch"])
}
