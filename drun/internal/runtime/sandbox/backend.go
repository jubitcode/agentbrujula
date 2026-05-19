package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"drun/internal/runtime"
)

// SandboxBackend implements RuntimeBackend using OS-level process isolation.
// This is a lightweight backend that doesn't require Docker or VMs.
type SandboxBackend struct {
	mu        sync.RWMutex
	instances map[string]*sandboxInstance
	workDir   string
}

type sandboxInstance struct {
	id        string
	name      string
	spec      *runtime.InstanceSpec
	cmd       *exec.Cmd
	status    runtime.InstanceStatus
	createdAt time.Time
	startedAt time.Time
	exitCode  int
	workDir   string
}

// NewSandboxBackend creates a new sandbox backend.
func NewSandboxBackend(workDir string) *SandboxBackend {
	if workDir == "" {
		workDir = os.TempDir()
	}
	return &SandboxBackend{
		instances: make(map[string]*sandboxInstance),
		workDir:   workDir,
	}
}

func (s *SandboxBackend) Name() string {
	return "sandbox"
}

func (s *SandboxBackend) Type() runtime.BackendType {
	return runtime.BackendSandbox
}

func (s *SandboxBackend) Available() bool {
	return true // Always available as a fallback
}

func (s *SandboxBackend) Create(ctx context.Context, spec *runtime.InstanceSpec) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := generateID()
	name := spec.Name
	if name == "" {
		name = fmt.Sprintf("sandbox-%s", id[:8])
	}

	// Create instance working directory
	instanceDir := filepath.Join(s.workDir, "sandbox-instances", id)
	if err := os.MkdirAll(instanceDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create instance directory: %w", err)
	}

	instance := &sandboxInstance{
		id:        id,
		name:      name,
		spec:      spec,
		status:    runtime.StatusCreated,
		createdAt: time.Now(),
		workDir:   instanceDir,
	}

	s.instances[id] = instance
	return id, nil
}

func (s *SandboxBackend) Start(ctx context.Context, id string) error {
	s.mu.Lock()
	instance, ok := s.instances[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status == runtime.StatusRunning {
		s.mu.Unlock()
		return nil
	}

	spec := instance.spec

	// Determine command to run
	var cmd *exec.Cmd
	if len(spec.Command) > 0 {
		cmd = exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	} else {
		// Default to sleep for sandbox instances
		cmd = exec.CommandContext(ctx, "sleep", "infinity")
	}

	// Set working directory
	workdir := spec.Workdir
	if workdir == "" {
		workdir = instance.workDir
	}
	cmd.Dir = workdir

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range spec.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set process group for isolation
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to start process: %w", err)
	}

	instance.cmd = cmd
	instance.status = runtime.StatusRunning
	instance.startedAt = time.Now()
	s.mu.Unlock()

	// Monitor process in background
	go func() {
		cmd.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		if inst, ok := s.instances[id]; ok {
			inst.status = runtime.StatusStopped
			if cmd.ProcessState != nil {
				inst.exitCode = cmd.ProcessState.ExitCode()
			}
		}
	}()

	return nil
}

func (s *SandboxBackend) Stop(ctx context.Context, id string, timeout int) error {
	s.mu.Lock()
	instance, ok := s.instances[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("instance %q not found", id)
	}

	if instance.status != runtime.StatusRunning || instance.cmd == nil || instance.cmd.Process == nil {
		s.mu.Unlock()
		return nil
	}

	proc := instance.cmd.Process
	s.mu.Unlock()

	// Try graceful shutdown first
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		return nil
	}

	// Wait for process to exit or timeout
	done := make(chan struct{})
	go func() {
		proc.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(time.Duration(timeout) * time.Second):
		// Force kill
		proc.Kill()
		return nil
	case <-ctx.Done():
		proc.Kill()
		return ctx.Err()
	}
}

func (s *SandboxBackend) Remove(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	instance, ok := s.instances[id]
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}

	// Stop if running
	if instance.status == runtime.StatusRunning && instance.cmd != nil && instance.cmd.Process != nil {
		instance.cmd.Process.Kill()
	}

	// Clean up working directory
	if instance.workDir != "" {
		os.RemoveAll(instance.workDir)
	}

	delete(s.instances, id)
	return nil
}

func (s *SandboxBackend) Inspect(ctx context.Context, id string) (*runtime.InstanceInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	instance, ok := s.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance %q not found", id)
	}

	// Check if process is still running
	if instance.status == runtime.StatusRunning && instance.cmd != nil && instance.cmd.ProcessState != nil {
		instance.status = runtime.StatusStopped
	}

	labels := make(map[string]string)
	if instance.spec.Labels != nil {
		for k, v := range instance.spec.Labels {
			labels[k] = v
		}
	}

	return &runtime.InstanceInfo{
		ID:        instance.id,
		Name:      instance.name,
		Status:    instance.status,
		StartedAt: instance.startedAt.Format(time.RFC3339),
		Labels:    labels,
	}, nil
}

func (s *SandboxBackend) Exec(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) (*runtime.ExecResult, error) {
	s.mu.RLock()
	instance, ok := s.instances[id]
	if !ok {
		s.mu.RUnlock()
		return nil, fmt.Errorf("instance %q not found", id)
	}
	spec := instance.spec
	workDir := instance.workDir
	s.mu.RUnlock()

	if opts == nil {
		opts = &runtime.ExecOptions{}
	}

	// Create command
	execCmd := exec.CommandContext(ctx, cmd[0], cmd[1:]...)

	// Set working directory
	if opts.WorkingDir != "" {
		execCmd.Dir = opts.WorkingDir
	} else if spec.Workdir != "" {
		execCmd.Dir = spec.Workdir
	} else {
		execCmd.Dir = workDir
	}

	// Set environment
	execCmd.Env = os.Environ()
	for k, v := range spec.Env {
		execCmd.Env = append(execCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	execCmd.Env = append(execCmd.Env, opts.Env...)

	// Set up I/O
	var stdout, stderr bytes.Buffer
	if opts.Stdout != nil {
		execCmd.Stdout = io.MultiWriter(&stdout, opts.Stdout)
	} else {
		execCmd.Stdout = &stdout
	}
	if opts.Stderr != nil {
		execCmd.Stderr = io.MultiWriter(&stderr, opts.Stderr)
	} else {
		execCmd.Stderr = &stderr
	}
	if opts.Stdin != nil {
		execCmd.Stdin = opts.Stdin
	}

	// Run command
	err := execCmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("exec failed: %w", err)
		}
	}

	return &runtime.ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

func (s *SandboxBackend) ExecInteractive(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) error {
	s.mu.RLock()
	instance, ok := s.instances[id]
	if !ok {
		s.mu.RUnlock()
		return fmt.Errorf("instance %q not found", id)
	}
	spec := instance.spec
	workDir := instance.workDir
	s.mu.RUnlock()

	if opts == nil {
		opts = &runtime.ExecOptions{}
	}

	execCmd := exec.CommandContext(ctx, cmd[0], cmd[1:]...)

	// Set working directory
	if opts.WorkingDir != "" {
		execCmd.Dir = opts.WorkingDir
	} else if spec.Workdir != "" {
		execCmd.Dir = spec.Workdir
	} else {
		execCmd.Dir = workDir
	}

	// Set environment
	execCmd.Env = os.Environ()
	for k, v := range spec.Env {
		execCmd.Env = append(execCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	execCmd.Env = append(execCmd.Env, opts.Env...)

	// Connect I/O directly
	if opts.Stdin != nil {
		execCmd.Stdin = opts.Stdin
	}
	if opts.Stdout != nil {
		execCmd.Stdout = opts.Stdout
	}
	if opts.Stderr != nil {
		execCmd.Stderr = opts.Stderr
	}

	return execCmd.Run()
}

func (s *SandboxBackend) PullImage(ctx context.Context, name string) error {
	// Sandbox doesn't use images - this is a no-op
	return nil
}

func (s *SandboxBackend) HasImage(ctx context.Context, name string) (bool, error) {
	// Sandbox doesn't use images - always return true
	return true, nil
}

func (s *SandboxBackend) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop all running instances
	for id, instance := range s.instances {
		if instance.status == runtime.StatusRunning && instance.cmd != nil && instance.cmd.Process != nil {
			instance.cmd.Process.Kill()
		}
		if instance.workDir != "" {
			os.RemoveAll(instance.workDir)
		}
		delete(s.instances, id)
	}

	return nil
}

// List returns all sandbox instances.
func (s *SandboxBackend) List() []runtime.InstanceInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]runtime.InstanceInfo, 0, len(s.instances))
	for _, inst := range s.instances {
		result = append(result, runtime.InstanceInfo{
			ID:        inst.id,
			Name:      inst.name,
			Status:    inst.status,
			StartedAt: inst.startedAt.Format(time.RFC3339),
		})
	}
	return result
}

// generateID generates a unique instance ID.
func generateID() string {
	return fmt.Sprintf("%d%d", time.Now().UnixNano(), os.Getpid())
}

// Ensure SandboxBackend implements RuntimeBackend
var _ runtime.RuntimeBackend = (*SandboxBackend)(nil)
