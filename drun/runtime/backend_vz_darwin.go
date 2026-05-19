//go:build darwin

package runtime

import (
	"drun/internal/runtime/vz"
)

func newVZBackendFromInternal() (RuntimeBackend, error) {
	return vz.NewVZBackend()
}
