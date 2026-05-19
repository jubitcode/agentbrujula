package config

import (
	"fmt"
	"strconv"
	"strings"
)

type NetworkMode string

const (
	NetworkModeBridge NetworkMode = "bridge"
	NetworkModeHost   NetworkMode = "host"
	NetworkModeNone   NetworkMode = "none"
)

type NetworkConfig struct {
	Mode    string   `yaml:"mode" mapstructure:"mode"`
	Join    []string `yaml:"join" mapstructure:"join"`
	Aliases []string `yaml:"aliases" mapstructure:"aliases"`
}

func (n *NetworkConfig) Validate() error {
	validModes := map[string]bool{
		"bridge": true,
		"host":   true,
		"none":   true,
		"":       true, // empty defaults to bridge
	}

	if !validModes[n.Mode] {
		return &ConfigError{
			Field:   "network.mode",
			Message: fmt.Sprintf("invalid mode %q, must be one of: bridge, host, none", n.Mode),
		}
	}

	if n.Mode == "host" && len(n.Join) > 0 {
		return &ConfigError{
			Field:   "network.join",
			Message: "cannot join networks when using host network mode",
		}
	}

	return nil
}

func (n *NetworkConfig) GetMode() NetworkMode {
	if n.Mode == "" {
		return NetworkModeBridge
	}
	return NetworkMode(n.Mode)
}

type PortBinding struct {
	HostIP        string
	HostPort      string
	ContainerPort string
	Protocol      string
}

func ParsePortBinding(s string) (*PortBinding, error) {
	pb := &PortBinding{
		HostIP:   "0.0.0.0",
		Protocol: "tcp",
	}

	if idx := strings.LastIndex(s, "/"); idx != -1 {
		proto := s[idx+1:]
		if proto != "tcp" && proto != "udp" {
			return nil, fmt.Errorf("invalid protocol %q", proto)
		}
		pb.Protocol = proto
		s = s[:idx]
	}

	parts := strings.Split(s, ":")

	switch len(parts) {
	case 1:
		if err := validatePort(parts[0]); err != nil {
			return nil, err
		}
		pb.HostPort = parts[0]
		pb.ContainerPort = parts[0]

	case 2:
		if err := validatePort(parts[0]); err != nil {
			return nil, err
		}
		if err := validatePort(parts[1]); err != nil {
			return nil, err
		}
		pb.HostPort = parts[0]
		pb.ContainerPort = parts[1]

	case 3:
		pb.HostIP = parts[0]
		if err := validatePort(parts[1]); err != nil {
			return nil, err
		}
		if err := validatePort(parts[2]); err != nil {
			return nil, err
		}
		pb.HostPort = parts[1]
		pb.ContainerPort = parts[2]

	default:
		return nil, fmt.Errorf("invalid port format: %s", s)
	}

	return pb, nil
}

func (pb *PortBinding) String() string {
	base := fmt.Sprintf("%s:%s:%s", pb.HostIP, pb.HostPort, pb.ContainerPort)
	if pb.Protocol != "tcp" {
		base += "/" + pb.Protocol
	}
	return base
}

func validatePort(s string) error {
	if strings.Contains(s, "-") {
		parts := strings.Split(s, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid port range: %s", s)
		}
		for _, p := range parts {
			if err := validatePort(p); err != nil {
				return err
			}
		}
		return nil
	}

	port, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("invalid port number: %s", s)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port out of range: %d", port)
	}
	return nil
}
