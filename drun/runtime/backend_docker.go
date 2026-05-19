package runtime

import (
	"context"

	"drun/internal/runtime/docker"
)

func newDockerBackendFromInternal(ctx context.Context, host string) (RuntimeBackend, error) {
	return docker.NewDockerBackend(ctx, host)
}
