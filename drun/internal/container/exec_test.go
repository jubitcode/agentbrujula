package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecHandlerNew(t *testing.T) {
	eh := NewExecHandler(nil)
	assert.NotNil(t, eh)
}

func TestExecOptionsStruct(t *testing.T) {
	opts := &ExecOptions{
		TTY:        true,
		Detach:     true,
		Privileged: true,
		User:       "1000:1000",
		WorkingDir: "/app",
		Env:        []string{"DEBUG=true"},
	}

	assert.True(t, opts.TTY)
	assert.True(t, opts.Detach)
	assert.True(t, opts.Privileged)
	assert.Equal(t, "1000:1000", opts.User)
	assert.Equal(t, "/app", opts.WorkingDir)
	assert.Contains(t, opts.Env, "DEBUG=true")
}

func TestExecOptionsZeroValue(t *testing.T) {
	opts := &ExecOptions{}

	assert.False(t, opts.TTY)
	assert.False(t, opts.Detach)
	assert.False(t, opts.Privileged)
	assert.Empty(t, opts.User)
	assert.Empty(t, opts.WorkingDir)
	assert.Nil(t, opts.Env)
}

func TestExecResultSuccess(t *testing.T) {
	result := &ExecResult{
		ExitCode: 0,
		Stdout:   []byte("output"),
		Stderr:   []byte{},
	}

	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "output", string(result.Stdout))
}

func TestExecResultFailure(t *testing.T) {
	result := &ExecResult{
		ExitCode: 1,
		Stdout:   []byte{},
		Stderr:   []byte("error message"),
	}

	assert.NotEqual(t, 0, result.ExitCode)
	assert.Equal(t, "error message", string(result.Stderr))
}

func TestExecResultCombinedOutput(t *testing.T) {
	result := &ExecResult{
		ExitCode: 0,
		Stdout:   []byte("stdout\n"),
		Stderr:   []byte("stderr\n"),
	}

	combined := string(result.Stdout) + string(result.Stderr)
	assert.Contains(t, combined, "stdout")
	assert.Contains(t, combined, "stderr")
}
