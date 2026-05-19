//go:build !darwin

// Package vz provides a RuntimeBackend using Apple's Virtualization.framework.
// This stub is used on non-macOS platforms where Virtualization.framework is not available.
package vz

import (
	"context"
	"fmt"

	"drun/internal/runtime"
)

// VZBackend implements RuntimeBackend using Apple's Virtualization.framework.
// This is a stub implementation for non-macOS platforms.
type VZBackend struct{}

// NewVZBackend returns an error on non-macOS platforms.
func NewVZBackend() (*VZBackend, error) {
	return nil, fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) Name() string                 { return "vz" }
func (v *VZBackend) Type() runtime.BackendType    { return runtime.BackendVZ }
func (v *VZBackend) Available() bool              { return false }
func (v *VZBackend) Close() error                 { return nil }

func (v *VZBackend) Create(ctx context.Context, spec *runtime.InstanceSpec) (string, error) {
	return "", fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) Start(ctx context.Context, id string) error {
	return fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) Stop(ctx context.Context, id string, timeout int) error {
	return fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) Remove(ctx context.Context, id string) error {
	return fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) Inspect(ctx context.Context, id string) (*runtime.InstanceInfo, error) {
	return nil, fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) Exec(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) (*runtime.ExecResult, error) {
	return nil, fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) ExecInteractive(ctx context.Context, id string, cmd []string, opts *runtime.ExecOptions) error {
	return fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) PullImage(ctx context.Context, name string) error {
	return fmt.Errorf("VZ backend is only available on macOS")
}

func (v *VZBackend) HasImage(ctx context.Context, name string) (bool, error) {
	return false, fmt.Errorf("VZ backend is only available on macOS")
}

// Ensure VZBackend implements RuntimeBackend
var _ runtime.RuntimeBackend = (*VZBackend)(nil)
