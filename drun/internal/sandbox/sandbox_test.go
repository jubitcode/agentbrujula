package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSandboxManagerNew(t *testing.T) {
	sm := NewSandboxManager(nil)
	assert.NotNil(t, sm)
}

func TestSandboxConfigStruct(t *testing.T) {
	config := &SandboxConfig{
		Network:        true,
		ReadOnlyRoot:   false,
		MountMode:      MountModeReadWrite,
		TimeoutSeconds: 60,
		SeccompProfile: "strict",
	}

	assert.True(t, config.Network)
	assert.False(t, config.ReadOnlyRoot)
	assert.Equal(t, MountModeReadWrite, config.MountMode)
	assert.Equal(t, 60, config.TimeoutSeconds)
	assert.Equal(t, "strict", config.SeccompProfile)
}

func TestSandboxConfigDefaults(t *testing.T) {
	config := &SandboxConfig{
		ReadOnlyRoot: true,
		MountMode:    MountModeReadOnly,
	}

	assert.False(t, config.Network)
	assert.True(t, config.ReadOnlyRoot)
	assert.Equal(t, MountModeReadOnly, config.MountMode)
}

func TestMountModeValues(t *testing.T) {
	assert.Equal(t, MountMode("ro"), MountModeReadOnly)
	assert.Equal(t, MountMode("rw"), MountModeReadWrite)
}

func TestSeccompProfileValues(t *testing.T) {
	assert.Equal(t, "default", SeccompProfileDefault)
	assert.Equal(t, "strict", SeccompProfileStrict)
	assert.Equal(t, "unconfined", SeccompProfileUnconfined)
}

func TestSandboxConfigValidate(t *testing.T) {
	valid := &SandboxConfig{
		ReadOnlyRoot: true,
		MountMode:    MountModeReadOnly,
	}
	assert.NoError(t, valid.Validate())

	invalid := &SandboxConfig{
		TimeoutSeconds: -1,
	}
	assert.Error(t, invalid.Validate())
}

func TestSandboxConfigIsStrict(t *testing.T) {
	strict := &SandboxConfig{
		Network:      false,
		ReadOnlyRoot: true,
		MountMode:    MountModeReadOnly,
	}

	isStrict := !strict.Network && strict.ReadOnlyRoot && strict.MountMode == MountModeReadOnly
	assert.True(t, isStrict)

	relaxed := &SandboxConfig{
		Network:      true,
		ReadOnlyRoot: false,
	}

	isRelaxed := !relaxed.Network && relaxed.ReadOnlyRoot && relaxed.MountMode == MountModeReadOnly
	assert.False(t, isRelaxed)
}

func TestResourceLimitsStruct(t *testing.T) {
	limits := &ResourceLimits{
		MemoryBytes: 512 * 1024 * 1024,
		CPUCount:    2,
		PidsLimit:   50,
	}

	assert.Equal(t, int64(512*1024*1024), limits.MemoryBytes)
	assert.Equal(t, int64(2), limits.CPUCount)
	assert.Equal(t, 50, limits.PidsLimit)
}

func TestSandboxConfigWithLimits(t *testing.T) {
	limits := &ResourceLimits{MemoryBytes: 128 * 1024 * 1024}
	config := &SandboxConfig{Limits: limits}

	assert.Equal(t, int64(128*1024*1024), config.Limits.MemoryBytes)
}

func TestSandboxResultSuccess(t *testing.T) {
	result := &SandboxResult{
		ExitCode: 0,
		Output:   "success",
	}

	assert.True(t, result.ExitCode == 0)
	assert.Equal(t, "success", result.Output)
}

func TestSandboxResultFailure(t *testing.T) {
	result := &SandboxResult{
		ExitCode: 1,
		Error:    "command failed",
	}

	assert.False(t, result.ExitCode == 0)
	assert.Equal(t, "command failed", result.Error)
}

func TestSandboxResultTimedOut(t *testing.T) {
	result := &SandboxResult{
		ExitCode: 124,
		TimedOut: true,
	}

	assert.True(t, result.TimedOut)
}

func TestSeccompPolicyStruct(t *testing.T) {
	policy := &SeccompPolicy{}
	assert.NotNil(t, policy)
}

func TestSeccompPolicyStrict(t *testing.T) {
	policy := &SeccompPolicy{
		BlockedSyscalls: StrictSeccompBlocklist,
	}
	assert.True(t, len(policy.BlockedSyscalls) > 0)
}

func TestSeccompPolicyAllowSyscall(t *testing.T) {
	policy := &SeccompPolicy{
		AllowedSyscalls: []string{"read", "write"},
	}

	assert.Contains(t, policy.AllowedSyscalls, "read")
	assert.Contains(t, policy.AllowedSyscalls, "write")
}

func TestSeccompPolicyBlockSyscall(t *testing.T) {
	policy := &SeccompPolicy{
		BlockedSyscalls: []string{"ptrace", "mount"},
	}

	assert.Contains(t, policy.BlockedSyscalls, "ptrace")
	assert.Contains(t, policy.BlockedSyscalls, "mount")
}

func TestStrictSeccompBlocklist(t *testing.T) {
	assert.Contains(t, StrictSeccompBlocklist, "ptrace")
	assert.Contains(t, StrictSeccompBlocklist, "mount")
	assert.Contains(t, StrictSeccompBlocklist, "umount")
}
