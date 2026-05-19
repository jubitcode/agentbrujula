package discovery

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ComposeDiscovery struct{}

func NewComposeDiscovery() *ComposeDiscovery {
	return &ComposeDiscovery{}
}

type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
	Networks map[string]ComposeNetwork `yaml:"networks"`
	Volumes  map[string]ComposeVolume  `yaml:"volumes"`
	Path     string                    `yaml:"-"`
}

type ComposeService struct {
	Image         string        `yaml:"image"`
	Build         interface{}   `yaml:"build"`
	ContainerName string        `yaml:"container_name"`
	Ports         []interface{} `yaml:"ports"`
	Environment   []string      `yaml:"-"`
	EnvironmentRaw interface{}  `yaml:"environment"`
	Volumes       []string      `yaml:"volumes"`
	Networks      []string      `yaml:"-"`
	NetworksRaw   interface{}   `yaml:"networks"`
	DependsOn     []string      `yaml:"-"`
	DependsOnRaw  interface{}   `yaml:"depends_on"`
	Command       interface{}   `yaml:"command"`
	Entrypoint    interface{}   `yaml:"entrypoint"`
	WorkingDir    string        `yaml:"working_dir"`
	User          string        `yaml:"user"`
	Restart       string        `yaml:"restart"`
	Labels        interface{}   `yaml:"labels"`
}

type ComposeNetwork struct {
	Driver   string            `yaml:"driver"`
	External bool              `yaml:"external"`
	Internal bool              `yaml:"internal"`
	Labels   map[string]string `yaml:"labels"`
}

type ComposeVolume struct {
	Driver   string            `yaml:"driver"`
	External bool              `yaml:"external"`
	Labels   map[string]string `yaml:"labels"`
}

var composeFileNames = []string{
	"docker-compose.yml",
	"docker-compose.yaml",
	"compose.yml",
	"compose.yaml",
}

func (c *ComposeDiscovery) FindComposeFile(dir string) (string, error) {
	for _, name := range composeFileNames {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", nil
}

func (c *ComposeDiscovery) Parse(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	compose.Path = path

	for name, service := range compose.Services {
		service.Environment = normalizeEnvironment(service.EnvironmentRaw)
		service.Networks = normalizeNetworks(service.NetworksRaw)
		service.DependsOn = normalizeDependsOn(service.DependsOnRaw)
		compose.Services[name] = service
	}

	if compose.Services == nil {
		compose.Services = make(map[string]ComposeService)
	}
	if compose.Networks == nil {
		compose.Networks = make(map[string]ComposeNetwork)
	}
	if compose.Volumes == nil {
		compose.Volumes = make(map[string]ComposeVolume)
	}

	return &compose, nil
}

func (c *ComposeDiscovery) ProjectName(dir string) string {
	if name := os.Getenv("COMPOSE_PROJECT_NAME"); name != "" {
		return name
	}

	return filepath.Base(dir)
}

func (c *ComposeDiscovery) NetworkName(project, network string) string {
	return fmt.Sprintf("%s_%s", project, network)
}

func normalizeEnvironment(raw interface{}) []string {
	if raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for key, val := range v {
			if val == nil {
				result = append(result, key)
			} else {
				result = append(result, fmt.Sprintf("%s=%v", key, val))
			}
		}
		return result
	}

	return nil
}

func normalizeNetworks(raw interface{}) []string {
	if raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for name := range v {
			result = append(result, name)
		}
		return result
	}

	return nil
}

func normalizeDependsOn(raw interface{}) []string {
	if raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for name := range v {
			result = append(result, name)
		}
		return result
	}

	return nil
}

func (c *ComposeDiscovery) Discover(dir string) (*ComposeFile, error) {
	path, err := c.FindComposeFile(dir)
	if err != nil {
		return nil, err
	}

	if path == "" {
		return nil, nil
	}

	return c.Parse(path)
}

func (cf *ComposeFile) ServiceNames() []string {
	names := make([]string, 0, len(cf.Services))
	for name := range cf.Services {
		names = append(names, name)
	}
	return names
}

func (cf *ComposeFile) NetworkNames() []string {
	names := make([]string, 0, len(cf.Networks))
	for name := range cf.Networks {
		names = append(names, name)
	}
	return names
}

func (cf *ComposeFile) VolumeNames() []string {
	names := make([]string, 0, len(cf.Volumes))
	for name := range cf.Volumes {
		names = append(names, name)
	}
	return names
}

func (cf *ComposeFile) GetServiceNetworks(serviceName string) []string {
	service, ok := cf.Services[serviceName]
	if !ok {
		return nil
	}

	if len(service.Networks) > 0 {
		return service.Networks
	}

	return []string{"default"}
}
