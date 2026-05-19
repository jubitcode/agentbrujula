//go:build !linux

package runtime

import "fmt"

func newFirecrackerBackendFromInternal(socketPath string) (RuntimeBackend, error) {
	return nil, fmt.Errorf("Firecracker backend is only available on Linux")
}
