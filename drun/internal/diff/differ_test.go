package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDifferNew(t *testing.T) {
	d := NewDiffer(nil)
	assert.NotNil(t, d)
}

func TestDiffResultNew(t *testing.T) {
	result := NewDiffResult()
	assert.NotNil(t, result)
	assert.False(t, result.HasChanges())
}

func TestDiffResultHasChanges(t *testing.T) {
	result := NewDiffResult()
	result.AddPackageChange(&PackageChange{Name: "curl", Type: ChangeTypeAdded})

	assert.True(t, result.HasChanges())
}

func TestChangeTypeValues(t *testing.T) {
	assert.Equal(t, ChangeType("added"), ChangeTypeAdded)
	assert.Equal(t, ChangeType("removed"), ChangeTypeRemoved)
	assert.Equal(t, ChangeType("modified"), ChangeTypeModified)
}

func TestPackageChangeNew(t *testing.T) {
	change := &PackageChange{
		Name:       "curl",
		Type:       ChangeTypeAdded,
		NewVersion: "7.68.0",
	}

	assert.Equal(t, "curl", change.Name)
	assert.Equal(t, ChangeTypeAdded, change.Type)
	assert.Equal(t, "7.68.0", change.NewVersion)
}

func TestPackageChangeString(t *testing.T) {
	tests := []struct {
		change   *PackageChange
		expected string
	}{
		{
			change:   &PackageChange{Name: "curl", Type: ChangeTypeAdded, NewVersion: "7.68.0"},
			expected: "+ curl (7.68.0)",
		},
		{
			change:   &PackageChange{Name: "vim", Type: ChangeTypeRemoved, OldVersion: "8.0"},
			expected: "- vim (8.0)",
		},
		{
			change:   &PackageChange{Name: "git", Type: ChangeTypeModified, OldVersion: "2.25", NewVersion: "2.30"},
			expected: "~ git (2.25 -> 2.30)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.change.Name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.change.String())
		})
	}
}

func TestEnvChangeNew(t *testing.T) {
	change := &EnvChange{
		Name:     "PATH",
		Type:     ChangeTypeModified,
		OldValue: "/usr/bin",
		NewValue: "/usr/bin:/usr/local/bin",
	}

	assert.Equal(t, "PATH", change.Name)
	assert.Equal(t, ChangeTypeModified, change.Type)
}

func TestEnvChangeString(t *testing.T) {
	tests := []struct {
		change   *EnvChange
		expected string
	}{
		{
			change:   &EnvChange{Name: "NEW_VAR", Type: ChangeTypeAdded, NewValue: "value"},
			expected: "+ NEW_VAR=value",
		},
		{
			change:   &EnvChange{Name: "OLD_VAR", Type: ChangeTypeRemoved, OldValue: "old"},
			expected: "- OLD_VAR=old",
		},
		{
			change:   &EnvChange{Name: "MOD_VAR", Type: ChangeTypeModified, OldValue: "old", NewValue: "new"},
			expected: "~ MOD_VAR: old -> new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.change.Name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.change.String())
		})
	}
}

func TestFileChangeNew(t *testing.T) {
	change := &FileChange{
		Path: "/etc/hosts",
		Type: ChangeTypeModified,
	}

	assert.Equal(t, "/etc/hosts", change.Path)
	assert.Equal(t, ChangeTypeModified, change.Type)
}

func TestFileChangeString(t *testing.T) {
	tests := []struct {
		change   *FileChange
		expected string
	}{
		{
			change:   &FileChange{Path: "/etc/newfile", Type: ChangeTypeAdded},
			expected: "+ /etc/newfile",
		},
		{
			change:   &FileChange{Path: "/etc/oldfile", Type: ChangeTypeRemoved},
			expected: "- /etc/oldfile",
		},
		{
			change:   &FileChange{Path: "/etc/hosts", Type: ChangeTypeModified},
			expected: "~ /etc/hosts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.change.Path, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.change.String())
		})
	}
}

func TestToolChangeNew(t *testing.T) {
	change := &ToolChange{
		Name:       "go",
		Type:       ChangeTypeModified,
		OldVersion: "1.20",
		NewVersion: "1.21",
	}

	assert.Equal(t, "go", change.Name)
	assert.Equal(t, "1.20", change.OldVersion)
	assert.Equal(t, "1.21", change.NewVersion)
}

func TestDiffResultAddChanges(t *testing.T) {
	result := NewDiffResult()

	result.AddPackageChange(&PackageChange{Name: "curl", Type: ChangeTypeAdded})
	result.AddEnvChange(&EnvChange{Name: "PATH", Type: ChangeTypeModified})
	result.AddFileChange(&FileChange{Path: "/etc/hosts", Type: ChangeTypeModified})
	result.AddToolChange(&ToolChange{Name: "go", Type: ChangeTypeAdded})

	assert.Len(t, result.PackageChanges, 1)
	assert.Len(t, result.EnvChanges, 1)
	assert.Len(t, result.FileChanges, 1)
	assert.Len(t, result.ToolChanges, 1)
}

func TestDiffResultSummary(t *testing.T) {
	result := NewDiffResult()
	result.AddPackageChange(&PackageChange{Name: "curl", Type: ChangeTypeAdded})
	result.AddPackageChange(&PackageChange{Name: "vim", Type: ChangeTypeRemoved})
	result.AddEnvChange(&EnvChange{Name: "PATH", Type: ChangeTypeModified})

	summary := result.Summary()
	assert.Contains(t, summary, "package")
	assert.Contains(t, summary, "environment")
}

func TestDiffResultSummaryNoChanges(t *testing.T) {
	result := NewDiffResult()

	summary := result.Summary()
	assert.Contains(t, summary, "No changes")
}

func TestDiffPackages(t *testing.T) {
	baseline := []PackageInfo{
		{Name: "curl", Version: "7.68.0"},
		{Name: "vim", Version: "8.0"},
		{Name: "git", Version: "2.25"},
	}

	current := []PackageInfo{
		{Name: "curl", Version: "7.68.0"},
		{Name: "git", Version: "2.30"},
		{Name: "wget", Version: "1.20"},
	}

	changes := DiffPackages(baseline, current)

	assert.Len(t, changes, 3)

	// Find specific changes
	var removed, modified, added *PackageChange
	for _, c := range changes {
		switch c.Name {
		case "vim":
			removed = c
		case "git":
			modified = c
		case "wget":
			added = c
		}
	}

	assert.NotNil(t, removed)
	assert.Equal(t, ChangeTypeRemoved, removed.Type)

	assert.NotNil(t, modified)
	assert.Equal(t, ChangeTypeModified, modified.Type)
	assert.Equal(t, "2.25", modified.OldVersion)
	assert.Equal(t, "2.30", modified.NewVersion)

	assert.NotNil(t, added)
	assert.Equal(t, ChangeTypeAdded, added.Type)
}

func TestDiffEnvVars(t *testing.T) {
	baseline := map[string]string{
		"PATH":    "/usr/bin",
		"HOME":    "/root",
		"OLD_VAR": "old_value",
	}

	current := map[string]string{
		"PATH":    "/usr/bin:/usr/local/bin",
		"HOME":    "/root",
		"NEW_VAR": "new_value",
	}

	changes := DiffEnvVars(baseline, current)

	assert.Len(t, changes, 3)

	var removed, modified, added *EnvChange
	for _, c := range changes {
		switch c.Name {
		case "OLD_VAR":
			removed = c
		case "PATH":
			modified = c
		case "NEW_VAR":
			added = c
		}
	}

	assert.NotNil(t, removed)
	assert.Equal(t, ChangeTypeRemoved, removed.Type)

	assert.NotNil(t, modified)
	assert.Equal(t, ChangeTypeModified, modified.Type)

	assert.NotNil(t, added)
	assert.Equal(t, ChangeTypeAdded, added.Type)
}

func TestDiffFiles(t *testing.T) {
	baseline := []FileInfo{
		{Path: "/etc/hosts", Hash: "abc123"},
		{Path: "/etc/passwd", Hash: "def456"},
		{Path: "/etc/oldfile", Hash: "ghi789"},
	}

	current := []FileInfo{
		{Path: "/etc/hosts", Hash: "abc123"},
		{Path: "/etc/passwd", Hash: "xyz999"},
		{Path: "/etc/newfile", Hash: "jkl012"},
	}

	changes := DiffFiles(baseline, current)

	assert.Len(t, changes, 3)

	var removed, modified, added *FileChange
	for _, c := range changes {
		switch c.Path {
		case "/etc/oldfile":
			removed = c
		case "/etc/passwd":
			modified = c
		case "/etc/newfile":
			added = c
		}
	}

	assert.NotNil(t, removed)
	assert.Equal(t, ChangeTypeRemoved, removed.Type)

	assert.NotNil(t, modified)
	assert.Equal(t, ChangeTypeModified, modified.Type)

	assert.NotNil(t, added)
	assert.Equal(t, ChangeTypeAdded, added.Type)
}

func TestDiffTools(t *testing.T) {
	baseline := []ToolInfo{
		{Name: "go", Version: "1.20"},
		{Name: "node", Version: "18.0"},
		{Name: "python", Version: "3.10"},
	}

	current := []ToolInfo{
		{Name: "go", Version: "1.21"},
		{Name: "node", Version: "18.0"},
		{Name: "rust", Version: "1.70"},
	}

	changes := DiffTools(baseline, current)

	assert.Len(t, changes, 3)

	var removed, modified, added *ToolChange
	for _, c := range changes {
		switch c.Name {
		case "python":
			removed = c
		case "go":
			modified = c
		case "rust":
			added = c
		}
	}

	assert.NotNil(t, removed)
	assert.Equal(t, ChangeTypeRemoved, removed.Type)

	assert.NotNil(t, modified)
	assert.Equal(t, ChangeTypeModified, modified.Type)

	assert.NotNil(t, added)
	assert.Equal(t, ChangeTypeAdded, added.Type)
}

func TestDiffDisplay(t *testing.T) {
	result := NewDiffResult()
	result.AddPackageChange(&PackageChange{Name: "curl", Type: ChangeTypeAdded, NewVersion: "7.68.0"})
	result.AddEnvChange(&EnvChange{Name: "PATH", Type: ChangeTypeModified, OldValue: "/usr/bin", NewValue: "/usr/bin:/usr/local/bin"})

	display := NewDiffDisplay()
	output := display.Format(result)

	assert.Contains(t, output, "Packages")
	assert.Contains(t, output, "curl")
	assert.Contains(t, output, "Environment")
	assert.Contains(t, output, "PATH")
}

func TestDiffDisplayNoChanges(t *testing.T) {
	result := NewDiffResult()

	display := NewDiffDisplay()
	output := display.Format(result)

	assert.Contains(t, output, "No changes")
}

func TestBaselineNew(t *testing.T) {
	baseline := NewBaseline()
	assert.NotNil(t, baseline)
}

func TestBaselineSetGet(t *testing.T) {
	baseline := NewBaseline()

	baseline.Packages = []PackageInfo{{Name: "curl", Version: "7.68.0"}}
	baseline.EnvVars = map[string]string{"PATH": "/usr/bin"}
	baseline.Files = []FileInfo{{Path: "/etc/hosts", Hash: "abc123"}}
	baseline.Tools = []ToolInfo{{Name: "go", Version: "1.20"}}

	assert.Len(t, baseline.Packages, 1)
	assert.Equal(t, "/usr/bin", baseline.EnvVars["PATH"])
	assert.Len(t, baseline.Files, 1)
	assert.Len(t, baseline.Tools, 1)
}

func TestPackageInfoString(t *testing.T) {
	info := PackageInfo{Name: "curl", Version: "7.68.0"}
	assert.Equal(t, "curl (7.68.0)", info.String())
}

func TestToolInfoString(t *testing.T) {
	info := ToolInfo{Name: "go", Version: "1.21", Path: "/usr/local/go/bin/go"}
	assert.Equal(t, "go (1.21)", info.String())
}

func TestFileInfoString(t *testing.T) {
	info := FileInfo{Path: "/etc/hosts", Hash: "abc123"}
	assert.Equal(t, "/etc/hosts [abc123]", info.String())
}
