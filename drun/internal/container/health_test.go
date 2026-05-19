package container

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHealthCheckerNew(t *testing.T) {
	hc := NewHealthChecker(nil)
	assert.NotNil(t, hc)
}

func TestHealthStatusValues(t *testing.T) {
	assert.Equal(t, HealthStatus("healthy"), HealthStatusHealthy)
	assert.Equal(t, HealthStatus("unhealthy"), HealthStatusUnhealthy)
	assert.Equal(t, HealthStatus("starting"), HealthStatusStarting)
	assert.Equal(t, HealthStatus("unknown"), HealthStatusUnknown)
}

func TestHealthReportStruct(t *testing.T) {
	report := &HealthReport{
		ContainerID: "container123",
		Status:      HealthStatusUnknown,
	}
	assert.NotNil(t, report)
	assert.Equal(t, "container123", report.ContainerID)
	assert.Equal(t, HealthStatusUnknown, report.Status)
}

func TestHealthReportIsHealthy(t *testing.T) {
	healthy := &HealthReport{Status: HealthStatusHealthy}
	unhealthy := &HealthReport{Status: HealthStatusUnhealthy}
	starting := &HealthReport{Status: HealthStatusStarting}
	unknown := &HealthReport{Status: HealthStatusUnknown}

	assert.True(t, healthy.Status == HealthStatusHealthy)
	assert.False(t, unhealthy.Status == HealthStatusHealthy)
	assert.False(t, starting.Status == HealthStatusHealthy)
	assert.False(t, unknown.Status == HealthStatusHealthy)
}

func TestHealthReportProcessCount(t *testing.T) {
	report := &HealthReport{
		ProcessCount: 5,
	}

	assert.Equal(t, 5, report.ProcessCount)
}

func TestHealthReportOrphans(t *testing.T) {
	report := &HealthReport{
		OrphanCount: 2,
		OrphanPIDs:  []int{123, 456},
	}

	assert.Equal(t, 2, report.OrphanCount)
	assert.Contains(t, report.OrphanPIDs, 123)
	assert.Contains(t, report.OrphanPIDs, 456)
}

func TestHealthReportZombies(t *testing.T) {
	report := &HealthReport{
		ZombieCount: 1,
		ZombiePIDs:  []int{789},
	}

	assert.Equal(t, 1, report.ZombieCount)
	assert.Contains(t, report.ZombiePIDs, 789)
}

func TestHealthReportMemoryUsage(t *testing.T) {
	report := &HealthReport{
		MemoryUsage: 1024 * 1024 * 100, // 100 MB
		MemoryLimit: 1024 * 1024 * 512, // 512 MB
	}

	assert.Equal(t, int64(1024*1024*100), report.MemoryUsage)
	assert.Equal(t, int64(1024*1024*512), report.MemoryLimit)
}

func TestHealthReportMemoryPercent(t *testing.T) {
	report := &HealthReport{
		MemoryUsage: 100,
		MemoryLimit: 400,
	}

	percent := report.MemoryPercent()
	assert.Equal(t, 25.0, percent)
}

func TestHealthReportMemoryPercentNoLimit(t *testing.T) {
	report := &HealthReport{
		MemoryUsage: 100,
		MemoryLimit: 0,
	}

	percent := report.MemoryPercent()
	assert.Equal(t, 0.0, percent)
}

func TestHealthReportCPUUsage(t *testing.T) {
	report := &HealthReport{
		CPUPercent: 45.5,
	}

	assert.Equal(t, 45.5, report.CPUPercent)
}

func TestHealthReportUptime(t *testing.T) {
	startTime := time.Now().Add(-10 * time.Minute)
	report := &HealthReport{
		StartedAt: startTime,
	}

	uptime := report.Uptime()
	assert.True(t, uptime >= 10*time.Minute)
	assert.True(t, uptime < 11*time.Minute)
}

func TestHealthReportUptimeNotStarted(t *testing.T) {
	report := &HealthReport{}

	uptime := report.Uptime()
	assert.Equal(t, time.Duration(0), uptime)
}

func TestHealthReportNetwork(t *testing.T) {
	report := &HealthReport{
		NetworkRxBytes: 1024 * 1024,
		NetworkTxBytes: 512 * 1024,
	}

	assert.Equal(t, int64(1024*1024), report.NetworkRxBytes)
	assert.Equal(t, int64(512*1024), report.NetworkTxBytes)
}

func TestHealthReportProblems(t *testing.T) {
	report := &HealthReport{
		Problems: []string{"High memory usage", "Orphan processes detected"},
	}

	assert.Len(t, report.Problems, 2)
	assert.Contains(t, report.Problems, "High memory usage")
}

func TestHealthReportAddProblem(t *testing.T) {
	report := &HealthReport{ContainerID: "container123"}
	report.AddProblem("Test problem")

	assert.Len(t, report.Problems, 1)
	assert.Equal(t, "Test problem", report.Problems[0])
}

func TestHealthReportHasProblems(t *testing.T) {
	noProblems := &HealthReport{}
	hasProblems := &HealthReport{Problems: []string{"issue"}}

	assert.False(t, len(noProblems.Problems) > 0)
	assert.True(t, len(hasProblems.Problems) > 0)
}

func TestHealthThresholdsStruct(t *testing.T) {
	thresholds := &HealthThresholds{
		MaxMemoryPercent: 80.0,
		MaxCPUPercent:    75.0,
	}
	assert.NotNil(t, thresholds)
	assert.True(t, thresholds.MaxMemoryPercent > 0)
	assert.True(t, thresholds.MaxCPUPercent > 0)
}

func TestHealthThresholdsDefaults(t *testing.T) {
	thresholds := DefaultHealthThresholds

	assert.Equal(t, 90.0, thresholds.MaxMemoryPercent)
	assert.Equal(t, 95.0, thresholds.MaxCPUPercent)
	assert.Equal(t, 0, thresholds.MaxOrphanCount)
	assert.Equal(t, 0, thresholds.MaxZombieCount)
}

func TestHealthThresholdsCustom(t *testing.T) {
	thresholds := &HealthThresholds{
		MaxMemoryPercent: 80.0,
		MaxCPUPercent:    75.0,
		MaxOrphanCount:   5,
		MaxZombieCount:   3,
	}
	assert.Equal(t, 80.0, thresholds.MaxMemoryPercent)
	assert.Equal(t, 75.0, thresholds.MaxCPUPercent)
	assert.Equal(t, 5, thresholds.MaxOrphanCount)
	assert.Equal(t, 3, thresholds.MaxZombieCount)
}

func TestHealthReportEvaluateHealthy(t *testing.T) {
	report := &HealthReport{
		MemoryUsage:  100,
		MemoryLimit:  1000,
		CPUPercent:   50.0,
		OrphanCount:  0,
		ZombieCount:  0,
		ProcessCount: 5,
	}

	thresholds := &DefaultHealthThresholds
	report.Evaluate(thresholds)

	assert.Equal(t, HealthStatusHealthy, report.Status)
	assert.False(t, len(report.Problems) > 0)
}

func TestHealthReportEvaluateHighMemory(t *testing.T) {
	report := &HealthReport{
		MemoryUsage: 950,
		MemoryLimit: 1000,
		CPUPercent:  50.0,
	}

	thresholds := &DefaultHealthThresholds // 90% default
	report.Evaluate(thresholds)

	assert.Equal(t, HealthStatusUnhealthy, report.Status)
	assert.True(t, len(report.Problems) > 0)
}

func TestHealthReportEvaluateHighCPU(t *testing.T) {
	report := &HealthReport{
		MemoryUsage: 100,
		MemoryLimit: 1000,
		CPUPercent:  98.0,
	}

	thresholds := &DefaultHealthThresholds // 95% default
	report.Evaluate(thresholds)

	assert.Equal(t, HealthStatusUnhealthy, report.Status)
	assert.True(t, len(report.Problems) > 0)
}

func TestHealthReportEvaluateOrphans(t *testing.T) {
	report := &HealthReport{
		MemoryUsage: 100,
		MemoryLimit: 1000,
		OrphanCount: 3,
		OrphanPIDs:  []int{100, 200, 300},
	}

	thresholds := &DefaultHealthThresholds // 0 orphans allowed by default
	report.Evaluate(thresholds)

	assert.Equal(t, HealthStatusUnhealthy, report.Status)
	assert.True(t, len(report.Problems) > 0)
}

func TestHealthReportEvaluateZombies(t *testing.T) {
	report := &HealthReport{
		MemoryUsage: 100,
		MemoryLimit: 1000,
		ZombieCount: 1,
		ZombiePIDs:  []int{999},
	}

	thresholds := &DefaultHealthThresholds // 0 zombies allowed by default
	report.Evaluate(thresholds)

	assert.Equal(t, HealthStatusUnhealthy, report.Status)
	assert.True(t, len(report.Problems) > 0)
}

func TestHealthReportSummary(t *testing.T) {
	report := &HealthReport{
		ContainerID:  "abc123",
		Status:       HealthStatusHealthy,
		ProcessCount: 5,
		MemoryUsage:  1024 * 1024 * 100,
		MemoryLimit:  1024 * 1024 * 512,
		CPUPercent:   25.5,
	}

	summary := report.Summary()
	assert.Contains(t, summary, "healthy")
	assert.Contains(t, summary, "5 processes")
}

func TestHealthReportSummaryWithProblems(t *testing.T) {
	report := &HealthReport{
		ContainerID: "abc123",
		Status:      HealthStatusUnhealthy,
		Problems:    []string{"High memory usage"},
	}

	summary := report.Summary()
	assert.Contains(t, summary, "unhealthy")
	assert.Contains(t, summary, "1 problem")
}

func TestHealthCheckOptionsStruct(t *testing.T) {
	opts := &HealthCheckOptions{
		Thresholds: &DefaultHealthThresholds,
	}
	assert.NotNil(t, opts)
	assert.NotNil(t, opts.Thresholds)
}

func TestHealthCheckOptionsWithThresholds(t *testing.T) {
	thresholds := &HealthThresholds{MaxMemoryPercent: 50.0}
	opts := &HealthCheckOptions{Thresholds: thresholds}

	assert.Equal(t, 50.0, opts.Thresholds.MaxMemoryPercent)
}

func TestHealthCheckOptionsFields(t *testing.T) {
	opts := &HealthCheckOptions{
		CheckProcesses: true,
		CheckNetwork:   true,
	}
	assert.True(t, opts.CheckProcesses)
	assert.True(t, opts.CheckNetwork)
}
