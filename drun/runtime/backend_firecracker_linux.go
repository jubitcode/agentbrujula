//go:build linux

package runtime

import (
	"drun/internal/runtime/firecracker"
)

func newFirecrackerBackendFromInternal(socketPath string) (RuntimeBackend, error) {
	return firecracker.NewFirecrackerBackend(socketPath)
}
