package runtime

import (
	"drun/internal/runtime/sandbox"
)

func newSandboxBackendFromInternal(baseDir string) RuntimeBackend {
	return sandbox.NewSandboxBackend(baseDir)
}
