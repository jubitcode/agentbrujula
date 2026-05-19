package build

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestCacheNew(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)
	assert.NotNil(t, cache)
}

func TestCachePutAndGet(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	entry := &CacheEntry{
		Key:       "test-key-123",
		ImageID:   "sha256:abc123",
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	err = cache.Put(ctx, entry)
	require.NoError(t, err)

	got, err := cache.Get(ctx, "test-key-123")
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, "sha256:abc123", got.ImageID)
}

func TestCacheGetMiss(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	got, err := cache.Get(ctx, "nonexistent-key")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestCacheUpdateLastUsed(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	oldTime := time.Now().Add(-24 * time.Hour)
	entry := &CacheEntry{
		Key:       "test-key",
		ImageID:   "sha256:abc123",
		CreatedAt: oldTime,
		LastUsed:  oldTime,
	}

	err = cache.Put(ctx, entry)
	require.NoError(t, err)

	// Get should update LastUsed
	got, err := cache.Get(ctx, "test-key")
	require.NoError(t, err)
	assert.True(t, got.LastUsed.After(oldTime))
}

func TestCacheDelete(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	entry := &CacheEntry{
		Key:       "test-key",
		ImageID:   "sha256:abc123",
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	err = cache.Put(ctx, entry)
	require.NoError(t, err)

	err = cache.Delete(ctx, "test-key")
	require.NoError(t, err)

	got, err := cache.Get(ctx, "test-key")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestCacheCleanup(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	// Add old entry
	oldEntry := &CacheEntry{
		Key:       "old-key",
		ImageID:   "sha256:old123",
		CreatedAt: time.Now().Add(-48 * time.Hour),
		LastUsed:  time.Now().Add(-48 * time.Hour),
	}
	err = cache.Put(ctx, oldEntry)
	require.NoError(t, err)

	// Add recent entry
	newEntry := &CacheEntry{
		Key:       "new-key",
		ImageID:   "sha256:new123",
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}
	err = cache.Put(ctx, newEntry)
	require.NoError(t, err)

	// Cleanup entries older than 24 hours
	removed, err := cache.Cleanup(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	// Old entry should be gone
	got, err := cache.Get(ctx, "old-key")
	require.NoError(t, err)
	assert.Nil(t, got)

	// New entry should still exist
	got, err = cache.Get(ctx, "new-key")
	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestCacheList(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	entries := []*CacheEntry{
		{Key: "key1", ImageID: "sha256:111", CreatedAt: time.Now(), LastUsed: time.Now()},
		{Key: "key2", ImageID: "sha256:222", CreatedAt: time.Now(), LastUsed: time.Now()},
		{Key: "key3", ImageID: "sha256:333", CreatedAt: time.Now(), LastUsed: time.Now()},
	}

	for _, e := range entries {
		err = cache.Put(ctx, e)
		require.NoError(t, err)
	}

	list, err := cache.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 3)
}

func TestCachePersistence(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)

	// Create cache and add entry
	cache1, err := NewCache(tmpDir)
	require.NoError(t, err)

	entry := &CacheEntry{
		Key:       "persist-key",
		ImageID:   "sha256:persist123",
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}
	err = cache1.Put(ctx, entry)
	require.NoError(t, err)

	// Create new cache instance (simulates restart)
	cache2, err := NewCache(tmpDir)
	require.NoError(t, err)

	// Entry should still exist
	got, err := cache2.Get(ctx, "persist-key")
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, "sha256:persist123", got.ImageID)
}

func TestCacheInvalidation(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	entry := &CacheEntry{
		Key:       "test-key",
		ImageID:   "sha256:abc123",
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	err = cache.Put(ctx, entry)
	require.NoError(t, err)

	// Update with same key but different image
	updatedEntry := &CacheEntry{
		Key:       "test-key",
		ImageID:   "sha256:def456",
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	err = cache.Put(ctx, updatedEntry)
	require.NoError(t, err)

	got, err := cache.Get(ctx, "test-key")
	require.NoError(t, err)
	assert.Equal(t, "sha256:def456", got.ImageID)
}

func TestCacheEntryExpired(t *testing.T) {
	now := time.Now()

	entry := &CacheEntry{
		Key:       "test",
		LastUsed:  now.Add(-48 * time.Hour),
		CreatedAt: now.Add(-48 * time.Hour),
	}

	assert.True(t, entry.IsExpired(24*time.Hour))
	assert.False(t, entry.IsExpired(72*time.Hour))
}

func TestCacheSize(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, 0, cache.Size())

	entry := &CacheEntry{
		Key:       "key1",
		ImageID:   "sha256:111",
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}
	err = cache.Put(ctx, entry)
	require.NoError(t, err)

	assert.Equal(t, 1, cache.Size())
}

func TestCacheClear(t *testing.T) {
	ctx := context.Background()
	tmpDir := testutil.TempDir(t)
	cache, err := NewCache(tmpDir)
	require.NoError(t, err)

	entries := []*CacheEntry{
		{Key: "key1", ImageID: "sha256:111", CreatedAt: time.Now(), LastUsed: time.Now()},
		{Key: "key2", ImageID: "sha256:222", CreatedAt: time.Now(), LastUsed: time.Now()},
	}

	for _, e := range entries {
		err = cache.Put(ctx, e)
		require.NoError(t, err)
	}

	err = cache.Clear(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, cache.Size())
}
