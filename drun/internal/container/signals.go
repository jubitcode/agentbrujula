package container

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/client"
)

type SignalForwarder struct {
	client *client.Client
}

func NewSignalForwarder(dockerClient *client.Client) *SignalForwarder {
	return &SignalForwarder{client: dockerClient}
}

type SignalOptions struct {
	GracePeriod time.Duration
	Signals     []os.Signal
}

var DefaultForwardSignals = []os.Signal{
	syscall.SIGINT,
	syscall.SIGTERM,
	syscall.SIGHUP,
	syscall.SIGQUIT,
}

var signalMap = map[string]syscall.Signal{
	"SIGINT":  syscall.SIGINT,
	"SIGTERM": syscall.SIGTERM,
	"SIGHUP":  syscall.SIGHUP,
	"SIGKILL": syscall.SIGKILL,
	"SIGQUIT": syscall.SIGQUIT,
	"SIGUSR1": syscall.SIGUSR1,
	"SIGUSR2": syscall.SIGUSR2,
	"SIGSTOP": syscall.SIGSTOP,
	"SIGCONT": syscall.SIGCONT,
}

func SignalNameToNumber(name string) (syscall.Signal, error) {
	name = strings.ToUpper(name)
	if !strings.HasPrefix(name, "SIG") {
		name = "SIG" + name
	}

	if sig, ok := signalMap[name]; ok {
		return sig, nil
	}

	return 0, fmt.Errorf("unknown signal: %s", name)
}

func SignalNumberToName(sig syscall.Signal) string {
	for name, s := range signalMap {
		if s == sig {
			return name
		}
	}
	return fmt.Sprintf("signal %d", sig)
}

func (f *SignalForwarder) Forward(ctx context.Context, containerID string, opts *SignalOptions) error {
	if opts == nil {
		opts = &SignalOptions{
			GracePeriod: 10 * time.Second,
			Signals:     DefaultForwardSignals,
		}
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, opts.Signals...)
	defer signal.Stop(sigChan)

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig := <-sigChan:
			if err := f.forwardSignal(ctx, containerID, sig, opts.GracePeriod); err != nil {
				return err
			}
		}
	}
}

func (f *SignalForwarder) forwardSignal(ctx context.Context, containerID string, sig os.Signal, gracePeriod time.Duration) error {
	sysSignal, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("invalid signal type")
	}

	signalName := SignalNumberToName(sysSignal)

	err := f.client.ContainerKill(ctx, containerID, signalName)
	if err != nil {
		return fmt.Errorf("failed to forward signal %s: %w", signalName, err)
	}

	if sysSignal == syscall.SIGTERM || sysSignal == syscall.SIGINT {
		waitCtx, cancel := context.WithTimeout(ctx, gracePeriod)
		defer cancel()

		statusCh, errCh := f.client.ContainerWait(waitCtx, containerID, "")
		select {
		case <-statusCh:
		case err := <-errCh:
			if err != nil && err != context.DeadlineExceeded {
				return err
			}
			f.client.ContainerKill(ctx, containerID, "SIGKILL")
		}
	}

	return nil
}

func (f *SignalForwarder) SendSignal(ctx context.Context, containerID string, sig syscall.Signal) error {
	signalName := SignalNumberToName(sig)
	return f.client.ContainerKill(ctx, containerID, signalName)
}

func (f *SignalForwarder) StopWithGrace(ctx context.Context, containerID string, gracePeriod time.Duration) error {
	err := f.SendSignal(ctx, containerID, syscall.SIGTERM)
	if err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, gracePeriod)
	defer cancel()

	statusCh, errCh := f.client.ContainerWait(waitCtx, containerID, "")
	select {
	case <-statusCh:
		return nil
	case err := <-errCh:
		if err == context.DeadlineExceeded {
			return f.SendSignal(ctx, containerID, syscall.SIGKILL)
		}
		return err
	}
}
