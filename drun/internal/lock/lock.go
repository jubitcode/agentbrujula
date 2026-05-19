package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

var (
	ErrLockBusy    = errors.New("lock is held by another process")
	ErrLockTimeout = errors.New("timeout waiting for lock")
)

type LockOptions struct {
	Timeout time.Duration
	Shared  bool
}

type Lock struct {
	Path string
	fd   int
}

func New(path string) *Lock {
	return &Lock{Path: path, fd: -1}
}

func (l *Lock) Acquire(ctx context.Context, opts *LockOptions) error {
	if opts == nil {
		opts = &LockOptions{}
	}

	dir := filepath.Dir(l.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	fd, err := unix.Open(l.Path, unix.O_CREAT|unix.O_RDWR, 0644)
	if err != nil {
		return err
	}
	l.fd = fd

	lockType := unix.LOCK_EX
	if opts.Shared {
		lockType = unix.LOCK_SH
	}

	if opts.Timeout == 0 {
		return l.acquireBlocking(ctx, lockType)
	}

	return l.acquireWithTimeout(ctx, lockType, opts.Timeout)
}

func (l *Lock) acquireBlocking(ctx context.Context, lockType int) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			unix.Close(l.fd)
			l.fd = -1
			return ctx.Err()
		case <-ticker.C:
			err := unix.Flock(l.fd, lockType|unix.LOCK_NB)
			if err == nil {
				return nil
			}
			if err != unix.EWOULDBLOCK {
				unix.Close(l.fd)
				l.fd = -1
				return err
			}
		}
	}
}

func (l *Lock) acquireWithTimeout(ctx context.Context, lockType int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			unix.Close(l.fd)
			l.fd = -1
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				unix.Close(l.fd)
				l.fd = -1
				return ErrLockTimeout
			}

			err := unix.Flock(l.fd, lockType|unix.LOCK_NB)
			if err == nil {
				return nil
			}
			if err != unix.EWOULDBLOCK {
				unix.Close(l.fd)
				l.fd = -1
				return err
			}
		}
	}
}

func (l *Lock) TryAcquire() (bool, error) {
	dir := filepath.Dir(l.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, err
	}

	fd, err := unix.Open(l.Path, unix.O_CREAT|unix.O_RDWR, 0644)
	if err != nil {
		return false, err
	}

	err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		unix.Close(fd)
		if err == unix.EWOULDBLOCK {
			return false, nil
		}
		return false, err
	}

	l.fd = fd
	return true, nil
}

func (l *Lock) Release() error {
	if l.fd < 0 {
		return nil
	}

	unix.Flock(l.fd, unix.LOCK_UN)
	err := unix.Close(l.fd)
	l.fd = -1
	return err
}
