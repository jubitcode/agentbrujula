package build

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CacheEntry struct {
	Key       string    `json:"key"`
	ImageID   string    `json:"image_id"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
}

func (e *CacheEntry) IsExpired(maxAge time.Duration) bool {
	return time.Since(e.LastUsed) > maxAge
}

type Cache struct {
	dir     string
	entries map[string]*CacheEntry
	mu      sync.RWMutex
}

func NewCache(dir string) (*Cache, error) {
	c := &Cache{
		dir:     dir,
		entries: make(map[string]*CacheEntry),
	}

	if err := c.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return c, nil
}

func (c *Cache) indexPath() string {
	return filepath.Join(c.dir, "cache", "index.json")
}

func (c *Cache) load() error {
	data, err := os.ReadFile(c.indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var entries []*CacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range entries {
		c.entries[entry.Key] = entry
	}

	return nil
}

func (c *Cache) saveLocked() error {
	entries := make([]*CacheEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(c.indexPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(c.indexPath(), data, 0644)
}

func (c *Cache) Get(ctx context.Context, key string) (*CacheEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, nil
	}

	entry.LastUsed = time.Now()

	if err := c.saveLocked(); err != nil {
		return entry, err
	}

	return entry, nil
}

func (c *Cache) Put(ctx context.Context, entry *CacheEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[entry.Key] = entry
	return c.saveLocked()
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
	return c.saveLocked()
}

func (c *Cache) List(ctx context.Context) ([]*CacheEntry, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := make([]*CacheEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}

	return entries, nil
}

func (c *Cache) Cleanup(ctx context.Context, maxAge time.Duration) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toRemove []string
	for key, entry := range c.entries {
		if entry.IsExpired(maxAge) {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		delete(c.entries, key)
	}

	if len(toRemove) > 0 {
		if err := c.saveLocked(); err != nil {
			return 0, err
		}
	}

	return len(toRemove), nil
}

func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func (c *Cache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	return c.saveLocked()
}
