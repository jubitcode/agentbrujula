package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotManagerNew(t *testing.T) {
	sm := NewSnapshotManager(nil)
	assert.NotNil(t, sm)
}

func TestSnapshotInfoNew(t *testing.T) {
	info := &SnapshotInfo{
		ID:          "snap123",
		Name:        "my-snapshot",
		ContainerID: "container456",
		ImageID:     "image789",
		CreatedAt:   time.Now(),
	}

	assert.Equal(t, "snap123", info.ID)
	assert.Equal(t, "my-snapshot", info.Name)
	assert.Equal(t, "container456", info.ContainerID)
}

func TestSnapshotInfoFullName(t *testing.T) {
	info := &SnapshotInfo{
		Name:    "my-snapshot",
		Project: "myproject",
	}

	assert.Equal(t, "myproject:my-snapshot", info.FullName())
}

func TestSnapshotInfoAge(t *testing.T) {
	info := &SnapshotInfo{
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}

	age := info.Age()
	assert.True(t, age >= 1*time.Hour)
	assert.True(t, age < 2*time.Hour)
}

func TestSnapshotOptionsNew(t *testing.T) {
	opts := NewSnapshotOptions()
	assert.NotNil(t, opts)
}

func TestSnapshotOptionsWithComment(t *testing.T) {
	opts := NewSnapshotOptions().WithComment("Test snapshot")
	assert.Equal(t, "Test snapshot", opts.Comment)
}

func TestSnapshotOptionsWithAuthor(t *testing.T) {
	opts := NewSnapshotOptions().WithAuthor("testuser")
	assert.Equal(t, "testuser", opts.Author)
}

func TestSnapshotOptionsWithPause(t *testing.T) {
	opts := NewSnapshotOptions().WithPause(true)
	assert.True(t, opts.Pause)
}

func TestSnapshotListEmpty(t *testing.T) {
	list := NewSnapshotList()
	assert.NotNil(t, list)
	assert.Empty(t, list.Snapshots)
}

func TestSnapshotListAdd(t *testing.T) {
	list := NewSnapshotList()
	info := &SnapshotInfo{Name: "test"}

	list.Add(info)
	assert.Len(t, list.Snapshots, 1)
}

func TestSnapshotListGetByName(t *testing.T) {
	list := NewSnapshotList()
	list.Add(&SnapshotInfo{Name: "snap1", ID: "id1"})
	list.Add(&SnapshotInfo{Name: "snap2", ID: "id2"})

	found := list.GetByName("snap1")
	assert.NotNil(t, found)
	assert.Equal(t, "id1", found.ID)
}

func TestSnapshotListGetByNameNotFound(t *testing.T) {
	list := NewSnapshotList()

	found := list.GetByName("nonexistent")
	assert.Nil(t, found)
}

func TestSnapshotListGetByID(t *testing.T) {
	list := NewSnapshotList()
	list.Add(&SnapshotInfo{Name: "snap1", ID: "id1"})

	found := list.GetByID("id1")
	assert.NotNil(t, found)
	assert.Equal(t, "snap1", found.Name)
}

func TestSnapshotListFilter(t *testing.T) {
	list := NewSnapshotList()
	list.Add(&SnapshotInfo{Name: "snap1", Project: "project1"})
	list.Add(&SnapshotInfo{Name: "snap2", Project: "project1"})
	list.Add(&SnapshotInfo{Name: "snap3", Project: "project2"})

	filtered := list.FilterByProject("project1")
	assert.Len(t, filtered.Snapshots, 2)
}

func TestSnapshotListSortByDate(t *testing.T) {
	list := NewSnapshotList()
	now := time.Now()

	list.Add(&SnapshotInfo{Name: "old", CreatedAt: now.Add(-2 * time.Hour)})
	list.Add(&SnapshotInfo{Name: "new", CreatedAt: now})
	list.Add(&SnapshotInfo{Name: "mid", CreatedAt: now.Add(-1 * time.Hour)})

	list.SortByDate()
	assert.Equal(t, "new", list.Snapshots[0].Name)
	assert.Equal(t, "old", list.Snapshots[2].Name)
}

func TestSnapshotNameValidate(t *testing.T) {
	tests := []struct {
		name    string
		valid   bool
	}{
		{"valid-name", true},
		{"valid_name", true},
		{"valid123", true},
		{"Valid-Name_123", true},
		{"", false},
		{"invalid name", false},
		{"invalid/name", false},
		{"invalid:name", false},
		{".invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSnapshotName(tt.name)
			if tt.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestSnapshotImageTag(t *testing.T) {
	info := &SnapshotInfo{
		Name:    "my-snapshot",
		Project: "myproject",
	}

	tag := info.ImageTag()
	assert.Equal(t, "drun-snapshot-myproject:my-snapshot", tag)
}

func TestSnapshotImageTagNoProject(t *testing.T) {
	info := &SnapshotInfo{
		Name: "my-snapshot",
	}

	tag := info.ImageTag()
	assert.Equal(t, "drun-snapshot:my-snapshot", tag)
}

func TestSnapshotStoreNew(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnapshotStore(tmpDir)
	assert.NotNil(t, store)
}

func TestSnapshotStoreSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnapshotStore(tmpDir)

	info := &SnapshotInfo{
		ID:        "snap123",
		Name:      "test-snapshot",
		Project:   "myproject",
		CreatedAt: time.Now(),
	}

	err := store.Save(info)
	require.NoError(t, err)

	loaded, err := store.Load("snap123")
	require.NoError(t, err)
	assert.Equal(t, info.Name, loaded.Name)
	assert.Equal(t, info.Project, loaded.Project)
}

func TestSnapshotStoreLoadNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnapshotStore(tmpDir)

	_, err := store.Load("nonexistent")
	assert.Error(t, err)
}

func TestSnapshotStoreList(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnapshotStore(tmpDir)

	store.Save(&SnapshotInfo{ID: "id1", Name: "snap1"})
	store.Save(&SnapshotInfo{ID: "id2", Name: "snap2"})

	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list.Snapshots, 2)
}

func TestSnapshotStoreDelete(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnapshotStore(tmpDir)

	info := &SnapshotInfo{ID: "snap123", Name: "test"}
	store.Save(info)

	err := store.Delete("snap123")
	assert.NoError(t, err)

	_, err = store.Load("snap123")
	assert.Error(t, err)
}

func TestExportOptionsNew(t *testing.T) {
	opts := NewExportOptions()
	assert.NotNil(t, opts)
}

func TestExportOptionsWithCompression(t *testing.T) {
	opts := NewExportOptions().WithCompression("gzip")
	assert.Equal(t, "gzip", opts.Compression)
}

func TestExportOptionsWithOutput(t *testing.T) {
	opts := NewExportOptions().WithOutput("/tmp/snapshot.tar.gz")
	assert.Equal(t, "/tmp/snapshot.tar.gz", opts.Output)
}

func TestImportOptionsNew(t *testing.T) {
	opts := NewImportOptions()
	assert.NotNil(t, opts)
}

func TestImportOptionsWithName(t *testing.T) {
	opts := NewImportOptions().WithName("imported-snapshot")
	assert.Equal(t, "imported-snapshot", opts.Name)
}

func TestImportOptionsWithProject(t *testing.T) {
	opts := NewImportOptions().WithProject("myproject")
	assert.Equal(t, "myproject", opts.Project)
}

func TestSnapshotExportFileName(t *testing.T) {
	info := &SnapshotInfo{
		Name:    "my-snapshot",
		Project: "myproject",
	}

	filename := info.ExportFileName()
	assert.Equal(t, "myproject-my-snapshot.tar", filename)
}

func TestSnapshotExportFileNameNoProject(t *testing.T) {
	info := &SnapshotInfo{
		Name: "my-snapshot",
	}

	filename := info.ExportFileName()
	assert.Equal(t, "my-snapshot.tar", filename)
}

func TestSnapshotMetadata(t *testing.T) {
	info := &SnapshotInfo{
		ID:          "snap123",
		Name:        "test",
		Project:     "myproject",
		ContainerID: "container456",
		Comment:     "Test comment",
		Author:      "testuser",
	}

	metadata := info.Metadata()
	assert.Equal(t, "snap123", metadata["drun.snapshot.id"])
	assert.Equal(t, "test", metadata["drun.snapshot.name"])
	assert.Equal(t, "myproject", metadata["drun.snapshot.project"])
}

func TestParseSnapshotMetadata(t *testing.T) {
	labels := map[string]string{
		"drun.snapshot.id":      "snap123",
		"drun.snapshot.name":    "test",
		"drun.snapshot.project": "myproject",
	}

	info := ParseSnapshotMetadata(labels)
	assert.Equal(t, "snap123", info.ID)
	assert.Equal(t, "test", info.Name)
	assert.Equal(t, "myproject", info.Project)
}

func TestSnapshotStoreDir(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewSnapshotStore(tmpDir)

	// Create a snapshot
	info := &SnapshotInfo{ID: "snap123", Name: "test"}
	store.Save(info)

	// Check file exists
	snapshotFile := filepath.Join(tmpDir, "snap123.json")
	_, err := os.Stat(snapshotFile)
	assert.NoError(t, err)
}
