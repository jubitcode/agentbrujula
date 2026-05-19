package container

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusStarting  HealthStatus = "starting"
	HealthStatusUnknown   HealthStatus = "unknown"
)

type HealthChecker struct {
	client         *client.Client
	processManager *ProcessManager
}

func NewHealthChecker(dockerClient *client.Client) *HealthChecker {
	return &HealthChecker{
		client:         dockerClient,
		processManager: NewProcessManager(dockerClient),
	}
}

type HealthReport struct {
	ContainerID string
	Status      HealthStatus
	CheckedAt   time.Time
	StartedAt   time.Time

	ProcessCount int
	OrphanCount  int
	OrphanPIDs   []int
	ZombieCount  int
	ZombiePIDs   []int

	MemoryUsage int64
	MemoryLimit int64
	CPUPercent  float64

	NetworkRxBytes int64
	NetworkTxBytes int64

	Problems []string
}

func (r *HealthReport) MemoryPercent() float64 {
	if r.MemoryLimit == 0 {
		return 0
	}
	return float64(r.MemoryUsage) / float64(r.MemoryLimit) * 100
}

func (r *HealthReport) Uptime() time.Duration {
	if r.StartedAt.IsZero() {
		return 0
	}
	return time.Since(r.StartedAt)
}

func (r *HealthReport) AddProblem(problem string) {
	r.Problems = append(r.Problems, problem)
}

func (r *HealthReport) Evaluate(thresholds *HealthThresholds) {
	r.Status = HealthStatusHealthy

	memPercent := r.MemoryPercent()
	if r.MemoryLimit > 0 && memPercent > thresholds.MaxMemoryPercent {
		r.Problems = append(r.Problems, fmt.Sprintf("memory usage %.1f%% exceeds threshold %.1f%%", memPercent, thresholds.MaxMemoryPercent))
		r.Status = HealthStatusUnhealthy
	}

	if r.CPUPercent > thresholds.MaxCPUPercent {
		r.Problems = append(r.Problems, fmt.Sprintf("CPU usage %.1f%% exceeds threshold %.1f%%", r.CPUPercent, thresholds.MaxCPUPercent))
		r.Status = HealthStatusUnhealthy
	}

	if r.OrphanCount > thresholds.MaxOrphanCount {
		r.Problems = append(r.Problems, fmt.Sprintf("%d orphan processes detected (max %d allowed)", r.OrphanCount, thresholds.MaxOrphanCount))
		r.Status = HealthStatusUnhealthy
	}

	if r.ZombieCount > thresholds.MaxZombieCount {
		r.Problems = append(r.Problems, fmt.Sprintf("%d zombie processes detected (max %d allowed)", r.ZombieCount, thresholds.MaxZombieCount))
		r.Status = HealthStatusUnhealthy
	}
}

func (r *HealthReport) Summary() string {
	if len(r.Problems) == 0 {
		return fmt.Sprintf("%s: %d processes, %.1f%% memory, %.1f%% CPU",
			r.Status, r.ProcessCount, r.MemoryPercent(), r.CPUPercent)
	}

	problemText := "problem"
	if len(r.Problems) > 1 {
		problemText = "problems"
	}

	return fmt.Sprintf("%s: %d %s found", r.Status, len(r.Problems), problemText)
}

type HealthThresholds struct {
	MaxMemoryPercent float64
	MaxCPUPercent    float64
	MaxOrphanCount   int
	MaxZombieCount   int
}

var DefaultHealthThresholds = HealthThresholds{
	MaxMemoryPercent: 90.0,
	MaxCPUPercent:    95.0,
	MaxOrphanCount:   0,
	MaxZombieCount:   0,
}

type HealthCheckOptions struct {
	Thresholds     *HealthThresholds
	CheckProcesses bool
	CheckNetwork   bool
}

func (c *HealthChecker) Check(ctx context.Context, containerID string, opts *HealthCheckOptions) (*HealthReport, error) {
	if opts == nil {
		opts = &HealthCheckOptions{
			Thresholds:     &DefaultHealthThresholds,
			CheckProcesses: true,
			CheckNetwork:   false,
		}
	}
	if opts.Thresholds == nil {
		opts.Thresholds = &DefaultHealthThresholds
	}

	report := &HealthReport{
		ContainerID: containerID,
		Status:      HealthStatusUnknown,
		CheckedAt:   time.Now(),
	}

	info, err := c.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	if info.State.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, info.State.StartedAt); err == nil {
			report.StartedAt = t
		}
	}

	stats, err := c.client.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get container stats: %w", err)
	}
	defer stats.Body.Close()

	var statsJSON types.StatsJSON
	if err := parseStats(stats.Body, &statsJSON); err != nil {
		report.Problems = append(report.Problems, fmt.Sprintf("failed to parse stats: %v", err))
	} else {
		report.MemoryUsage = int64(statsJSON.MemoryStats.Usage)
		report.MemoryLimit = int64(statsJSON.MemoryStats.Limit)
		report.CPUPercent = calculateCPUPercent(&statsJSON)

		if opts.CheckNetwork {
			for _, netStats := range statsJSON.Networks {
				report.NetworkRxBytes += int64(netStats.RxBytes)
				report.NetworkTxBytes += int64(netStats.TxBytes)
			}
		}
	}

	if opts.CheckProcesses && c.processManager != nil {
		snapshot, err := c.processManager.TakeSnapshot(ctx, containerID)
		if err == nil {
			report.ProcessCount = len(snapshot.Processes)

			orphans := snapshot.Orphans()
			report.OrphanCount = len(orphans)
			for _, p := range orphans {
				if p.PID != 1 {
					report.OrphanPIDs = append(report.OrphanPIDs, p.PID)
				}
			}

			zombies := snapshot.Zombies()
			report.ZombieCount = len(zombies)
			for _, p := range zombies {
				report.ZombiePIDs = append(report.ZombiePIDs, p.PID)
			}
		}
	}

	report.Evaluate(opts.Thresholds)

	return report, nil
}

func (c *HealthChecker) QuickCheck(ctx context.Context, containerID string) (*HealthReport, error) {
	return c.Check(ctx, containerID, &HealthCheckOptions{
		Thresholds:     &DefaultHealthThresholds,
		CheckProcesses: false,
		CheckNetwork:   false,
	})
}

func calculateCPUPercent(stats *types.StatsJSON) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)

	if systemDelta > 0 && cpuDelta > 0 {
		cpuCount := float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
		if cpuCount == 0 {
			cpuCount = 1
		}
		return (cpuDelta / systemDelta) * cpuCount * 100.0
	}

	return 0
}

func parseStats(r interface{ Read([]byte) (int, error) }, stats *types.StatsJSON) error {
	buf := make([]byte, 8192)
	n, err := r.Read(buf)
	if err != nil {
		return err
	}

	return json.Unmarshal(buf[:n], stats)
}
