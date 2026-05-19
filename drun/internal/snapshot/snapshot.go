package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

type SnapshotManager struct {
	client *client.Client
	store  *SnapshotStore
}

func NewSnapshotManager(dockerClient *client.Client) *SnapshotManager {
	return &SnapshotManager{
		client: dockerClient,
	}
}

func (m *SnapshotManager) WithStore(store *SnapshotStore) *SnapshotManager {
	m.store = store
	return m
}

type SnapshotInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Project     string    `json:"project"`
	ContainerID string    `json:"container_id"`
	ImageID     string    `json:"image_id"`
	Comment     string    `json:"comment"`
	Author      string    `json:"author"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *SnapshotInfo) FullName() string {
	if s.Project != "" {
		return fmt.Sprintf("%s:%s", s.Project, s.Name)
	}
	return s.Name
}

func (s *SnapshotInfo) Age() time.Duration {
	return time.Since(s.CreatedAt)
}

func (s *SnapshotInfo) ImageTag() string {
	if s.Project != "" {
		return fmt.Sprintf("drun-snapshot-%s:%s", s.Project, s.Name)
	}
	return fmt.Sprintf("drun-snapshot:%s", s.Name)
}

func (s *SnapshotInfo) ExportFileName() string {
	if s.Project != "" {
		return fmt.Sprintf("%s-%s.tar", s.Project, s.Name)
	}
	return fmt.Sprintf("%s.tar", s.Name)
}

func (s *SnapshotInfo) Metadata() map[string]string {
	return map[string]string{
		"drun.snapshot.id":           s.ID,
		"drun.snapshot.name":         s.Name,
		"drun.snapshot.project":      s.Project,
		"drun.snapshot.container_id": s.ContainerID,
		"drun.snapshot.comment":      s.Comment,
		"drun.snapshot.author":       s.Author,
		"drun.snapshot.created_at":   s.CreatedAt.Format(time.RFC3339),
	}
}

func ParseSnapshotMetadata(labels map[string]string) *SnapshotInfo {
	info := &SnapshotInfo{
		ID:          labels["drun.snapshot.id"],
		Name:        labels["drun.snapshot.name"],
		Project:     labels["drun.snapshot.project"],
		ContainerID: labels["drun.snapshot.container_id"],
		Comment:     labels["drun.snapshot.comment"],
		Author:      labels["drun.snapshot.author"],
	}

	if t, err := time.Parse(time.RFC3339, labels["drun.snapshot.created_at"]); err == nil {
		info.CreatedAt = t
	}

	return info
}

type SnapshotOptions struct {
	Comment string
	Author  string
	Pause   bool
}

func NewSnapshotOptions() *SnapshotOptions {
	return &SnapshotOptions{
		Pause: true,
	}
}

func (o *SnapshotOptions) WithComment(comment string) *SnapshotOptions {
	o.Comment = comment
	return o
}

func (o *SnapshotOptions) WithAuthor(author string) *SnapshotOptions {
	o.Author = author
	return o
}

func (o *SnapshotOptions) WithPause(pause bool) *SnapshotOptions {
	o.Pause = pause
	return o
}

type SnapshotList struct {
	Snapshots []*SnapshotInfo
}

func NewSnapshotList() *SnapshotList {
	return &SnapshotList{
		Snapshots: make([]*SnapshotInfo, 0),
	}
}

func (l *SnapshotList) Add(info *SnapshotInfo) {
	l.Snapshots = append(l.Snapshots, info)
}

func (l *SnapshotList) GetByName(name string) *SnapshotInfo {
	for _, s := range l.Snapshots {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (l *SnapshotList) GetByID(id string) *SnapshotInfo {
	for _, s := range l.Snapshots {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (l *SnapshotList) FilterByProject(project string) *SnapshotList {
	result := NewSnapshotList()
	for _, s := range l.Snapshots {
		if s.Project == project {
			result.Add(s)
		}
	}
	return result
}

func (l *SnapshotList) SortByDate() {
	sort.Slice(l.Snapshots, func(i, j int) bool {
		return l.Snapshots[i].CreatedAt.After(l.Snapshots[j].CreatedAt)
	})
}

var snapshotNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

func ValidateSnapshotName(name string) error {
	if name == "" {
		return fmt.Errorf("snapshot name cannot be empty")
	}

	if !snapshotNamePattern.MatchString(name) {
		return fmt.Errorf("invalid snapshot name: must start with a letter and contain only letters, numbers, underscores, and hyphens")
	}

	return nil
}

type SnapshotStore struct {
	dir string
}

func NewSnapshotStore(dir string) *SnapshotStore {
	return &SnapshotStore{dir: dir}
}

func (s *SnapshotStore) Save(info *SnapshotInfo) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("failed to create store directory: %w", err)
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot info: %w", err)
	}

	path := filepath.Join(s.dir, info.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot info: %w", err)
	}

	return nil
}

func (s *SnapshotStore) Load(id string) (*SnapshotInfo, error) {
	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot info: %w", err)
	}

	var info SnapshotInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot info: %w", err)
	}

	return &info, nil
}

func (s *SnapshotStore) List() (*SnapshotList, error) {
	list := NewSnapshotList()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return list, nil
		}
		return nil, fmt.Errorf("failed to read store directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".json")
		info, err := s.Load(id)
		if err != nil {
			continue
		}

		list.Add(info)
	}

	return list, nil
}

func (s *SnapshotStore) Delete(id string) error {
	path := filepath.Join(s.dir, id+".json")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete snapshot info: %w", err)
	}
	return nil
}

type ExportOptions struct {
	Compression string
	Output      string
}

func NewExportOptions() *ExportOptions {
	return &ExportOptions{
		Compression: "none",
	}
}

func (o *ExportOptions) WithCompression(compression string) *ExportOptions {
	o.Compression = compression
	return o
}

func (o *ExportOptions) WithOutput(output string) *ExportOptions {
	o.Output = output
	return o
}

type ImportOptions struct {
	Name    string
	Project string
}

func NewImportOptions() *ImportOptions {
	return &ImportOptions{}
}

func (o *ImportOptions) WithName(name string) *ImportOptions {
	o.Name = name
	return o
}

func (o *ImportOptions) WithProject(project string) *ImportOptions {
	o.Project = project
	return o
}

func (m *SnapshotManager) Create(ctx context.Context, containerID, name, project string, opts *SnapshotOptions) (*SnapshotInfo, error) {
	if m.client == nil {
		return nil, fmt.Errorf("no Docker client available")
	}

	if err := ValidateSnapshotName(name); err != nil {
		return nil, err
	}

	if opts == nil {
		opts = NewSnapshotOptions()
	}

	id := generateSnapshotID()

	info := &SnapshotInfo{
		ID:          id,
		Name:        name,
		Project:     project,
		ContainerID: containerID,
		Comment:     opts.Comment,
		Author:      opts.Author,
		CreatedAt:   time.Now(),
	}

	commitResp, err := m.client.ContainerCommit(ctx, containerID, container.CommitOptions{
		Reference: info.ImageTag(),
		Comment:   opts.Comment,
		Author:    opts.Author,
		Pause:     opts.Pause,
		Config:    nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to commit container: %w", err)
	}

	info.ImageID = commitResp.ID

	if m.store != nil {
		if err := m.store.Save(info); err != nil {
			return nil, fmt.Errorf("failed to save snapshot info: %w", err)
		}
	}

	return info, nil
}

func (m *SnapshotManager) List(ctx context.Context) (*SnapshotList, error) {
	if m.client == nil {
		return nil, fmt.Errorf("no Docker client available")
	}

	list := NewSnapshotList()

	images, err := m.client.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if strings.HasPrefix(tag, "drun-snapshot") {
				inspect, _, err := m.client.ImageInspectWithRaw(ctx, img.ID)
				if err != nil {
					continue
				}

				info := ParseSnapshotMetadata(inspect.Config.Labels)
				if info.ID == "" {
					parts := strings.SplitN(tag, ":", 2)
					if len(parts) == 2 {
						info.Name = parts[1]
					}
					info.ImageID = img.ID
					info.CreatedAt = time.Unix(img.Created, 0)
				}

				info.Size = img.Size
				list.Add(info)
				break
			}
		}
	}

	if m.store != nil {
		storeList, err := m.store.List()
		if err == nil {
			for _, s := range storeList.Snapshots {
				if list.GetByID(s.ID) == nil {
					list.Add(s)
				}
			}
		}
	}

	list.SortByDate()
	return list, nil
}

func (m *SnapshotManager) Restore(ctx context.Context, snapshotID string) (string, error) {
	if m.client == nil {
		return "", fmt.Errorf("no Docker client available")
	}

	var info *SnapshotInfo
	if m.store != nil {
		var err error
		info, err = m.store.Load(snapshotID)
		if err != nil {
			return "", fmt.Errorf("snapshot not found: %w", err)
		}
	} else {
		return "", fmt.Errorf("no snapshot store configured")
	}

	resp, err := m.client.ContainerCreate(ctx, &container.Config{
		Image: info.ImageTag(),
	}, nil, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create container from snapshot: %w", err)
	}

	return resp.ID, nil
}

func (m *SnapshotManager) Delete(ctx context.Context, snapshotID string) error {
	if m.client == nil {
		return fmt.Errorf("no Docker client available")
	}

	var info *SnapshotInfo
	if m.store != nil {
		var err error
		info, err = m.store.Load(snapshotID)
		if err != nil {
			return fmt.Errorf("snapshot not found: %w", err)
		}
	} else {
		return fmt.Errorf("no snapshot store configured")
	}

	_, err := m.client.ImageRemove(ctx, info.ImageID, image.RemoveOptions{Force: false})
	if err != nil {
		return fmt.Errorf("failed to remove snapshot image: %w", err)
	}

	if m.store != nil {
		if err := m.store.Delete(snapshotID); err != nil {
			return fmt.Errorf("failed to delete snapshot info: %w", err)
		}
	}

	return nil
}

func generateSnapshotID() string {
	return fmt.Sprintf("snap_%d", time.Now().UnixNano())
}
