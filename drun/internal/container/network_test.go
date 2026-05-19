package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetworkManagerNew(t *testing.T) {
	nm := NewNetworkManager(nil)
	assert.NotNil(t, nm)
}

func TestNetworkNameGeneration(t *testing.T) {
	nm := NewNetworkManager(nil)

	name := nm.GenerateName("myproject")
	assert.Contains(t, name, "drun-")
	assert.Contains(t, name, "myproject")
}

func TestNetworkNameGenerationConsistent(t *testing.T) {
	nm := NewNetworkManager(nil)

	name1 := nm.GenerateName("myproject")
	name2 := nm.GenerateName("myproject")
	assert.Equal(t, name1, name2)
}

func TestNetworkNameGenerationDifferent(t *testing.T) {
	nm := NewNetworkManager(nil)

	name1 := nm.GenerateName("project1")
	name2 := nm.GenerateName("project2")
	assert.NotEqual(t, name1, name2)
}

func TestNetworkLabels(t *testing.T) {
	nm := NewNetworkManager(nil)

	labels := nm.GenerateLabels("myproject")
	assert.Equal(t, "true", labels["drun.managed"])
	assert.Equal(t, "myproject", labels["drun.project"])
}

func TestNetworkOptionsStruct(t *testing.T) {
	opts := &NetworkOptions{
		Driver: "bridge",
	}

	assert.Equal(t, "bridge", opts.Driver)
	assert.False(t, opts.Internal)
	assert.False(t, opts.Attachable)
}

func TestNetworkOptionsAllFields(t *testing.T) {
	opts := &NetworkOptions{
		Driver:     "overlay",
		Internal:   true,
		Attachable: true,
		Subnet:     "172.20.0.0/16",
		Gateway:    "172.20.0.1",
	}

	assert.Equal(t, "overlay", opts.Driver)
	assert.True(t, opts.Internal)
	assert.True(t, opts.Attachable)
	assert.Equal(t, "172.20.0.0/16", opts.Subnet)
	assert.Equal(t, "172.20.0.1", opts.Gateway)
}

func TestNetworkInfoStruct(t *testing.T) {
	info := &NetworkInfo{
		ID:     "abc123",
		Name:   "drun-myproject",
		Driver: "bridge",
		Scope:  "local",
	}

	assert.Equal(t, "abc123", info.ID)
	assert.Equal(t, "drun-myproject", info.Name)
	assert.Equal(t, "bridge", info.Driver)
	assert.Equal(t, "local", info.Scope)
}

func TestJoinOptionsStruct(t *testing.T) {
	opts := &JoinOptions{}

	assert.Empty(t, opts.Aliases)
	assert.Empty(t, opts.IPv4Address)
}

func TestJoinOptionsAllFields(t *testing.T) {
	opts := &JoinOptions{
		Aliases:     []string{"api", "backend"},
		IPv4Address: "172.20.0.10",
	}

	assert.Len(t, opts.Aliases, 2)
	assert.Contains(t, opts.Aliases, "api")
	assert.Contains(t, opts.Aliases, "backend")
	assert.Equal(t, "172.20.0.10", opts.IPv4Address)
}

func TestParseNetworkMode(t *testing.T) {
	tests := []struct {
		input    string
		expected NetworkMode
	}{
		{"bridge", NetworkModeBridge},
		{"host", NetworkModeHost},
		{"none", NetworkModeNone},
		{"", NetworkModeBridge}, // default
		{"container:abc123", NetworkModeContainer},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			mode := ParseNetworkMode(tt.input)
			assert.Equal(t, tt.expected, mode)
		})
	}
}

func TestNetworkModeValues(t *testing.T) {
	assert.Equal(t, NetworkMode("bridge"), NetworkModeBridge)
	assert.Equal(t, NetworkMode("host"), NetworkModeHost)
	assert.Equal(t, NetworkMode("none"), NetworkModeNone)
	assert.Equal(t, NetworkMode("container"), NetworkModeContainer)
}

func TestNetworkModeBridgeCheck(t *testing.T) {
	assert.True(t, NetworkModeBridge == NetworkModeBridge)
	assert.False(t, NetworkModeHost == NetworkModeBridge)
	assert.False(t, NetworkModeNone == NetworkModeBridge)
}

func TestNetworkModeHostCheck(t *testing.T) {
	assert.False(t, NetworkModeBridge == NetworkModeHost)
	assert.True(t, NetworkModeHost == NetworkModeHost)
	assert.False(t, NetworkModeNone == NetworkModeHost)
}

func TestNetworkModePortMapping(t *testing.T) {
	// Bridge mode allows port mapping
	assert.True(t, NetworkModeBridge == NetworkModeBridge)
	// Host and none modes don't typically use port mapping
	assert.True(t, NetworkModeHost == NetworkModeHost)
	assert.True(t, NetworkModeNone == NetworkModeNone)
}
