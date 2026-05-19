package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"drun/test/testutil"
)

func TestSnapshot_SaveAndRestore(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Setup: create a container with some state
	testutil.RunDrun(t, dir, "run", "touch", "/tmp/before-snapshot")

	// Create a snapshot
	testutil.RunDrun(t, dir, "snapshot", "create", "test-snap")

	// Modify the container state
	testutil.RunDrun(t, dir, "run", "rm", "/tmp/before-snapshot")
	testutil.RunDrun(t, dir, "run", "touch", "/tmp/after-snapshot")

	// Verify the modification
	out, err := testutil.RunDrunOutput(t, dir, "run", "ls", "/tmp/before-snapshot")
	assert.Error(t, err, "before-snapshot should be deleted")
	assert.Contains(t, out, "No such file")

	out = testutil.RunDrun(t, dir, "run", "ls", "/tmp/after-snapshot")
	assert.Contains(t, out, "after-snapshot")

	// Restore the snapshot
	testutil.RunDrun(t, dir, "snapshot", "restore", "test-snap")

	// Original file should exist
	out = testutil.RunDrun(t, dir, "run", "ls", "/tmp/before-snapshot")
	assert.Contains(t, out, "before-snapshot")

	// New file should NOT exist
	out, err = testutil.RunDrunOutput(t, dir, "run", "ls", "/tmp/after-snapshot")
	assert.Error(t, err, "after-snapshot should not exist after restore")
	assert.Contains(t, out, "No such file")

	// Cleanup
	testutil.RunDrun(t, dir, "snapshot", "delete", "test-snap")
}

func TestSnapshot_List(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Create container
	testutil.RunDrun(t, dir, "run", "echo", "init")

	// Create multiple snapshots
	testutil.RunDrun(t, dir, "snapshot", "create", "snap1")
	testutil.RunDrun(t, dir, "snapshot", "create", "snap2")

	// List snapshots
	out := testutil.RunDrun(t, dir, "snapshot", "list")

	assert.Contains(t, out, "snap1")
	assert.Contains(t, out, "snap2")

	// Cleanup
	testutil.RunDrun(t, dir, "snapshot", "delete", "snap1")
	testutil.RunDrun(t, dir, "snapshot", "delete", "snap2")
}

func TestSnapshot_Delete(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Create container and snapshot
	testutil.RunDrun(t, dir, "run", "echo", "init")
	testutil.RunDrun(t, dir, "snapshot", "create", "to-delete")

	// Verify snapshot exists
	out := testutil.RunDrun(t, dir, "snapshot", "list")
	assert.Contains(t, out, "to-delete")

	// Delete snapshot
	testutil.RunDrun(t, dir, "snapshot", "delete", "to-delete")

	// Verify snapshot is gone
	out = testutil.RunDrun(t, dir, "snapshot", "list")
	assert.NotContains(t, out, "to-delete")
}

func TestSnapshot_WithComment(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Create container and snapshot with comment
	testutil.RunDrun(t, dir, "run", "echo", "init")
	testutil.RunDrun(t, dir, "snapshot", "create", "-m", "Test snapshot comment", "commented-snap")

	// List should show snapshot
	out := testutil.RunDrun(t, dir, "snapshot", "list")
	assert.Contains(t, out, "commented-snap")

	// Cleanup
	testutil.RunDrun(t, dir, "snapshot", "delete", "commented-snap")
}

func TestSnapshot_RestoreNonexistent(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Create container
	testutil.RunDrun(t, dir, "run", "echo", "init")

	// Try to restore nonexistent snapshot
	out := testutil.RunDrunExpectError(t, dir, "snapshot", "restore", "nonexistent-snapshot")

	// Should get an error
	assert.True(t, strings.Contains(out, "not found") || strings.Contains(out, "error") || strings.Contains(out, "Error"),
		"expected error about snapshot not found, got: %s", out)
}
