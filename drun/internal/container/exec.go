package container

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type ExecHandler struct {
	client *client.Client
}

func NewExecHandler(dockerClient *client.Client) *ExecHandler {
	return &ExecHandler{client: dockerClient}
}

type ExecOptions struct {
	TTY        bool
	Detach     bool
	Privileged bool
	User       string
	WorkingDir string
	Env        []string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
}

type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

func (e *ExecHandler) Exec(ctx context.Context, containerID string, cmd []string, opts *ExecOptions) (*ExecResult, error) {
	if opts == nil {
		opts = &ExecOptions{}
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		Tty:          opts.TTY,
		Privileged:   opts.Privileged,
		User:         opts.User,
		WorkingDir:   opts.WorkingDir,
		Env:          opts.Env,
		AttachStdin:  opts.Stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := e.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := e.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{
		Tty: opts.TTY,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	var stdout, stderr bytes.Buffer
	outWriter := io.Writer(&stdout)
	errWriter := io.Writer(&stderr)

	if opts.Stdout != nil {
		outWriter = io.MultiWriter(&stdout, opts.Stdout)
	}
	if opts.Stderr != nil {
		errWriter = io.MultiWriter(&stderr, opts.Stderr)
	}

	if opts.TTY {
		_, err = io.Copy(outWriter, attachResp.Reader)
	} else {
		_, err = stdCopy(outWriter, errWriter, attachResp.Reader)
	}
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	inspect, err := e.client.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	return &ExecResult{
		ExitCode: inspect.ExitCode,
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
	}, nil
}

func (e *ExecHandler) ExecInteractive(ctx context.Context, containerID string, cmd []string, opts *ExecOptions) error {
	if opts == nil {
		opts = &ExecOptions{}
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		Tty:          true,
		Privileged:   opts.Privileged,
		User:         opts.User,
		WorkingDir:   opts.WorkingDir,
		Env:          opts.Env,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := e.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := e.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{
		Tty: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	if opts.Stdin != nil {
		go func() {
			io.Copy(attachResp.Conn, opts.Stdin)
			attachResp.CloseWrite()
		}()
	}

	if opts.Stdout != nil {
		_, err = io.Copy(opts.Stdout, attachResp.Reader)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read exec output: %w", err)
		}
	}

	return nil
}

func stdCopy(stdout, stderr io.Writer, src io.Reader) (int64, error) {
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			nw, ew := stdout.Write(buf[:n])
			written += int64(nw)
			if ew != nil {
				return written, ew
			}
		}
		if err != nil {
			return written, err
		}
	}
}
