package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RemoteManager struct {
	Store   *RemoteStore
	current *ConnectionState
}

func NewRemoteManager() *RemoteManager {
	return &RemoteManager{current: &ConnectionState{}}
}

type RemoteConfig struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	User       string `json:"user"`
	Port       int    `json:"port"`
	KeyPath    string `json:"key_path,omitempty"`
	DockerHost string `json:"docker_host,omitempty"`
}

func (c *RemoteConfig) Address() string {
	port := c.Port
	if port == 0 {
		port = 22
	}
	return fmt.Sprintf("%s@%s:%d", c.User, c.Host, port)
}

func (c *RemoteConfig) SSHTarget() string {
	return fmt.Sprintf("%s@%s", c.User, c.Host)
}

func (c *RemoteConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("remote name is required")
	}
	if c.Host == "" {
		return fmt.Errorf("remote host is required")
	}
	if c.User == "" {
		return fmt.Errorf("remote user is required")
	}
	return nil
}

type RemoteStore struct {
	dir string
}

func NewRemoteStore(dir string) *RemoteStore {
	return &RemoteStore{dir: dir}
}

func (s *RemoteStore) Add(config *RemoteConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("failed to create store directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	path := filepath.Join(s.dir, config.Name+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func (s *RemoteStore) Get(name string) (*RemoteConfig, error) {
	path := filepath.Join(s.dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("remote not found: %s", name)
	}

	var config RemoteConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

func (s *RemoteStore) List() ([]*RemoteConfig, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*RemoteConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read store: %w", err)
	}

	var configs []*RemoteConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".json")
		config, err := s.Get(name)
		if err != nil {
			continue
		}
		configs = append(configs, config)
	}

	return configs, nil
}

func (s *RemoteStore) Remove(name string) error {
	path := filepath.Join(s.dir, name+".json")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove remote: %w", err)
	}
	return nil
}

type SyncOptions struct {
	Exclude []string
	Delete  bool
	DryRun  bool
}

type SyncStatus struct {
	InSync        bool
	LocalChanges  int
	RemoteChanges int
	LastSync      string
}

type ConnectionState struct {
	Connected  bool
	RemoteName string
	Error      string
}

type Syncer struct {
	config *RemoteConfig
}

func NewSyncer(config *RemoteConfig) *Syncer {
	return &Syncer{config: config}
}

func (s *Syncer) Sync(localPath, remotePath string, opts *SyncOptions) error {
	if opts == nil {
		opts = &SyncOptions{}
	}

	args := []string{
		"-avz",
		"--progress",
		"-e", s.sshCommand(),
	}

	if opts.Delete {
		args = append(args, "--delete")
	}

	for _, pattern := range opts.Exclude {
		args = append(args, "--exclude", pattern)
	}

	if opts.DryRun {
		args = append(args, "--dry-run")
	}

	if !strings.HasSuffix(localPath, "/") {
		localPath += "/"
	}

	remoteDest := fmt.Sprintf("%s:%s", s.config.SSHTarget(), remotePath)
	args = append(args, localPath, remoteDest)

	return s.runRsync(args)
}

func (s *Syncer) sshCommand() string {
	port := s.config.Port
	if port == 0 {
		port = 22
	}

	cmd := fmt.Sprintf("ssh -p %d", port)
	if s.config.KeyPath != "" {
		cmd += fmt.Sprintf(" -i %s", s.config.KeyPath)
	}
	cmd += " -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

	return cmd
}

func (s *Syncer) runRsync(args []string) error {
	cmd := exec.Command("rsync", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *RemoteManager) TestConnection(config *RemoteConfig) error {
	args := []string{
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}

	if config.Port != 0 {
		args = append(args, "-p", fmt.Sprintf("%d", config.Port))
	}

	if config.KeyPath != "" {
		args = append(args, "-i", config.KeyPath)
	}

	args = append(args, config.SSHTarget(), "echo ok")

	cmd := exec.Command("ssh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("connection failed: %s", strings.TrimSpace(string(output)))
	}

	return nil
}

func (m *RemoteManager) GetDockerHost(config *RemoteConfig) string {
	if config.DockerHost != "" {
		return config.DockerHost
	}
	return fmt.Sprintf("ssh://%s", config.SSHTarget())
}

func (m *RemoteManager) RunRemoteCommand(config *RemoteConfig, command string) (string, error) {
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	}

	if config.Port != 0 {
		args = append(args, "-p", fmt.Sprintf("%d", config.Port))
	}

	if config.KeyPath != "" {
		args = append(args, "-i", config.KeyPath)
	}

	args = append(args, config.SSHTarget(), command)

	cmd := exec.Command("ssh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %s", strings.TrimSpace(string(output)))
	}

	return strings.TrimSpace(string(output)), nil
}
