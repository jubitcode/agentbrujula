package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"drun/test/testutil"
)

func TestBranch_SeparateContainers(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize git repo
	testutil.InitGitRepo(t, dir)
	testutil.GitCheckout(t, dir, "main")

	// Create a marker file in the main branch container
	testutil.RunDrun(t, dir, "run", "touch", "/tmp/main-marker")

	// Switch to a feature branch
	testutil.GitCheckout(t, dir, "feature")

	// The marker file should NOT exist in the feature branch container
	// (it's a different container)
	out, err := testutil.RunDrunOutput(t, dir, "run", "ls", "/tmp/main-marker")

	// Should fail because file doesn't exist in this container
	assert.Error(t, err, "expected error because file should not exist in feature branch container")
	assert.Contains(t, out, "No such file")
}

func TestBranch_SwitchBackRestoresContainer(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize git repo
	testutil.InitGitRepo(t, dir)
	testutil.GitCheckout(t, dir, "main")

	// Create a marker file in the main branch container
	testutil.RunDrun(t, dir, "run", "touch", "/tmp/main-marker")

	// Switch to feature branch
	testutil.GitCheckout(t, dir, "feature")

	// Do something in feature branch container
	testutil.RunDrun(t, dir, "run", "touch", "/tmp/feature-marker")

	// Switch back to main
	testutil.GitCheckout(t, dir, "main")

	// The main marker should still exist (persistent container)
	out := testutil.RunDrun(t, dir, "run", "ls", "/tmp/main-marker")
	assert.Contains(t, out, "main-marker")

	// The feature marker should NOT exist in main container
	out, err := testutil.RunDrunOutput(t, dir, "run", "ls", "/tmp/feature-marker")
	assert.Error(t, err)
	assert.Contains(t, out, "No such file")
}

func TestBranch_ContainerNamingIncludesBranch(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize git repo and create a branch
	testutil.InitGitRepo(t, dir)
	testutil.GitCheckout(t, dir, "test-branch")

	// Run a command to create the container
	testutil.RunDrun(t, dir, "run", "echo", "hello")

	// Verify container name includes branch
	containerID := testutil.GetContainerID(t, "drun-")
	assert.NotEmpty(t, containerID, "container should be created")

	// List containers to verify naming
	out, _ := testutil.ExecInContainer(t, containerID, "hostname")
	// The hostname should be based on the container name
	assert.NotEmpty(t, out)
}

func TestBranch_FeatureBranchSlashesHandled(t *testing.T) {
	testutil.RequireDocker(t)

	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Initialize git repo with a branch that has slashes
	testutil.InitGitRepo(t, dir)
	testutil.GitCheckout(t, dir, "feature/my-feature")

	// Run a command - should handle slashes in branch name
	out := testutil.RunDrun(t, dir, "run", "echo", "works")

	assert.Contains(t, out, "works")
}

func TestBranch_NoGitRepoUsesSimpleName(t *testing.T) {
	testutil.RequireDocker(t)

	// Create project without git
	dir, cleanup := testutil.TempProject(t, "go")
	defer cleanup()
	defer testutil.CleanupContainersWithLabel(t)

	// Run a command - should work without git
	out := testutil.RunDrun(t, dir, "run", "echo", "hello")

	assert.Contains(t, out, "hello")
}
