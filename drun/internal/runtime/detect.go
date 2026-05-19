package runtime

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"
)

type RuntimeType string

const (
	RuntimeOrbStack      RuntimeType = "orbstack"
	RuntimeColima        RuntimeType = "colima"
	RuntimeDockerDesktop RuntimeType = "docker-desktop"
	RuntimeCustom        RuntimeType = "custom"
	RuntimeUnknown       RuntimeType = "unknown"
)

var (
	ErrRuntimeNotFound = errors.New("no container runtime found")
)

type Runtime struct {
	Type       RuntimeType
	SocketPath string
	Host       string
}

type DetectOptions struct {
	SkipEnv             bool
	OrbStackSocket      string
	ColimaSocket        string
	DockerDesktopSocket string
}

type Detector struct {
	opts *DetectOptions
}

func NewDetector(opts *DetectOptions) *Detector {
	if opts == nil {
		opts = &DetectOptions{}
	}

	if opts.OrbStackSocket == "" {
		home, _ := os.UserHomeDir()
		opts.OrbStackSocket = filepath.Join(home, ".orbstack", "run", "docker.sock")
	}
	if opts.ColimaSocket == "" {
		home, _ := os.UserHomeDir()
		opts.ColimaSocket = filepath.Join(home, ".colima", "default", "docker.sock")
	}
	if opts.DockerDesktopSocket == "" {
		opts.DockerDesktopSocket = "/var/run/docker.sock"
	}

	return &Detector{opts: opts}
}

func (d *Detector) Detect(ctx context.Context) (*Runtime, error) {
	if !d.opts.SkipEnv {
		if host := os.Getenv("DOCKER_HOST"); host != "" {
			return &Runtime{
				Type: RuntimeCustom,
				Host: host,
			}, nil
		}
	}

	if d.isSocketAccessible(d.opts.OrbStackSocket) {
		return &Runtime{
			Type:       RuntimeOrbStack,
			SocketPath: d.opts.OrbStackSocket,
			Host:       "unix://" + d.opts.OrbStackSocket,
		}, nil
	}

	if d.isSocketAccessible(d.opts.ColimaSocket) {
		return &Runtime{
			Type:       RuntimeColima,
			SocketPath: d.opts.ColimaSocket,
			Host:       "unix://" + d.opts.ColimaSocket,
		}, nil
	}

	if d.isSocketAccessible(d.opts.DockerDesktopSocket) {
		return &Runtime{
			Type:       RuntimeDockerDesktop,
			SocketPath: d.opts.DockerDesktopSocket,
			Host:       "unix://" + d.opts.DockerDesktopSocket,
		}, nil
	}

	return nil, ErrRuntimeNotFound
}

func (d *Detector) DetectAll(ctx context.Context) ([]*Runtime, error) {
	var runtimes []*Runtime

	if !d.opts.SkipEnv {
		if host := os.Getenv("DOCKER_HOST"); host != "" {
			runtimes = append(runtimes, &Runtime{
				Type: RuntimeCustom,
				Host: host,
			})
		}
	}

	if d.isSocketAccessible(d.opts.OrbStackSocket) {
		runtimes = append(runtimes, &Runtime{
			Type:       RuntimeOrbStack,
			SocketPath: d.opts.OrbStackSocket,
			Host:       "unix://" + d.opts.OrbStackSocket,
		})
	}

	if d.isSocketAccessible(d.opts.ColimaSocket) {
		runtimes = append(runtimes, &Runtime{
			Type:       RuntimeColima,
			SocketPath: d.opts.ColimaSocket,
			Host:       "unix://" + d.opts.ColimaSocket,
		})
	}

	if d.isSocketAccessible(d.opts.DockerDesktopSocket) {
		runtimes = append(runtimes, &Runtime{
			Type:       RuntimeDockerDesktop,
			SocketPath: d.opts.DockerDesktopSocket,
			Host:       "unix://" + d.opts.DockerDesktopSocket,
		})
	}

	if len(runtimes) == 0 {
		return nil, ErrRuntimeNotFound
	}

	return runtimes, nil
}

func (d *Detector) isSocketAccessible(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	if info.Mode()&os.ModeSocket == 0 {
		return false
	}

	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()

	return true
}

func DefaultSocketPaths() map[RuntimeType]string {
	home, _ := os.UserHomeDir()
	return map[RuntimeType]string{
		RuntimeOrbStack:      filepath.Join(home, ".orbstack", "run", "docker.sock"),
		RuntimeColima:        filepath.Join(home, ".colima", "default", "docker.sock"),
		RuntimeDockerDesktop: "/var/run/docker.sock",
	}
}
