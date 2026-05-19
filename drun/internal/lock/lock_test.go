package lock

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"drun/internal/testutil"
)

func TestAcquireLock(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock := New(lockPath)
	err := lock.Acquire(context.Background(), nil)
	require.NoError(t, err)

	testutil.AssertFileExists(t, lockPath)

	err = lock.Release()
	require.NoError(t, err)
}

func TestLockRelease(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock := New(lockPath)
	err := lock.Acquire(context.Background(), nil)
	require.NoError(t, err)

	err = lock.Release()
	require.NoError(t, err)

	// Should be able to acquire again after release
	lock2 := New(lockPath)
	err = lock2.Acquire(context.Background(), nil)
	require.NoError(t, err)
	lock2.Release()
}

func TestLockBlocking(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock1 := New(lockPath)
	err := lock1.Acquire(context.Background(), nil)
	require.NoError(t, err)
	defer lock1.Release()

	// Second lock should fail to acquire with short timeout
	lock2 := New(lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = lock2.Acquire(ctx, &LockOptions{Timeout: 50 * time.Millisecond})
	assert.Error(t, err)
}

func TestLockNonBlocking(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock1 := New(lockPath)
	err := lock1.Acquire(context.Background(), nil)
	require.NoError(t, err)
	defer lock1.Release()

	// TryAcquire should return false immediately
	lock2 := New(lockPath)
	acquired, err := lock2.TryAcquire()
	assert.NoError(t, err)
	assert.False(t, acquired)
}

func TestLockWaitsAndAcquires(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock1 := New(lockPath)
	err := lock1.Acquire(context.Background(), nil)
	require.NoError(t, err)

	// Release after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		lock1.Release()
	}()

	// Second lock should eventually acquire
	lock2 := New(lockPath)
	err = lock2.Acquire(context.Background(), &LockOptions{Timeout: 500 * time.Millisecond})
	require.NoError(t, err)
	lock2.Release()
}

func TestLockContextCancellation(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock1 := New(lockPath)
	err := lock1.Acquire(context.Background(), nil)
	require.NoError(t, err)
	defer lock1.Release()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	lock2 := New(lockPath)
	err = lock2.Acquire(ctx, &LockOptions{Timeout: 1 * time.Second})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestLockConcurrentAccess(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	var counter int64
	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lock := New(lockPath)
			err := lock.Acquire(context.Background(), &LockOptions{Timeout: 5 * time.Second})
			if err != nil {
				t.Errorf("failed to acquire lock: %v", err)
				return
			}

			// Critical section - increment counter
			val := atomic.LoadInt64(&counter)
			time.Sleep(1 * time.Millisecond)
			atomic.StoreInt64(&counter, val+1)

			lock.Release()
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(numGoroutines), atomic.LoadInt64(&counter))
}

func TestLockCreatesDirectory(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "subdir", "nested", "test.lock")

	lock := New(lockPath)
	err := lock.Acquire(context.Background(), nil)
	require.NoError(t, err)
	defer lock.Release()

	testutil.AssertFileExists(t, lockPath)
}

func TestLockDoubleRelease(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock := New(lockPath)
	err := lock.Acquire(context.Background(), nil)
	require.NoError(t, err)

	err = lock.Release()
	require.NoError(t, err)

	// Second release should not panic
	err = lock.Release()
	assert.NoError(t, err) // Should be no-op or return nil
}

func TestLockPath(t *testing.T) {
	lockPath := "/some/path/test.lock"
	lock := New(lockPath)
	assert.Equal(t, lockPath, lock.Path)
}

func TestTryAcquireSuccess(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock := New(lockPath)
	acquired, err := lock.TryAcquire()
	require.NoError(t, err)
	assert.True(t, acquired)

	lock.Release()
}

func TestLockSharedMode(t *testing.T) {
	tmpDir := testutil.TempDir(t)
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Acquire shared lock
	lock1 := New(lockPath)
	err := lock1.Acquire(context.Background(), &LockOptions{Shared: true})
	require.NoError(t, err)
	defer lock1.Release()

	// Second shared lock should succeed
	lock2 := New(lockPath)
	err = lock2.Acquire(context.Background(), &LockOptions{Shared: true})
	require.NoError(t, err)
	defer lock2.Release()

	// Exclusive lock should fail
	lock3 := New(lockPath)
	acquired, err := lock3.TryAcquire()
	assert.NoError(t, err)
	assert.False(t, acquired, "exclusive lock should fail when shared locks are held")
}
