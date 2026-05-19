package container

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/docker/docker/client"
)

type DepsManager struct {
	client *client.Client
	state  *DepsState
	mu     sync.Mutex
}

func NewDepsManager(dockerClient *client.Client) *DepsManager {
	return &DepsManager{
		client: dockerClient,
		state:  NewDepsState(),
	}
}

type DepsConfig struct {
	Command    string
	Triggers   []string
	WorkingDir string
}

func (c *DepsConfig) Copy() *DepsConfig {
	if c == nil {
		return nil
	}

	triggers := make([]string, len(c.Triggers))
	copy(triggers, c.Triggers)

	return &DepsConfig{
		Command:    c.Command,
		Triggers:   triggers,
		WorkingDir: c.WorkingDir,
	}
}

func ValidateDepsConfig(cfg *DepsConfig) error {
	if cfg == nil {
		return fmt.Errorf("deps config is nil")
	}

	if cfg.Command == "" {
		return fmt.Errorf("deps command is empty")
	}

	return nil
}

type DepsState struct {
	Hashes map[string]string `json:"hashes"`
	mu     sync.RWMutex
}

func NewDepsState() *DepsState {
	return &DepsState{
		Hashes: make(map[string]string),
	}
}

func (s *DepsState) GetHash(containerID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hash, ok := s.Hashes[containerID]
	return hash, ok
}

func (s *DepsState) SetHash(containerID, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Hashes[containerID] = hash
}

func (s *DepsState) Save(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal deps state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write deps state: %w", err)
	}

	return nil
}

func LoadDepsState(path string) (*DepsState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewDepsState(), nil
		}
		return nil, fmt.Errorf("failed to read deps state: %w", err)
	}

	var state DepsState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal deps state: %w", err)
	}

	if state.Hashes == nil {
		state.Hashes = make(map[string]string)
	}

	return &state, nil
}

type DepsSyncResult struct {
	Success  bool
	ExitCode int
	Output   string
	Error    string
	NewHash  string
}

func (m *DepsManager) ComputeHash(projectDir string, cfg *DepsConfig) (string, error) {
	h := sha256.New()

	triggers := make([]string, len(cfg.Triggers))
	copy(triggers, cfg.Triggers)
	sort.Strings(triggers)

	for _, pattern := range triggers {
		matches, err := expandGlob(projectDir, pattern)
		if err != nil {
			return "", fmt.Errorf("failed to expand pattern %s: %w", pattern, err)
		}

		sort.Strings(matches)

		for _, match := range matches {
			content, err := os.ReadFile(match)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return "", fmt.Errorf("failed to read %s: %w", match, err)
			}

			h.Write([]byte(match))
			h.Write(content)
		}
	}

	h.Write([]byte(cfg.Command))

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func expandGlob(dir, pattern string) ([]string, error) {
	if containsGlob(pattern) {
		fullPattern := filepath.Join(dir, pattern)
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			return nil, err
		}
		return matches, nil
	}

	fullPath := filepath.Join(dir, pattern)
	if _, err := os.Stat(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	return []string{fullPath}, nil
}

func containsGlob(pattern string) bool {
	for _, c := range pattern {
		switch c {
		case '*', '?', '[':
			return true
		}
	}
	return false
}

func (m *DepsManager) NeedsSync(ctx context.Context, containerID, projectDir string, cfg *DepsConfig) (bool, error) {
	currentHash, err := m.ComputeHash(projectDir, cfg)
	if err != nil {
		return false, err
	}

	previousHash, ok := m.state.GetHash(containerID)
	if !ok {
		return true, nil
	}

	return currentHash != previousHash, nil
}

func (m *DepsManager) Sync(ctx context.Context, containerID, projectDir string, cfg *DepsConfig) (*DepsSyncResult, error) {
	if m.client == nil {
		return nil, fmt.Errorf("no Docker client available")
	}

	if err := ValidateDepsConfig(cfg); err != nil {
		return nil, err
	}

	hash, err := m.ComputeHash(projectDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to compute hash: %w", err)
	}

	if previousHash, ok := m.state.GetHash(containerID); ok && previousHash == hash {
		return &DepsSyncResult{
			Success: true,
			Output:  "dependencies unchanged, skipping sync",
			NewHash: hash,
		}, nil
	}

	execHandler := NewExecHandler(m.client)
	cmd := []string{"/bin/sh", "-c", cfg.Command}

	opts := &ExecOptions{}
	if cfg.WorkingDir != "" {
		opts.WorkingDir = cfg.WorkingDir
	}

	result, err := execHandler.Exec(ctx, containerID, cmd, opts)
	if err != nil {
		return &DepsSyncResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	syncResult := &DepsSyncResult{
		Success:  result.ExitCode == 0,
		ExitCode: result.ExitCode,
		Output:   string(result.Stdout) + string(result.Stderr),
		NewHash:  hash,
	}

	if result.ExitCode != 0 {
		syncResult.Error = string(result.Stderr)
	} else {
		m.mu.Lock()
		m.state.SetHash(containerID, hash)
		m.mu.Unlock()
	}

	return syncResult, nil
}

func (m *DepsManager) UpdateHash(containerID, projectDir string, cfg *DepsConfig) error {
	hash, err := m.ComputeHash(projectDir, cfg)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.state.SetHash(containerID, hash)
	m.mu.Unlock()

	return nil
}

func (m *DepsManager) ClearHash(containerID string) {
	m.mu.Lock()
	delete(m.state.Hashes, containerID)
	m.mu.Unlock()
}

func (m *DepsManager) LoadState(path string) error {
	state, err := LoadDepsState(path)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.state = state
	m.mu.Unlock()

	return nil
}

func (m *DepsManager) SaveState(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.state.Save(path)
}
