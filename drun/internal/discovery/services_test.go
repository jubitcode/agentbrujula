package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceDiscoveryNew(t *testing.T) {
	sd := NewServiceDiscovery(nil)
	assert.NotNil(t, sd)
}

func TestServiceInfoNew(t *testing.T) {
	info := &ServiceInfo{
		Name:        "postgres",
		ContainerID: "abc123",
		Image:       "postgres:15",
		Status:      "running",
	}

	assert.Equal(t, "postgres", info.Name)
	assert.Equal(t, "abc123", info.ContainerID)
	assert.Equal(t, "postgres:15", info.Image)
}

func TestServiceInfoIsRunning(t *testing.T) {
	running := &ServiceInfo{Status: "running"}
	stopped := &ServiceInfo{Status: "exited"}
	created := &ServiceInfo{Status: "created"}

	assert.True(t, running.IsRunning())
	assert.False(t, stopped.IsRunning())
	assert.False(t, created.IsRunning())
}

func TestServiceSourceValues(t *testing.T) {
	assert.Equal(t, ServiceSource("compose"), ServiceSourceCompose)
	assert.Equal(t, ServiceSource("manual"), ServiceSourceManual)
	assert.Equal(t, ServiceSource("drun"), ServiceSourceDrun)
	assert.Equal(t, ServiceSource("unknown"), ServiceSourceUnknown)
}

func TestServiceInfoSource(t *testing.T) {
	composeService := &ServiceInfo{
		Labels: map[string]string{
			"com.docker.compose.project": "myproject",
			"com.docker.compose.service": "web",
		},
	}

	drunService := &ServiceInfo{
		Labels: map[string]string{
			"com.drun.project": "myproject",
		},
	}

	manualService := &ServiceInfo{
		Labels: map[string]string{},
	}

	assert.Equal(t, ServiceSourceCompose, composeService.Source())
	assert.Equal(t, ServiceSourceDrun, drunService.Source())
	assert.Equal(t, ServiceSourceManual, manualService.Source())
}

func TestServiceInfoNetworks(t *testing.T) {
	info := &ServiceInfo{
		Networks: []string{"frontend", "backend"},
	}

	assert.Contains(t, info.Networks, "frontend")
	assert.Contains(t, info.Networks, "backend")
}

func TestServiceInfoPorts(t *testing.T) {
	info := &ServiceInfo{
		Ports: []PortMapping{
			{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
			{HostPort: 443, ContainerPort: 443, Protocol: "tcp"},
		},
	}

	assert.Len(t, info.Ports, 2)
	assert.Equal(t, 8080, info.Ports[0].HostPort)
	assert.Equal(t, 80, info.Ports[0].ContainerPort)
}

func TestPortMappingString(t *testing.T) {
	pm := PortMapping{
		HostPort:      8080,
		ContainerPort: 80,
		Protocol:      "tcp",
	}

	assert.Equal(t, "8080:80/tcp", pm.String())
}

func TestPortMappingStringNoHost(t *testing.T) {
	pm := PortMapping{
		ContainerPort: 80,
		Protocol:      "tcp",
	}

	assert.Equal(t, "80/tcp", pm.String())
}

func TestSuggestConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		ports    []PortMapping
		expected string
	}{
		{
			name:  "postgres",
			image: "postgres:15",
			ports: []PortMapping{{HostPort: 5432, ContainerPort: 5432, Protocol: "tcp"}},
			expected: "postgres://localhost:5432/postgres",
		},
		{
			name:  "mysql",
			image: "mysql:8",
			ports: []PortMapping{{HostPort: 3306, ContainerPort: 3306, Protocol: "tcp"}},
			expected: "mysql://localhost:3306",
		},
		{
			name:  "redis",
			image: "redis:alpine",
			ports: []PortMapping{{HostPort: 6379, ContainerPort: 6379, Protocol: "tcp"}},
			expected: "redis://localhost:6379",
		},
		{
			name:  "mongodb",
			image: "mongo:6",
			ports: []PortMapping{{HostPort: 27017, ContainerPort: 27017, Protocol: "tcp"}},
			expected: "mongodb://localhost:27017",
		},
		{
			name:  "nginx",
			image: "nginx:latest",
			ports: []PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}},
			expected: "http://localhost:8080",
		},
		{
			name:     "unknown",
			image:    "myapp:latest",
			ports:    []PortMapping{{HostPort: 9000, ContainerPort: 9000, Protocol: "tcp"}},
			expected: "localhost:9000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &ServiceInfo{
				Name:  tt.name,
				Image: tt.image,
				Ports: tt.ports,
			}

			suggestion := SuggestConnectionString(info)
			assert.Equal(t, tt.expected, suggestion)
		})
	}
}

func TestSuggestConnectionStringNoPorts(t *testing.T) {
	info := &ServiceInfo{
		Name:  "db",
		Image: "postgres:15",
		Ports: []PortMapping{},
	}

	suggestion := SuggestConnectionString(info)
	assert.Empty(t, suggestion)
}

func TestSuggestConnectionStringByNetwork(t *testing.T) {
	info := &ServiceInfo{
		Name:     "db",
		Image:    "postgres:15",
		Networks: []string{"myproject_default"},
	}

	suggestion := SuggestConnectionStringByNetwork(info, "myproject_default")
	assert.Equal(t, "postgres://db:5432/postgres", suggestion)
}

func TestNetworkInfoNew(t *testing.T) {
	info := &NetworkInfo{
		ID:     "abc123",
		Name:   "mynetwork",
		Driver: "bridge",
	}

	assert.Equal(t, "abc123", info.ID)
	assert.Equal(t, "mynetwork", info.Name)
	assert.Equal(t, "bridge", info.Driver)
}

func TestNetworkInfoContainers(t *testing.T) {
	info := &NetworkInfo{
		Name:       "mynetwork",
		Containers: []string{"container1", "container2"},
	}

	assert.Len(t, info.Containers, 2)
	assert.Contains(t, info.Containers, "container1")
}

func TestDiscoveryResultNew(t *testing.T) {
	result := NewDiscoveryResult()
	assert.NotNil(t, result)
	assert.Empty(t, result.Services)
	assert.Empty(t, result.Networks)
}

func TestDiscoveryResultAddService(t *testing.T) {
	result := NewDiscoveryResult()
	service := &ServiceInfo{Name: "web", Image: "nginx"}

	result.AddService(service)
	assert.Len(t, result.Services, 1)
	assert.Equal(t, "web", result.Services[0].Name)
}

func TestDiscoveryResultAddNetwork(t *testing.T) {
	result := NewDiscoveryResult()
	network := &NetworkInfo{Name: "frontend", Driver: "bridge"}

	result.AddNetwork(network)
	assert.Len(t, result.Networks, 1)
	assert.Equal(t, "frontend", result.Networks[0].Name)
}

func TestDiscoveryResultGetServicesByNetwork(t *testing.T) {
	result := NewDiscoveryResult()
	result.AddService(&ServiceInfo{Name: "web", Networks: []string{"frontend", "backend"}})
	result.AddService(&ServiceInfo{Name: "api", Networks: []string{"backend"}})
	result.AddService(&ServiceInfo{Name: "db", Networks: []string{"backend"}})

	frontendServices := result.GetServicesByNetwork("frontend")
	assert.Len(t, frontendServices, 1)

	backendServices := result.GetServicesByNetwork("backend")
	assert.Len(t, backendServices, 3)
}

func TestDiscoveryResultGetRunningServices(t *testing.T) {
	result := NewDiscoveryResult()
	result.AddService(&ServiceInfo{Name: "web", Status: "running"})
	result.AddService(&ServiceInfo{Name: "worker", Status: "exited"})
	result.AddService(&ServiceInfo{Name: "db", Status: "running"})

	running := result.GetRunningServices()
	assert.Len(t, running, 2)
}

func TestDiscoveryResultGetComposeServices(t *testing.T) {
	result := NewDiscoveryResult()
	result.AddService(&ServiceInfo{
		Name: "web",
		Labels: map[string]string{
			"com.docker.compose.project": "myproject",
		},
	})
	result.AddService(&ServiceInfo{
		Name:   "manual",
		Labels: map[string]string{},
	})

	composeServices := result.GetServicesBySource(ServiceSourceCompose)
	assert.Len(t, composeServices, 1)
	assert.Equal(t, "web", composeServices[0].Name)
}

func TestServiceInfoGetLabel(t *testing.T) {
	info := &ServiceInfo{
		Labels: map[string]string{
			"app":     "myapp",
			"version": "1.0",
		},
	}

	assert.Equal(t, "myapp", info.GetLabel("app"))
	assert.Equal(t, "1.0", info.GetLabel("version"))
	assert.Empty(t, info.GetLabel("nonexistent"))
}

func TestServiceInfoComposeProject(t *testing.T) {
	info := &ServiceInfo{
		Labels: map[string]string{
			"com.docker.compose.project": "myproject",
			"com.docker.compose.service": "web",
		},
	}

	assert.Equal(t, "myproject", info.ComposeProject())
	assert.Equal(t, "web", info.ComposeService())
}

func TestServiceInfoComposeProjectEmpty(t *testing.T) {
	info := &ServiceInfo{Labels: map[string]string{}}

	assert.Empty(t, info.ComposeProject())
	assert.Empty(t, info.ComposeService())
}
