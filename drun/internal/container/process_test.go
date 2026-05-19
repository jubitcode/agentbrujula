package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessManagerNew(t *testing.T) {
	pm := NewProcessManager(nil)
	assert.NotNil(t, pm)
}

func TestProcessInfoString(t *testing.T) {
	info := &ProcessInfo{
		PID:     1234,
		PPID:    1,
		Command: "/bin/bash",
		Args:    []string{"-c", "sleep 100"},
		State:   "running",
	}

	str := info.String()
	assert.Contains(t, str, "1234")
	assert.Contains(t, str, "/bin/bash")
}

func TestProcessInfoIsZombie(t *testing.T) {
	zombie := &ProcessInfo{State: "zombie"}
	running := &ProcessInfo{State: "running"}

	assert.True(t, zombie.IsZombie())
	assert.False(t, running.IsZombie())
}

func TestProcessInfoIsOrphan(t *testing.T) {
	orphan := &ProcessInfo{PPID: 1}
	child := &ProcessInfo{PPID: 1234}

	assert.True(t, orphan.IsOrphan())
	assert.False(t, child.IsOrphan())
}

func TestProcessSnapshotNew(t *testing.T) {
	snapshot := NewProcessSnapshot()
	assert.NotNil(t, snapshot)
	assert.Empty(t, snapshot.Processes)
}

func TestProcessSnapshotAdd(t *testing.T) {
	snapshot := NewProcessSnapshot()

	proc := &ProcessInfo{
		PID:     1234,
		Command: "/bin/bash",
	}

	snapshot.Add(proc)
	assert.Len(t, snapshot.Processes, 1)
}

func TestProcessSnapshotGetByPID(t *testing.T) {
	snapshot := NewProcessSnapshot()

	proc := &ProcessInfo{
		PID:     1234,
		Command: "/bin/bash",
	}

	snapshot.Add(proc)

	got := snapshot.GetByPID(1234)
	assert.NotNil(t, got)
	assert.Equal(t, "/bin/bash", got.Command)
}

func TestProcessSnapshotGetByPIDNotFound(t *testing.T) {
	snapshot := NewProcessSnapshot()

	got := snapshot.GetByPID(9999)
	assert.Nil(t, got)
}

func TestProcessSnapshotDiff(t *testing.T) {
	old := NewProcessSnapshot()
	old.Add(&ProcessInfo{PID: 1, Command: "init"})
	old.Add(&ProcessInfo{PID: 100, Command: "bash"})

	new := NewProcessSnapshot()
	new.Add(&ProcessInfo{PID: 1, Command: "init"})
	new.Add(&ProcessInfo{PID: 100, Command: "bash"})
	new.Add(&ProcessInfo{PID: 200, Command: "go"})
	new.Add(&ProcessInfo{PID: 300, Command: "make"})

	diff := new.Diff(old)

	assert.Len(t, diff.Added, 2)
	assert.Empty(t, diff.Removed)
}

func TestProcessSnapshotDiffRemoved(t *testing.T) {
	old := NewProcessSnapshot()
	old.Add(&ProcessInfo{PID: 1, Command: "init"})
	old.Add(&ProcessInfo{PID: 100, Command: "bash"})
	old.Add(&ProcessInfo{PID: 200, Command: "go"})

	new := NewProcessSnapshot()
	new.Add(&ProcessInfo{PID: 1, Command: "init"})
	new.Add(&ProcessInfo{PID: 100, Command: "bash"})

	diff := new.Diff(old)

	assert.Empty(t, diff.Added)
	assert.Len(t, diff.Removed, 1)
	assert.Equal(t, 200, diff.Removed[0].PID)
}

func TestProcessSnapshotZombies(t *testing.T) {
	snapshot := NewProcessSnapshot()
	snapshot.Add(&ProcessInfo{PID: 1, State: "running"})
	snapshot.Add(&ProcessInfo{PID: 100, State: "zombie"})
	snapshot.Add(&ProcessInfo{PID: 200, State: "zombie"})
	snapshot.Add(&ProcessInfo{PID: 300, State: "sleeping"})

	zombies := snapshot.Zombies()
	assert.Len(t, zombies, 2)
}

func TestProcessSnapshotOrphans(t *testing.T) {
	snapshot := NewProcessSnapshot()
	snapshot.Add(&ProcessInfo{PID: 1, PPID: 0, State: "running"})
	snapshot.Add(&ProcessInfo{PID: 100, PPID: 1, State: "running"})
	snapshot.Add(&ProcessInfo{PID: 200, PPID: 1, State: "running"})
	snapshot.Add(&ProcessInfo{PID: 300, PPID: 999, State: "running"})

	orphans := snapshot.Orphans()
	// Orphans are processes whose parent is PID 1 (init adopted them)
	// PID 100 and 200 have PPID=1, so they are orphans
	// PID 300 has PPID=999, so it's not an orphan (its parent may still exist)
	assert.Len(t, orphans, 2)
}
