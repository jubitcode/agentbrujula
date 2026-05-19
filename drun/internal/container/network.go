package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type NetworkMode string

const (
	NetworkModeBridge    NetworkMode = "bridge"
	NetworkModeHost      NetworkMode = "host"
	NetworkModeNone      NetworkMode = "none"
	NetworkModeContainer NetworkMode = "container"
)

func ParseNetworkMode(s string) NetworkMode {
	switch strings.ToLower(s) {
	case "host":
		return NetworkModeHost
	case "none":
		return NetworkModeNone
	case "":
		return NetworkModeBridge
	default:
		if strings.HasPrefix(s, "container:") {
			return NetworkModeContainer
		}
		return NetworkModeBridge
	}
}

type NetworkManager struct {
	client *client.Client
}

func NewNetworkManager(dockerClient *client.Client) *NetworkManager {
	return &NetworkManager{client: dockerClient}
}

type NetworkOptions struct {
	Driver     string
	Internal   bool
	Attachable bool
	Subnet     string
	Gateway    string
	Labels     map[string]string
}

type JoinOptions struct {
	Aliases     []string
	IPv4Address string
	IPv6Address string
}

type NetworkInfo struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Internal   bool
	Attachable bool
	Containers []string
}

func (m *NetworkManager) GenerateName(project string) string {
	return fmt.Sprintf("drun-%s", project)
}

func (m *NetworkManager) GenerateLabels(project string) map[string]string {
	return map[string]string{
		"drun.managed": "true",
		"drun.project": project,
	}
}

func (m *NetworkManager) Create(ctx context.Context, name string, opts *NetworkOptions) (string, error) {
	if opts == nil {
		opts = &NetworkOptions{Driver: "bridge"}
	}

	createOpts := network.CreateOptions{
		Driver:     opts.Driver,
		Internal:   opts.Internal,
		Attachable: opts.Attachable,
		Labels:     opts.Labels,
	}

	if opts.Subnet != "" {
		ipamConfig := network.IPAMConfig{Subnet: opts.Subnet}
		if opts.Gateway != "" {
			ipamConfig.Gateway = opts.Gateway
		}
		createOpts.IPAM = &network.IPAM{
			Config: []network.IPAMConfig{ipamConfig},
		}
	}

	resp, err := m.client.NetworkCreate(ctx, name, createOpts)
	if err != nil {
		return "", fmt.Errorf("failed to create network: %w", err)
	}

	return resp.ID, nil
}

func (m *NetworkManager) Remove(ctx context.Context, networkID string) error {
	return m.client.NetworkRemove(ctx, networkID)
}

func (m *NetworkManager) Inspect(ctx context.Context, networkID string) (*NetworkInfo, error) {
	resp, err := m.client.NetworkInspect(ctx, networkID, network.InspectOptions{})
	if err != nil {
		return nil, err
	}

	containers := make([]string, 0, len(resp.Containers))
	for id := range resp.Containers {
		containers = append(containers, id)
	}

	return &NetworkInfo{
		ID:         resp.ID,
		Name:       resp.Name,
		Driver:     resp.Driver,
		Scope:      resp.Scope,
		Internal:   resp.Internal,
		Attachable: resp.Attachable,
		Containers: containers,
	}, nil
}

func (m *NetworkManager) Exists(ctx context.Context, name string) (bool, error) {
	_, err := m.client.NetworkInspect(ctx, name, network.InspectOptions{})
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (m *NetworkManager) EnsureNetwork(ctx context.Context, name string, opts *NetworkOptions) (string, error) {
	exists, err := m.Exists(ctx, name)
	if err != nil {
		return "", err
	}

	if exists {
		info, err := m.Inspect(ctx, name)
		if err != nil {
			return "", err
		}
		return info.ID, nil
	}

	return m.Create(ctx, name, opts)
}

func (m *NetworkManager) Connect(ctx context.Context, networkID, containerID string, opts *JoinOptions) error {
	if opts == nil {
		opts = &JoinOptions{}
	}

	endpointSettings := &network.EndpointSettings{
		Aliases: opts.Aliases,
	}

	if opts.IPv4Address != "" || opts.IPv6Address != "" {
		endpointSettings.IPAMConfig = &network.EndpointIPAMConfig{
			IPv4Address: opts.IPv4Address,
			IPv6Address: opts.IPv6Address,
		}
	}

	return m.client.NetworkConnect(ctx, networkID, containerID, endpointSettings)
}

func (m *NetworkManager) Disconnect(ctx context.Context, networkID, containerID string, force bool) error {
	return m.client.NetworkDisconnect(ctx, networkID, containerID, force)
}

func (m *NetworkManager) ListDrunNetworks(ctx context.Context) ([]*NetworkInfo, error) {
	networks, err := m.client.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, err
	}

	var result []*NetworkInfo
	for _, n := range networks {
		if n.Labels["drun.managed"] == "true" {
			result = append(result, &NetworkInfo{
				ID:         n.ID,
				Name:       n.Name,
				Driver:     n.Driver,
				Scope:      n.Scope,
				Internal:   n.Internal,
				Attachable: n.Attachable,
			})
		}
	}

	return result, nil
}
