package discovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type ServiceSource string

const (
	ServiceSourceCompose ServiceSource = "compose"
	ServiceSourceManual  ServiceSource = "manual"
	ServiceSourceDrun    ServiceSource = "drun"
	ServiceSourceUnknown ServiceSource = "unknown"
)

type ServiceDiscovery struct {
	client *client.Client
}

func NewServiceDiscovery(dockerClient *client.Client) *ServiceDiscovery {
	return &ServiceDiscovery{
		client: dockerClient,
	}
}

type ServiceInfo struct {
	Name        string
	ContainerID string
	Image       string
	Status      string
	Networks    []string
	Ports       []PortMapping
	Labels      map[string]string
	Created     string
}

type PortMapping struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string
}

func (p PortMapping) String() string {
	if p.HostPort == 0 {
		return fmt.Sprintf("%d/%s", p.ContainerPort, p.Protocol)
	}
	return fmt.Sprintf("%d:%d/%s", p.HostPort, p.ContainerPort, p.Protocol)
}

type NetworkInfo struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Internal   bool
	Containers []string
	Labels     map[string]string
}

type DiscoveryResult struct {
	Services []*ServiceInfo
	Networks []*NetworkInfo
}

func NewDiscoveryResult() *DiscoveryResult {
	return &DiscoveryResult{
		Services: make([]*ServiceInfo, 0),
		Networks: make([]*NetworkInfo, 0),
	}
}

func (r *DiscoveryResult) AddService(s *ServiceInfo) {
	r.Services = append(r.Services, s)
}

func (r *DiscoveryResult) AddNetwork(n *NetworkInfo) {
	r.Networks = append(r.Networks, n)
}

func (r *DiscoveryResult) GetServicesByNetwork(networkName string) []*ServiceInfo {
	var result []*ServiceInfo
	for _, s := range r.Services {
		for _, n := range s.Networks {
			if n == networkName {
				result = append(result, s)
				break
			}
		}
	}
	return result
}

func (r *DiscoveryResult) GetRunningServices() []*ServiceInfo {
	var result []*ServiceInfo
	for _, s := range r.Services {
		if s.IsRunning() {
			result = append(result, s)
		}
	}
	return result
}

func (r *DiscoveryResult) GetServicesBySource(source ServiceSource) []*ServiceInfo {
	var result []*ServiceInfo
	for _, s := range r.Services {
		if s.Source() == source {
			result = append(result, s)
		}
	}
	return result
}

func (s *ServiceInfo) IsRunning() bool {
	return s.Status == "running"
}

func (s *ServiceInfo) Source() ServiceSource {
	if s.Labels == nil {
		return ServiceSourceManual
	}

	if _, ok := s.Labels["com.docker.compose.project"]; ok {
		return ServiceSourceCompose
	}

	if _, ok := s.Labels["com.drun.project"]; ok {
		return ServiceSourceDrun
	}

	return ServiceSourceManual
}

func (s *ServiceInfo) GetLabel(key string) string {
	if s.Labels == nil {
		return ""
	}
	return s.Labels[key]
}

func (s *ServiceInfo) ComposeProject() string {
	return s.GetLabel("com.docker.compose.project")
}

func (s *ServiceInfo) ComposeService() string {
	return s.GetLabel("com.docker.compose.service")
}

func (sd *ServiceDiscovery) Discover(ctx context.Context) (*DiscoveryResult, error) {
	if sd.client == nil {
		return nil, fmt.Errorf("no Docker client available")
	}

	result := NewDiscoveryResult()

	containers, err := sd.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, c := range containers {
		service := sd.containerToServiceInfo(c)
		result.AddService(service)
	}

	networks, err := sd.client.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	for _, n := range networks {
		network := sd.networkToNetworkInfo(n)
		result.AddNetwork(network)
	}

	return result, nil
}

func (sd *ServiceDiscovery) DiscoverByProject(ctx context.Context, project string) (*DiscoveryResult, error) {
	if sd.client == nil {
		return nil, fmt.Errorf("no Docker client available")
	}

	result := NewDiscoveryResult()

	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("com.docker.compose.project=%s", project))

	containers, err := sd.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, c := range containers {
		service := sd.containerToServiceInfo(c)
		result.AddService(service)
	}

	networks, err := sd.client.NetworkList(ctx, types.NetworkListOptions{
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	for _, n := range networks {
		network := sd.networkToNetworkInfo(n)
		result.AddNetwork(network)
	}

	return result, nil
}

func (sd *ServiceDiscovery) containerToServiceInfo(c types.Container) *ServiceInfo {
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	networks := make([]string, 0)
	if c.NetworkSettings != nil {
		for netName := range c.NetworkSettings.Networks {
			networks = append(networks, netName)
		}
	}

	ports := make([]PortMapping, 0)
	for _, p := range c.Ports {
		ports = append(ports, PortMapping{
			HostIP:        p.IP,
			HostPort:      int(p.PublicPort),
			ContainerPort: int(p.PrivatePort),
			Protocol:      p.Type,
		})
	}

	return &ServiceInfo{
		Name:        name,
		ContainerID: c.ID[:12],
		Image:       c.Image,
		Status:      c.State,
		Networks:    networks,
		Ports:       ports,
		Labels:      c.Labels,
		Created:     fmt.Sprintf("%d", c.Created),
	}
}

func (sd *ServiceDiscovery) networkToNetworkInfo(n types.NetworkResource) *NetworkInfo {
	containers := make([]string, 0)
	for _, c := range n.Containers {
		containers = append(containers, c.Name)
	}

	return &NetworkInfo{
		ID:         n.ID[:12],
		Name:       n.Name,
		Driver:     n.Driver,
		Scope:      n.Scope,
		Internal:   n.Internal,
		Containers: containers,
		Labels:     n.Labels,
	}
}

func SuggestConnectionString(info *ServiceInfo) string {
	if len(info.Ports) == 0 {
		return ""
	}

	port := info.Ports[0]
	if port.HostPort == 0 {
		return ""
	}

	return suggestConnectionStringForImage(info.Image, "localhost", port.HostPort)
}

func SuggestConnectionStringByNetwork(info *ServiceInfo, network string) string {
	port := getDefaultPort(info.Image)
	if port == 0 {
		return ""
	}

	return suggestConnectionStringForImage(info.Image, info.Name, port)
}

func suggestConnectionStringForImage(image, host string, port int) string {
	imageLower := strings.ToLower(image)

	switch {
	case strings.Contains(imageLower, "postgres"):
		return fmt.Sprintf("postgres://%s:%d/postgres", host, port)
	case strings.Contains(imageLower, "mysql"):
		return fmt.Sprintf("mysql://%s:%d", host, port)
	case strings.Contains(imageLower, "mariadb"):
		return fmt.Sprintf("mysql://%s:%d", host, port)
	case strings.Contains(imageLower, "redis"):
		return fmt.Sprintf("redis://%s:%d", host, port)
	case strings.Contains(imageLower, "mongo"):
		return fmt.Sprintf("mongodb://%s:%d", host, port)
	case strings.Contains(imageLower, "nginx"):
		return fmt.Sprintf("http://%s:%d", host, port)
	case strings.Contains(imageLower, "httpd"):
		return fmt.Sprintf("http://%s:%d", host, port)
	case strings.Contains(imageLower, "rabbitmq"):
		return fmt.Sprintf("amqp://%s:%d", host, port)
	case strings.Contains(imageLower, "kafka"):
		return fmt.Sprintf("%s:%d", host, port)
	case strings.Contains(imageLower, "elasticsearch"):
		return fmt.Sprintf("http://%s:%d", host, port)
	default:
		return fmt.Sprintf("%s:%d", host, port)
	}
}

func getDefaultPort(image string) int {
	imageLower := strings.ToLower(image)

	switch {
	case strings.Contains(imageLower, "postgres"):
		return 5432
	case strings.Contains(imageLower, "mysql"):
		return 3306
	case strings.Contains(imageLower, "mariadb"):
		return 3306
	case strings.Contains(imageLower, "redis"):
		return 6379
	case strings.Contains(imageLower, "mongo"):
		return 27017
	case strings.Contains(imageLower, "nginx"):
		return 80
	case strings.Contains(imageLower, "httpd"):
		return 80
	case strings.Contains(imageLower, "rabbitmq"):
		return 5672
	case strings.Contains(imageLower, "kafka"):
		return 9092
	case strings.Contains(imageLower, "elasticsearch"):
		return 9200
	default:
		return 0
	}
}
