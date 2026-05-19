package container

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/client"
)

type ProcessManager struct {
	client *client.Client
}

func NewProcessManager(dockerClient *client.Client) *ProcessManager {
	return &ProcessManager{
		client: dockerClient,
	}
}

type ProcessInfo struct {
	PID     int
	PPID    int
	Command string
	Args    []string
	State   string
	User    string
	CPU     float64
	Memory  float64
}

func (p *ProcessInfo) String() string {
	return fmt.Sprintf("%d: %s %s", p.PID, p.Command, strings.Join(p.Args, " "))
}

func (p *ProcessInfo) IsZombie() bool {
	return strings.ToLower(p.State) == "zombie"
}

func (p *ProcessInfo) IsOrphan() bool {
	return p.PPID == 1
}

type ProcessSnapshot struct {
	Processes []*ProcessInfo
	Timestamp time.Time
}

func NewProcessSnapshot() *ProcessSnapshot {
	return &ProcessSnapshot{
		Processes: make([]*ProcessInfo, 0),
		Timestamp: time.Now(),
	}
}

func (s *ProcessSnapshot) Add(p *ProcessInfo) {
	s.Processes = append(s.Processes, p)
}

func (s *ProcessSnapshot) GetByPID(pid int) *ProcessInfo {
	for _, p := range s.Processes {
		if p.PID == pid {
			return p
		}
	}
	return nil
}

func (s *ProcessSnapshot) PIDs() []int {
	pids := make([]int, len(s.Processes))
	for i, p := range s.Processes {
		pids[i] = p.PID
	}
	return pids
}

type ProcessDiff struct {
	Added   []*ProcessInfo
	Removed []*ProcessInfo
}

func (s *ProcessSnapshot) Diff(old *ProcessSnapshot) *ProcessDiff {
	diff := &ProcessDiff{
		Added:   make([]*ProcessInfo, 0),
		Removed: make([]*ProcessInfo, 0),
	}

	oldPIDs := make(map[int]bool)
	for _, p := range old.Processes {
		oldPIDs[p.PID] = true
	}

	newPIDs := make(map[int]bool)
	for _, p := range s.Processes {
		newPIDs[p.PID] = true
	}

	for _, p := range s.Processes {
		if !oldPIDs[p.PID] {
			diff.Added = append(diff.Added, p)
		}
	}

	for _, p := range old.Processes {
		if !newPIDs[p.PID] {
			diff.Removed = append(diff.Removed, p)
		}
	}

	return diff
}

func (s *ProcessSnapshot) Zombies() []*ProcessInfo {
	zombies := make([]*ProcessInfo, 0)
	for _, p := range s.Processes {
		if p.IsZombie() {
			zombies = append(zombies, p)
		}
	}
	return zombies
}

func (s *ProcessSnapshot) Orphans() []*ProcessInfo {
	orphans := make([]*ProcessInfo, 0)
	for _, p := range s.Processes {
		if p.IsOrphan() {
			orphans = append(orphans, p)
		}
	}
	return orphans
}

func (m *ProcessManager) TakeSnapshot(ctx context.Context, containerID string) (*ProcessSnapshot, error) {
	if m.client == nil {
		return nil, fmt.Errorf("no Docker client available")
	}

	topResp, err := m.client.ContainerTop(ctx, containerID, []string{"-eo", "pid,ppid,state,user,cmd"})
	if err != nil {
		return nil, fmt.Errorf("failed to get container processes: %w", err)
	}

	snapshot := NewProcessSnapshot()

	for _, proc := range topResp.Processes {
		if len(proc) < 5 {
			continue
		}

		var pid, ppid int
		fmt.Sscanf(proc[0], "%d", &pid)
		fmt.Sscanf(proc[1], "%d", &ppid)

		info := &ProcessInfo{
			PID:     pid,
			PPID:    ppid,
			State:   proc[2],
			User:    proc[3],
			Command: proc[4],
		}

		snapshot.Add(info)
	}

	return snapshot, nil
}

func (m *ProcessManager) KillProcess(ctx context.Context, containerID string, pid int, signal string) error {
	if m.client == nil {
		return fmt.Errorf("no Docker client available")
	}

	execHandler := NewExecHandler(m.client)
	cmd := []string{"/bin/sh", "-c", fmt.Sprintf("kill -%s %d", signal, pid)}

	result, err := execHandler.Exec(ctx, containerID, cmd, nil)
	if err != nil {
		return fmt.Errorf("failed to kill process %d: %w", pid, err)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("kill command failed: %s", string(result.Stderr))
	}

	return nil
}

func (m *ProcessManager) KillOrphans(ctx context.Context, containerID string, signal string) (int, error) {
	snapshot, err := m.TakeSnapshot(ctx, containerID)
	if err != nil {
		return 0, err
	}

	orphans := snapshot.Orphans()
	killed := 0

	for _, p := range orphans {
		if p.PID == 1 {
			continue
		}

		if err := m.KillProcess(ctx, containerID, p.PID, signal); err != nil {
			continue
		}
		killed++
	}

	return killed, nil
}

func (m *ProcessManager) CleanupZombies(ctx context.Context, containerID string) (int, error) {
	snapshot, err := m.TakeSnapshot(ctx, containerID)
	if err != nil {
		return 0, err
	}

	zombies := snapshot.Zombies()
	return len(zombies), nil
}
