package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeDiscoveryNew(t *testing.T) {
	cd := NewComposeDiscovery()
	assert.NotNil(t, cd)
}

func TestFindComposeFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create docker-compose.yml
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte("version: '3'\n"), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	found, err := cd.FindComposeFile(tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, composePath, found)
}

func TestFindComposeFileYamlExtension(t *testing.T) {
	tmpDir := t.TempDir()

	// Create docker-compose.yaml (yaml extension)
	composePath := filepath.Join(tmpDir, "docker-compose.yaml")
	err := os.WriteFile(composePath, []byte("version: '3'\n"), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	found, err := cd.FindComposeFile(tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, composePath, found)
}

func TestFindComposeFileComposeYml(t *testing.T) {
	tmpDir := t.TempDir()

	// Create compose.yml (new naming convention)
	composePath := filepath.Join(tmpDir, "compose.yml")
	err := os.WriteFile(composePath, []byte("version: '3'\n"), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	found, err := cd.FindComposeFile(tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, composePath, found)
}

func TestFindComposeFilePriority(t *testing.T) {
	tmpDir := t.TempDir()

	// Create both files - docker-compose.yml should take priority
	dcPath := filepath.Join(tmpDir, "docker-compose.yml")
	cPath := filepath.Join(tmpDir, "compose.yml")
	err := os.WriteFile(dcPath, []byte("version: '3'\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(cPath, []byte("version: '3'\n"), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	found, err := cd.FindComposeFile(tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, dcPath, found)
}

func TestFindComposeFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cd := NewComposeDiscovery()
	found, err := cd.FindComposeFile(tmpDir)
	assert.NoError(t, err)
	assert.Empty(t, found)
}

func TestParseComposeServices(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `
version: '3'
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
  db:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD: secret
  redis:
    image: redis:alpine
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	compose, err := cd.Parse(composePath)
	assert.NoError(t, err)
	assert.NotNil(t, compose)

	assert.Len(t, compose.Services, 3)
	assert.Contains(t, compose.Services, "web")
	assert.Contains(t, compose.Services, "db")
	assert.Contains(t, compose.Services, "redis")

	assert.Equal(t, "nginx:latest", compose.Services["web"].Image)
	assert.Equal(t, "postgres:15", compose.Services["db"].Image)
}

func TestParseComposeNetworks(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `
version: '3'
services:
  web:
    image: nginx
    networks:
      - frontend
      - backend
networks:
  frontend:
    driver: bridge
  backend:
    driver: bridge
    internal: true
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	compose, err := cd.Parse(composePath)
	assert.NoError(t, err)

	assert.Len(t, compose.Networks, 2)
	assert.Contains(t, compose.Networks, "frontend")
	assert.Contains(t, compose.Networks, "backend")
}

func TestParseComposeVolumes(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `
version: '3'
services:
  db:
    image: postgres
    volumes:
      - db-data:/var/lib/postgresql/data
volumes:
  db-data:
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	compose, err := cd.Parse(composePath)
	assert.NoError(t, err)

	assert.Len(t, compose.Volumes, 1)
	assert.Contains(t, compose.Volumes, "db-data")
}

func TestComposeProjectName(t *testing.T) {
	tests := []struct {
		dir      string
		expected string
	}{
		{"/home/user/myproject", "myproject"},
		{"/var/www/my-app", "my-app"},
		{"/projects/test_project", "test_project"},
	}

	cd := NewComposeDiscovery()
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			name := cd.ProjectName(tt.dir)
			assert.Equal(t, tt.expected, name)
		})
	}
}

func TestComposeProjectNameFromEnv(t *testing.T) {
	tmpDir := t.TempDir()

	// Set COMPOSE_PROJECT_NAME
	os.Setenv("COMPOSE_PROJECT_NAME", "custom-name")
	defer os.Unsetenv("COMPOSE_PROJECT_NAME")

	cd := NewComposeDiscovery()
	name := cd.ProjectName(tmpDir)
	assert.Equal(t, "custom-name", name)
}

func TestComposeServicePorts(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `
version: '3'
services:
  web:
    image: nginx
    ports:
      - "8080:80"
      - "443:443"
      - "3000"
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	compose, err := cd.Parse(composePath)
	assert.NoError(t, err)

	service := compose.Services["web"]
	assert.Len(t, service.Ports, 3)
}

func TestComposeServiceEnvironment(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `
version: '3'
services:
  app:
    image: myapp
    environment:
      - DEBUG=true
      - API_KEY=secret
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	compose, err := cd.Parse(composePath)
	assert.NoError(t, err)

	service := compose.Services["app"]
	assert.Contains(t, service.Environment, "DEBUG=true")
}

func TestComposeServiceDependsOn(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `
version: '3'
services:
  web:
    image: nginx
    depends_on:
      - db
      - redis
  db:
    image: postgres
  redis:
    image: redis
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	compose, err := cd.Parse(composePath)
	assert.NoError(t, err)

	service := compose.Services["web"]
	assert.Contains(t, service.DependsOn, "db")
	assert.Contains(t, service.DependsOn, "redis")
}

func TestComposeNetworkName(t *testing.T) {
	cd := NewComposeDiscovery()

	// Default network name follows project_network pattern
	name := cd.NetworkName("myproject", "default")
	assert.Equal(t, "myproject_default", name)

	name = cd.NetworkName("myproject", "backend")
	assert.Equal(t, "myproject_backend", name)
}

func TestComposeServiceContainerName(t *testing.T) {
	tmpDir := t.TempDir()

	composeContent := `
version: '3'
services:
  web:
    image: nginx
    container_name: my-custom-nginx
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	compose, err := cd.Parse(composePath)
	assert.NoError(t, err)

	service := compose.Services["web"]
	assert.Equal(t, "my-custom-nginx", service.ContainerName)
}

func TestComposeInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	err := os.WriteFile(composePath, []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	cd := NewComposeDiscovery()
	_, err = cd.Parse(composePath)
	assert.Error(t, err)
}

func TestComposeFileMissing(t *testing.T) {
	cd := NewComposeDiscovery()
	_, err := cd.Parse("/nonexistent/docker-compose.yml")
	assert.Error(t, err)
}
