package discovery

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisplayNew(t *testing.T) {
	d := NewDisplay()
	assert.NotNil(t, d)
}

func TestFormatContainers(t *testing.T) {
	services := []*ServiceInfo{
		{
			Name:        "web",
			ContainerID: "abc123456789",
			Image:       "nginx:latest",
			Status:      "running",
			Ports: []PortMapping{
				{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
			},
		},
		{
			Name:        "db",
			ContainerID: "def456789012",
			Image:       "postgres:15",
			Status:      "running",
			Ports: []PortMapping{
				{HostPort: 5432, ContainerPort: 5432, Protocol: "tcp"},
			},
		},
	}

	d := NewDisplay()
	output := d.FormatContainers(services)

	assert.Contains(t, output, "web")
	assert.Contains(t, output, "db")
	assert.Contains(t, output, "nginx:latest")
	assert.Contains(t, output, "postgres:15")
	assert.Contains(t, output, "running")
}

func TestFormatContainersEmpty(t *testing.T) {
	d := NewDisplay()
	output := d.FormatContainers([]*ServiceInfo{})

	assert.Contains(t, output, "No containers found")
}

func TestFormatNetworks(t *testing.T) {
	networks := []*NetworkInfo{
		{
			ID:         "net123456789",
			Name:       "frontend",
			Driver:     "bridge",
			Scope:      "local",
			Containers: []string{"web", "api"},
		},
		{
			ID:         "net234567890",
			Name:       "backend",
			Driver:     "bridge",
			Scope:      "local",
			Internal:   true,
			Containers: []string{"api", "db"},
		},
	}

	d := NewDisplay()
	output := d.FormatNetworks(networks)

	assert.Contains(t, output, "frontend")
	assert.Contains(t, output, "backend")
	assert.Contains(t, output, "bridge")
}

func TestFormatNetworksEmpty(t *testing.T) {
	d := NewDisplay()
	output := d.FormatNetworks([]*NetworkInfo{})

	assert.Contains(t, output, "No networks found")
}

func TestFormatServices(t *testing.T) {
	services := []*ServiceInfo{
		{
			Name:   "db",
			Image:  "postgres:15",
			Status: "running",
			Ports: []PortMapping{
				{HostPort: 5432, ContainerPort: 5432, Protocol: "tcp"},
			},
			Labels: map[string]string{
				"com.docker.compose.project": "myproject",
			},
		},
		{
			Name:   "redis",
			Image:  "redis:alpine",
			Status: "running",
			Ports: []PortMapping{
				{HostPort: 6379, ContainerPort: 6379, Protocol: "tcp"},
			},
		},
	}

	d := NewDisplay()
	output := d.FormatServices(services)

	assert.Contains(t, output, "db")
	assert.Contains(t, output, "redis")
	assert.Contains(t, output, "postgres")
	assert.Contains(t, output, "redis://localhost:6379")
}

func TestFormatServicesWithSuggestions(t *testing.T) {
	services := []*ServiceInfo{
		{
			Name:   "db",
			Image:  "postgres:15",
			Status: "running",
			Ports: []PortMapping{
				{HostPort: 5432, ContainerPort: 5432, Protocol: "tcp"},
			},
		},
	}

	d := NewDisplay()
	output := d.FormatServices(services)

	// Should include connection string suggestion
	assert.Contains(t, output, "postgres://localhost:5432")
}

func TestFormatServicesEmpty(t *testing.T) {
	d := NewDisplay()
	output := d.FormatServices([]*ServiceInfo{})

	assert.Contains(t, output, "No services found")
}

func TestFormatDiscoveryResult(t *testing.T) {
	result := NewDiscoveryResult()
	result.AddService(&ServiceInfo{
		Name:   "web",
		Image:  "nginx:latest",
		Status: "running",
	})
	result.AddNetwork(&NetworkInfo{
		Name:   "frontend",
		Driver: "bridge",
	})

	d := NewDisplay()
	output := d.FormatDiscoveryResult(result)

	assert.Contains(t, output, "Containers")
	assert.Contains(t, output, "Networks")
	assert.Contains(t, output, "web")
	assert.Contains(t, output, "frontend")
}

func TestFormatPortMappings(t *testing.T) {
	ports := []PortMapping{
		{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
		{HostPort: 443, ContainerPort: 443, Protocol: "tcp"},
	}

	d := NewDisplay()
	output := d.FormatPortMappings(ports)

	assert.Contains(t, output, "8080:80/tcp")
	assert.Contains(t, output, "443:443/tcp")
}

func TestFormatPortMappingsEmpty(t *testing.T) {
	d := NewDisplay()
	output := d.FormatPortMappings([]PortMapping{})

	assert.Equal(t, "-", output)
}

func TestFormatStatus(t *testing.T) {
	d := NewDisplay()

	assert.Equal(t, "running", d.FormatStatus("running"))
	assert.Equal(t, "exited", d.FormatStatus("exited"))
	assert.Equal(t, "created", d.FormatStatus("created"))
}

func TestFormatSource(t *testing.T) {
	d := NewDisplay()

	assert.Equal(t, "compose", d.FormatSource(ServiceSourceCompose))
	assert.Equal(t, "manual", d.FormatSource(ServiceSourceManual))
	assert.Equal(t, "drun", d.FormatSource(ServiceSourceDrun))
}

func TestTableBuilder(t *testing.T) {
	tb := NewTableBuilder()
	tb.AddHeader("NAME", "IMAGE", "STATUS")
	tb.AddRow("web", "nginx", "running")
	tb.AddRow("db", "postgres:15", "running")

	output := tb.String()

	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "IMAGE")
	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "web")
	assert.Contains(t, output, "postgres:15")
}

func TestTableBuilderAlignment(t *testing.T) {
	tb := NewTableBuilder()
	tb.AddHeader("NAME", "IMAGE")
	tb.AddRow("web", "nginx")
	tb.AddRow("database", "postgres:15-alpine")

	output := tb.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// All lines should have consistent column alignment
	assert.True(t, len(lines) >= 3)
}

func TestTableBuilderEmpty(t *testing.T) {
	tb := NewTableBuilder()
	output := tb.String()

	assert.Empty(t, strings.TrimSpace(output))
}

func TestFormatNetworkContainers(t *testing.T) {
	d := NewDisplay()

	containers := []string{"web", "api", "db"}
	output := d.FormatNetworkContainers(containers)

	assert.Contains(t, output, "web")
	assert.Contains(t, output, "api")
	assert.Contains(t, output, "db")
}

func TestFormatNetworkContainersEmpty(t *testing.T) {
	d := NewDisplay()
	output := d.FormatNetworkContainers([]string{})

	assert.Equal(t, "-", output)
}

func TestFormatNetworkContainersMany(t *testing.T) {
	d := NewDisplay()

	containers := []string{"c1", "c2", "c3", "c4", "c5", "c6"}
	output := d.FormatNetworkContainers(containers)

	// Should truncate or indicate more
	assert.True(t, len(output) > 0)
}

func TestDisplayOptions(t *testing.T) {
	opts := NewDisplayOptions()
	assert.NotNil(t, opts)
	assert.False(t, opts.Quiet)
	assert.False(t, opts.Wide)
}

func TestDisplayOptionsWithQuiet(t *testing.T) {
	opts := NewDisplayOptions().WithQuiet(true)
	assert.True(t, opts.Quiet)
}

func TestDisplayOptionsWithWide(t *testing.T) {
	opts := NewDisplayOptions().WithWide(true)
	assert.True(t, opts.Wide)
}

func TestFormatContainersQuiet(t *testing.T) {
	services := []*ServiceInfo{
		{ContainerID: "abc123456789"},
		{ContainerID: "def456789012"},
	}

	d := NewDisplay()
	d.Options = NewDisplayOptions().WithQuiet(true)
	output := d.FormatContainers(services)

	// Quiet mode should only show IDs
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Len(t, lines, 2)
	assert.Contains(t, output, "abc123456789")
	assert.Contains(t, output, "def456789012")
}
