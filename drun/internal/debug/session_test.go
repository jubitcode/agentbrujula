package debug

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDebugSessionNew(t *testing.T) {
	session := NewSession(nil)
	assert.NotNil(t, session)
}

func TestDebugSessionStateNew(t *testing.T) {
	state := NewSessionState()
	assert.NotNil(t, state)
	assert.Empty(t, state.CommandHistory)
}

func TestDebugSessionStateAddCommand(t *testing.T) {
	state := NewSessionState()
	state.AddCommand("ls -la")
	state.AddCommand("pwd")

	assert.Len(t, state.CommandHistory, 2)
	assert.Equal(t, "pwd", state.LastCommand())
}

func TestDebugSessionStateLastCommandEmpty(t *testing.T) {
	state := NewSessionState()
	assert.Empty(t, state.LastCommand())
}

func TestDebugSessionStateSetLastError(t *testing.T) {
	state := NewSessionState()
	state.SetLastError("command failed", 1)

	assert.Equal(t, "command failed", state.LastError)
	assert.Equal(t, 1, state.LastExitCode)
}

func TestDebugSessionStateClearError(t *testing.T) {
	state := NewSessionState()
	state.SetLastError("error", 1)
	state.ClearError()

	assert.Empty(t, state.LastError)
	assert.Equal(t, 0, state.LastExitCode)
}

func TestDebugCommandParseEnv(t *testing.T) {
	cmd := ParseCommand("env")
	assert.Equal(t, CommandTypeEnv, cmd.Type)
}

func TestDebugCommandParseDiff(t *testing.T) {
	cmd := ParseCommand("diff")
	assert.Equal(t, CommandTypeDiff, cmd.Type)
}

func TestDebugCommandParseRetry(t *testing.T) {
	cmd := ParseCommand("retry")
	assert.Equal(t, CommandTypeRetry, cmd.Type)
}

func TestDebugCommandParseExit(t *testing.T) {
	cmd := ParseCommand("exit")
	assert.Equal(t, CommandTypeExit, cmd.Type)
}

func TestDebugCommandParseHelp(t *testing.T) {
	cmd := ParseCommand("help")
	assert.Equal(t, CommandTypeHelp, cmd.Type)
}

func TestDebugCommandParseShell(t *testing.T) {
	cmd := ParseCommand("ls -la /tmp")
	assert.Equal(t, CommandTypeShell, cmd.Type)
	assert.Equal(t, "ls -la /tmp", cmd.Args)
}

func TestDebugCommandParseSave(t *testing.T) {
	cmd := ParseCommand("save")
	assert.Equal(t, CommandTypeSave, cmd.Type)
}

func TestDebugCommandParseStatus(t *testing.T) {
	cmd := ParseCommand("status")
	assert.Equal(t, CommandTypeStatus, cmd.Type)
}

func TestDebugCommandParseProcesses(t *testing.T) {
	cmd := ParseCommand("ps")
	assert.Equal(t, CommandTypeProcesses, cmd.Type)
}

func TestDebugOptionsNew(t *testing.T) {
	opts := NewDebugOptions()
	assert.NotNil(t, opts)
}

func TestDebugOptionsWithContext(t *testing.T) {
	opts := NewDebugOptions().WithLastCommand("go test ./...")
	assert.Equal(t, "go test ./...", opts.LastCommand)
}

func TestDebugOptionsWithLastError(t *testing.T) {
	opts := NewDebugOptions().WithLastError("test failed", 1)
	assert.Equal(t, "test failed", opts.LastError)
	assert.Equal(t, 1, opts.LastExitCode)
}

func TestDebugPromptDefault(t *testing.T) {
	prompt := NewDebugPrompt()
	assert.Equal(t, "debug> ", prompt.String())
}

func TestDebugPromptWithError(t *testing.T) {
	prompt := NewDebugPrompt().WithError(true)
	assert.Contains(t, prompt.String(), "!")
}

func TestDebugHelpText(t *testing.T) {
	help := GetHelpText()
	assert.Contains(t, help, "env")
	assert.Contains(t, help, "diff")
	assert.Contains(t, help, "retry")
	assert.Contains(t, help, "exit")
}

func TestDebugSessionIsDebugCommand(t *testing.T) {
	assert.True(t, IsDebugCommand("env"))
	assert.True(t, IsDebugCommand("diff"))
	assert.True(t, IsDebugCommand("retry"))
	assert.True(t, IsDebugCommand("exit"))
	assert.True(t, IsDebugCommand("help"))
	assert.False(t, IsDebugCommand("ls"))
	assert.False(t, IsDebugCommand("cat /etc/hosts"))
}

func TestCommandResultNew(t *testing.T) {
	result := &CommandResult{
		Output:   "command output",
		ExitCode: 0,
	}

	assert.Equal(t, "command output", result.Output)
	assert.True(t, result.Success())
}

func TestCommandResultFailure(t *testing.T) {
	result := &CommandResult{
		Output:   "error",
		ExitCode: 1,
		Error:    "command failed",
	}

	assert.False(t, result.Success())
	assert.Equal(t, "command failed", result.Error)
}

func TestDebugSessionStateHistory(t *testing.T) {
	state := NewSessionState()
	state.AddCommand("cmd1")
	state.AddCommand("cmd2")
	state.AddCommand("cmd3")

	history := state.GetHistory(2)
	assert.Len(t, history, 2)
	assert.Equal(t, "cmd2", history[0])
	assert.Equal(t, "cmd3", history[1])
}

func TestDebugSessionStateHistoryAll(t *testing.T) {
	state := NewSessionState()
	state.AddCommand("cmd1")
	state.AddCommand("cmd2")

	history := state.GetHistory(10)
	assert.Len(t, history, 2)
}

func TestDebugContextNew(t *testing.T) {
	ctx := NewDebugContext()
	assert.NotNil(t, ctx)
}

func TestDebugContextSetContainer(t *testing.T) {
	ctx := NewDebugContext()
	ctx.ContainerID = "abc123"
	ctx.ContainerName = "mycontainer"

	assert.Equal(t, "abc123", ctx.ContainerID)
	assert.Equal(t, "mycontainer", ctx.ContainerName)
}

func TestDebugContextSetWorkingDir(t *testing.T) {
	ctx := NewDebugContext()
	ctx.WorkingDir = "/workspace"

	assert.Equal(t, "/workspace", ctx.WorkingDir)
}
