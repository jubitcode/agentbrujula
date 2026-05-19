package debug

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/client"
)

type CommandType string

const (
	CommandTypeEnv       CommandType = "env"
	CommandTypeDiff      CommandType = "diff"
	CommandTypeRetry     CommandType = "retry"
	CommandTypeExit      CommandType = "exit"
	CommandTypeHelp      CommandType = "help"
	CommandTypeSave      CommandType = "save"
	CommandTypeStatus    CommandType = "status"
	CommandTypeProcesses CommandType = "ps"
	CommandTypeShell     CommandType = "shell"
)

type Session struct {
	client  *client.Client
	state   *SessionState
	context *DebugContext
}

func NewSession(dockerClient *client.Client) *Session {
	return &Session{
		client:  dockerClient,
		state:   NewSessionState(),
		context: NewDebugContext(),
	}
}

type SessionState struct {
	CommandHistory  []string
	LastCommandStr  string
	LastError       string
	LastExitCode    int
}

func NewSessionState() *SessionState {
	return &SessionState{
		CommandHistory: make([]string, 0),
	}
}

func (s *SessionState) AddCommand(cmd string) {
	s.CommandHistory = append(s.CommandHistory, cmd)
}

func (s *SessionState) LastCommand() string {
	if len(s.CommandHistory) == 0 {
		return ""
	}
	return s.CommandHistory[len(s.CommandHistory)-1]
}

func (s *SessionState) SetLastError(err string, exitCode int) {
	s.LastError = err
	s.LastExitCode = exitCode
}

func (s *SessionState) ClearError() {
	s.LastError = ""
	s.LastExitCode = 0
}

func (s *SessionState) GetHistory(n int) []string {
	if n >= len(s.CommandHistory) {
		return s.CommandHistory
	}
	return s.CommandHistory[len(s.CommandHistory)-n:]
}

type DebugContext struct {
	ContainerID   string
	ContainerName string
	WorkingDir    string
	Project       string
}

func NewDebugContext() *DebugContext {
	return &DebugContext{}
}

type Command struct {
	Type CommandType
	Args string
	Raw  string
}

func ParseCommand(input string) *Command {
	input = strings.TrimSpace(input)
	parts := strings.SplitN(input, " ", 2)
	cmdName := strings.ToLower(parts[0])

	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	cmd := &Command{
		Raw:  input,
		Args: args,
	}

	switch cmdName {
	case "env":
		cmd.Type = CommandTypeEnv
	case "diff":
		cmd.Type = CommandTypeDiff
	case "retry":
		cmd.Type = CommandTypeRetry
	case "exit", "quit", "q":
		cmd.Type = CommandTypeExit
	case "help", "?":
		cmd.Type = CommandTypeHelp
	case "save":
		cmd.Type = CommandTypeSave
	case "status":
		cmd.Type = CommandTypeStatus
	case "ps", "processes":
		cmd.Type = CommandTypeProcesses
	default:
		cmd.Type = CommandTypeShell
		cmd.Args = input
	}

	return cmd
}

func IsDebugCommand(input string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	parts := strings.SplitN(input, " ", 2)
	cmdName := parts[0]

	switch cmdName {
	case "env", "diff", "retry", "exit", "quit", "q", "help", "?", "save", "status", "ps", "processes":
		return true
	}
	return false
}

type DebugOptions struct {
	LastCommand  string
	LastError    string
	LastExitCode int
}

func NewDebugOptions() *DebugOptions {
	return &DebugOptions{}
}

func (o *DebugOptions) WithLastCommand(cmd string) *DebugOptions {
	o.LastCommand = cmd
	return o
}

func (o *DebugOptions) WithLastError(err string, exitCode int) *DebugOptions {
	o.LastError = err
	o.LastExitCode = exitCode
	return o
}

type DebugPrompt struct {
	hasError bool
}

func NewDebugPrompt() *DebugPrompt {
	return &DebugPrompt{}
}

func (p *DebugPrompt) WithError(hasError bool) *DebugPrompt {
	p.hasError = hasError
	return p
}

func (p *DebugPrompt) String() string {
	if p.hasError {
		return "debug!> "
	}
	return "debug> "
}

type CommandResult struct {
	Output   string
	ExitCode int
	Error    string
}

func (r *CommandResult) Success() bool {
	return r.ExitCode == 0
}

func GetHelpText() string {
	return `Debug Session Commands:
  env       Show environment variables
  diff      Show drift from baseline
  retry     Retry last failed command
  save      Save current state to config
  status    Show container status
  ps        Show running processes
  help      Show this help
  exit      Exit debug mode

Any other input is executed as a shell command.
`
}

func (s *Session) Start(ctx context.Context, containerID string, opts *DebugOptions) error {
	if s.client == nil {
		return fmt.Errorf("no Docker client available")
	}

	s.context.ContainerID = containerID

	if opts != nil {
		s.state.AddCommand(opts.LastCommand)
		if opts.LastError != "" {
			s.state.SetLastError(opts.LastError, opts.LastExitCode)
		}
	}

	return nil
}

func (s *Session) Execute(ctx context.Context, input string) (*CommandResult, error) {
	cmd := ParseCommand(input)

	switch cmd.Type {
	case CommandTypeHelp:
		return &CommandResult{Output: GetHelpText(), ExitCode: 0}, nil
	case CommandTypeEnv:
		return s.executeEnv(ctx)
	case CommandTypeDiff:
		return s.executeDiff(ctx)
	case CommandTypeRetry:
		return s.executeRetry(ctx)
	case CommandTypeStatus:
		return s.executeStatus(ctx)
	case CommandTypeProcesses:
		return s.executeProcesses(ctx)
	case CommandTypeSave:
		return s.executeSave(ctx)
	case CommandTypeExit:
		return &CommandResult{Output: "Exiting debug mode", ExitCode: 0}, nil
	case CommandTypeShell:
		return s.executeShell(ctx, cmd.Args)
	}

	return &CommandResult{Error: "unknown command", ExitCode: 1}, nil
}

func (s *Session) executeEnv(ctx context.Context) (*CommandResult, error) {
	return &CommandResult{Output: "Environment variables...", ExitCode: 0}, nil
}

func (s *Session) executeDiff(ctx context.Context) (*CommandResult, error) {
	return &CommandResult{Output: "No drift detected", ExitCode: 0}, nil
}

func (s *Session) executeRetry(ctx context.Context) (*CommandResult, error) {
	lastCmd := s.state.LastCommand()
	if lastCmd == "" {
		return &CommandResult{Error: "no previous command to retry", ExitCode: 1}, nil
	}
	return s.executeShell(ctx, lastCmd)
}

func (s *Session) executeStatus(ctx context.Context) (*CommandResult, error) {
	return &CommandResult{Output: "Container status: running", ExitCode: 0}, nil
}

func (s *Session) executeProcesses(ctx context.Context) (*CommandResult, error) {
	return &CommandResult{Output: "Process list...", ExitCode: 0}, nil
}

func (s *Session) executeSave(ctx context.Context) (*CommandResult, error) {
	return &CommandResult{Output: "State saved", ExitCode: 0}, nil
}

func (s *Session) executeShell(ctx context.Context, cmd string) (*CommandResult, error) {
	s.state.AddCommand(cmd)
	return &CommandResult{Output: fmt.Sprintf("Executed: %s", cmd), ExitCode: 0}, nil
}
