package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkModeValidation(t *testing.T) {
	tests := []struct {
		mode    string
		wantErr bool
	}{
		{"bridge", false},
		{"host", false},
		{"none", false},
		{"", false}, // defaults to bridge
		{"invalid", true},
		{"BRIDGE", true}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			nc := &NetworkConfig{Mode: tt.mode}
			err := nc.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNetworkJoinWithHostMode(t *testing.T) {
	nc := &NetworkConfig{
		Mode: "host",
		Join: []string{"mynet"},
	}

	err := nc.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot join networks")
}

func TestNetworkGetMode(t *testing.T) {
	tests := []struct {
		mode     string
		expected NetworkMode
	}{
		{"bridge", NetworkModeBridge},
		{"host", NetworkModeHost},
		{"none", NetworkModeNone},
		{"", NetworkModeBridge}, // default
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			nc := &NetworkConfig{Mode: tt.mode}
			assert.Equal(t, tt.expected, nc.GetMode())
		})
	}
}

func TestPortBindingParseSimple(t *testing.T) {
	pb, err := ParsePortBinding("8080")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", pb.HostIP)
	assert.Equal(t, "8080", pb.HostPort)
	assert.Equal(t, "8080", pb.ContainerPort)
	assert.Equal(t, "tcp", pb.Protocol)
}

func TestPortBindingParseMapping(t *testing.T) {
	pb, err := ParsePortBinding("8080:80")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", pb.HostIP)
	assert.Equal(t, "8080", pb.HostPort)
	assert.Equal(t, "80", pb.ContainerPort)
	assert.Equal(t, "tcp", pb.Protocol)
}

func TestPortBindingParseWithIP(t *testing.T) {
	pb, err := ParsePortBinding("127.0.0.1:8080:80")
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1", pb.HostIP)
	assert.Equal(t, "8080", pb.HostPort)
	assert.Equal(t, "80", pb.ContainerPort)
	assert.Equal(t, "tcp", pb.Protocol)
}

func TestPortBindingParseWithUDP(t *testing.T) {
	pb, err := ParsePortBinding("8080:80/udp")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", pb.HostIP)
	assert.Equal(t, "8080", pb.HostPort)
	assert.Equal(t, "80", pb.ContainerPort)
	assert.Equal(t, "udp", pb.Protocol)
}

func TestPortBindingParseRange(t *testing.T) {
	pb, err := ParsePortBinding("8080-8090:80-90")
	require.NoError(t, err)

	assert.Equal(t, "8080-8090", pb.HostPort)
	assert.Equal(t, "80-90", pb.ContainerPort)
}

func TestPortBindingParseInvalidPort(t *testing.T) {
	_, err := ParsePortBinding("abc")
	assert.Error(t, err)
}

func TestPortBindingParsePortOutOfRange(t *testing.T) {
	_, err := ParsePortBinding("99999")
	assert.Error(t, err)
}

func TestPortBindingParseInvalidFormat(t *testing.T) {
	_, err := ParsePortBinding("a:b:c:d")
	assert.Error(t, err)
}

func TestPortBindingParseInvalidProtocol(t *testing.T) {
	_, err := ParsePortBinding("8080/sctp")
	assert.Error(t, err)
}

func TestPortBindingString(t *testing.T) {
	pb := &PortBinding{
		HostIP:        "127.0.0.1",
		HostPort:      "8080",
		ContainerPort: "80",
		Protocol:      "tcp",
	}

	assert.Equal(t, "127.0.0.1:8080:80", pb.String())
}

func TestPortBindingStringWithUDP(t *testing.T) {
	pb := &PortBinding{
		HostIP:        "0.0.0.0",
		HostPort:      "8080",
		ContainerPort: "80",
		Protocol:      "udp",
	}

	assert.Equal(t, "0.0.0.0:8080:80/udp", pb.String())
}
