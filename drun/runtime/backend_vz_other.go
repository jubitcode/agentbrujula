//go:build !darwin

package runtime

import "fmt"

func newVZBackendFromInternal() (RuntimeBackend, error) {
	return nil, fmt.Errorf("VZ backend is only available on macOS")
}
