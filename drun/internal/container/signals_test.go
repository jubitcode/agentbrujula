package container

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSignalForwarderNew(t *testing.T) {
	sf := NewSignalForwarder(nil)
	assert.NotNil(t, sf)
}

func TestSignalOptionsStruct(t *testing.T) {
	opts := &SignalOptions{
		GracePeriod: 10 * time.Second,
		Signals:     DefaultForwardSignals,
	}

	assert.Equal(t, 10*time.Second, opts.GracePeriod)
	assert.NotNil(t, opts.Signals)
}

func TestSignalOptionsCustom(t *testing.T) {
	opts := &SignalOptions{
		GracePeriod: 30 * time.Second,
		Signals:     []os.Signal{syscall.SIGINT, syscall.SIGTERM},
	}
	assert.Equal(t, 30*time.Second, opts.GracePeriod)
	assert.Contains(t, opts.Signals, syscall.SIGINT)
	assert.Contains(t, opts.Signals, syscall.SIGTERM)
}

func TestDefaultForwardSignals(t *testing.T) {
	signals := DefaultForwardSignals

	assert.Contains(t, signals, syscall.SIGINT)
	assert.Contains(t, signals, syscall.SIGTERM)
	assert.Contains(t, signals, syscall.SIGHUP)
}

func TestSignalNameToNumber(t *testing.T) {
	tests := []struct {
		name     string
		expected syscall.Signal
	}{
		{"SIGINT", syscall.SIGINT},
		{"SIGTERM", syscall.SIGTERM},
		{"SIGHUP", syscall.SIGHUP},
		{"SIGKILL", syscall.SIGKILL},
		{"SIGUSR1", syscall.SIGUSR1},
		{"SIGUSR2", syscall.SIGUSR2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := SignalNameToNumber(tt.name)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, sig)
		})
	}
}

func TestSignalNameToNumberInvalid(t *testing.T) {
	_, err := SignalNameToNumber("INVALID")
	assert.Error(t, err)
}

func TestSignalNameToNumberWithoutPrefix(t *testing.T) {
	sig, err := SignalNameToNumber("INT")
	assert.NoError(t, err)
	assert.Equal(t, syscall.SIGINT, sig)
}

func TestSignalNumberToName(t *testing.T) {
	name := SignalNumberToName(syscall.SIGINT)
	assert.Equal(t, "SIGINT", name)

	name = SignalNumberToName(syscall.SIGTERM)
	assert.Equal(t, "SIGTERM", name)
}
