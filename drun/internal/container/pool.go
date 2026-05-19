package container

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/client"

	"drun/internal/config"
)

type ContainerStatus int

const (
	ContainerStatusCreated ContainerStatus = iota
	ContainerStatusRunning
	ContainerStatusStopped
	ContainerStatusPaused
	ContainerStatusDead
)

func (s ContainerStatus) String() string {
	switch s {
	case ContainerStatusCreated:
		return "created"
	case ContainerStatusRunning:
		return "running"
	case ContainerStatusStopped:
		return "stopped"
	case ContainerStatusPaused:
		return "paused"
	case ContainerStatusDead:
		return "dead"
	default:
		return "unknown"
	}
}

type PoolEntry struct {
	ContainerID string          `json:"container_id"`
	Key         string          `json:"key"`
	ImageID     string          `json:"image_id"`
	Status      ContainerStatus `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	LastUsed    time.Time       `json:"last_used"`
	ProjectDir  string          `json:"project_dir"`
	Branch      string          `json:"branch"`
}

func (e *PoolEntry) IsRunning() bool {
	return e.Status == ContainerStatusRunning
}

type Pool struct {
	client   *client.Client
	entries  map[string]*PoolEntry
	mu       sync.RWMutex
	stateDir string
}

func NewPool(dockerClient *client.Client) *Pool {
	return &Pool{
		client:  dockerClient,
		entries: make(map[string]*PoolEntry),
	}
}

func (p *Pool) SetStateDir(dir string) {
	p.stateDir = dir
}

func (p *Pool) statePath() string {
	if p.stateDir == "" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".drun", "pool.json")
	}
	return filepath.Join(p.stateDir, "pool.json")
}

func (p *Pool) GenerateKey(cfg *config.Config, branch string) string {
	h := sha256.New()
	h.Write([]byte(cfg.ProjectDir))
	h.Write([]byte(branch))
	return fmt.Sprintf("drun-%x", h.Sum(nil))[:20]
}

func (p *Pool) GenerateLabels(cfg *config.Config, branch string) map[string]string {
	h := sha256.New()
	h.Write([]byte(cfg.ProjectDir))
	projectHash := fmt.Sprintf("%x", h.Sum(nil))[:12]

	return map[string]string{
		"drun.managed": "true",
		"drun.project": projectHash,
		"drun.branch":  branch,
	}
}

func (p *Pool) Add(entry *PoolEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[entry.Key] = entry
}

func (p *Pool) Get(key string) *PoolEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.entries[key]
}

func (p *Pool) GetByContainerID(containerID string) *PoolEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, entry := range p.entries {
		if entry.ContainerID == containerID {
			return entry
		}
	}
	return nil
}

func (p *Pool) Remove(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.entries, key)
}

func (p *Pool) UpdateStatus(key string, status ContainerStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, exists := p.entries[key]; exists {
		entry.Status = status
	}
}

func (p *Pool) List() []*PoolEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()

	entries := make([]*PoolEntry, 0, len(p.entries))
	for _, entry := range p.entries {
		entries = append(entries, entry)
	}
	return entries
}

func (p *Pool) ListRunning() []*PoolEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()

	entries := make([]*PoolEntry, 0)
	for _, entry := range p.entries {
		if entry.IsRunning() {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries)
}

func (p *Pool) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = make(map[string]*PoolEntry)
}

func (p *Pool) Save(ctx context.Context) error {
	p.mu.RLock()
	entries := make([]*PoolEntry, 0, len(p.entries))
	for _, entry := range p.entries {
		entries = append(entries, entry)
	}
	p.mu.RUnlock()

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(p.statePath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(p.statePath(), data, 0644)
}

func (p *Pool) Load(ctx context.Context) error {
	data, err := os.ReadFile(p.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var entries []*PoolEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range entries {
		p.entries[entry.Key] = entry
	}

	return nil
}

func (p *Pool) GetOrCreate(ctx context.Context, cfg *config.Config, branch string) (*PoolEntry, error) {
	key := p.GenerateKey(cfg, branch)

	if entry := p.Get(key); entry != nil {
		entry.LastUsed = time.Now()
		return entry, nil
	}

	entry := &PoolEntry{
		Key:        key,
		Status:     ContainerStatusCreated,
		CreatedAt:  time.Now(),
		LastUsed:   time.Now(),
		ProjectDir: cfg.ProjectDir,
		Branch:     branch,
	}

	p.Add(entry)
	return entry, nil
}
