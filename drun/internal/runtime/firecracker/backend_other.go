//go:build !linux

// Package firecracker provides a RuntimeBackend using Firecracker microVMs.
// This stub is used on non-Linux platforms where Firecracker is not available.
package firecracker

import (
	"context"
	"fmt"

	"drun/internal/runtime"
)

// FirecrackerBackend implements RuntimeBackend using Firecracker microVMs.
// This is a stub implementation for non-Linux platforms.
type FirecrackerBackend struct{}

// NewFirecrackerBackend returns an error on non-Linux platforms.
func NewFirecrackerBackend(socketPath string) (*FirecrackerBackend, error) {
	return nil, fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) Name() string              { return "firecracker" }
func (f *FirecrackerBackend) Type() runtime.BackendType { return runtime.BackendFirecracker }
func (f *FirecrackerBackend) Available() bool           { return false }
func (f *FirecrackerBackend) Close() error              { return nil }

func (f *FirecrackerBackend) Create(ctx context.Context, spec *runtime.InstanceSpec) (string, error) {
	return "", fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) Start(ctx context.Context, id string) error {
	return fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) Stop(ctx context.Context, id string, timeout int) error {
	return fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) Remove(ctx context.Context, id string) error {
	return fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) Inspect(ctx context.Context, id string) (*runtime.InstanceInfo, error) {
	return nil, fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) Exec(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) (*runtime.ExecResult, error) {
	return nil, fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) ExecInteractive(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) error {
	return fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) PullImage(ctx context.Context, name string) error {
	return fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

func (f *FirecrackerBackend) HasImage(ctx context.Context, name string) (bool, error) {
	return false, fmt.Errorf("Firecracker backend is only available on Linux with KVM support")
}

// Ensure FirecrackerBackend implements RuntimeBackend
var _ runtime.RuntimeBackend = (*FirecrackerBackend)(nil)
