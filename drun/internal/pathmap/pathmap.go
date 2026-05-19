package pathmap

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrPathNotMapped = errors.New("path is not within any mount")
)

type Mount struct {
	Source string
	Target string
}

type Mapper struct {
	mounts   []Mount
	workdir  string
	hostRoot string
}

type MapperOption func(*Mapper)

func NewMapper(mounts []Mount, opts ...MapperOption) *Mapper {
	m := &Mapper{
		mounts: make([]Mount, len(mounts)),
	}

	for i, mount := range mounts {
		m.mounts[i] = Mount{
			Source: expandPath(mount.Source),
			Target: filepath.Clean(mount.Target),
		}
	}

	sort.Slice(m.mounts, func(i, j int) bool {
		return len(m.mounts[i].Source) > len(m.mounts[j].Source)
	})

	for _, opt := range opts {
		opt(m)
	}

	return m
}

func WithWorkdir(workdir string) MapperOption {
	return func(m *Mapper) {
		m.workdir = workdir
	}
}

func WithHostRoot(root string) MapperOption {
	return func(m *Mapper) {
		m.hostRoot = expandPath(root)
	}
}

func (m *Mapper) Workdir() string {
	return m.workdir
}

func (m *Mapper) ToContainer(hostPath string) (string, error) {
	if !filepath.IsAbs(hostPath) {
		if m.hostRoot != "" {
			hostPath = filepath.Join(m.hostRoot, hostPath)
		} else {
			abs, err := filepath.Abs(hostPath)
			if err != nil {
				return "", err
			}
			hostPath = abs
		}
	}

	hostPath = expandPath(hostPath)
	hostPath = filepath.Clean(hostPath)

	for _, mount := range m.mounts {
		if hostPath == mount.Source {
			return mount.Target, nil
		}
		if strings.HasPrefix(hostPath, mount.Source+string(filepath.Separator)) {
			rel, err := filepath.Rel(mount.Source, hostPath)
			if err != nil {
				continue
			}
			return filepath.Join(mount.Target, rel), nil
		}
	}

	return "", ErrPathNotMapped
}

func (m *Mapper) ToHost(containerPath string) (string, error) {
	containerPath = filepath.Clean(containerPath)

	for _, mount := range m.mounts {
		if containerPath == mount.Target {
			return mount.Source, nil
		}
		if strings.HasPrefix(containerPath, mount.Target+"/") {
			rel, err := filepath.Rel(mount.Target, containerPath)
			if err != nil {
				continue
			}
			return filepath.Join(mount.Source, rel), nil
		}
	}

	return "", ErrPathNotMapped
}

func (m *Mapper) IsInMount(hostPath string) bool {
	_, err := m.ToContainer(hostPath)
	return err == nil
}

func (m *Mapper) RelativeToWorkdir(containerPath string) (string, error) {
	if m.workdir == "" {
		return containerPath, nil
	}
	return filepath.Rel(m.workdir, containerPath)
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	} else if path == "." {
		cwd, err := os.Getwd()
		if err == nil {
			path = cwd
		}
	} else if strings.HasPrefix(path, "./") {
		cwd, err := os.Getwd()
		if err == nil {
			path = filepath.Join(cwd, path[2:])
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
