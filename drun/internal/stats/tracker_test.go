package stats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTrackerNew(t *testing.T) {
	tracker := NewTracker(nil)
	assert.NotNil(t, tracker)
}

func TestStatsSampleNew(t *testing.T) {
	sample := &StatsSample{
		Timestamp:   time.Now(),
		CPUPercent:  25.5,
		MemoryBytes: 1024 * 1024 * 100,
		MemoryLimit: 1024 * 1024 * 512,
	}

	assert.Equal(t, 25.5, sample.CPUPercent)
	assert.Equal(t, int64(1024*1024*100), sample.MemoryBytes)
}

func TestStatsSampleMemoryPercent(t *testing.T) {
	sample := &StatsSample{
		MemoryBytes: 100,
		MemoryLimit: 400,
	}

	assert.Equal(t, 25.0, sample.MemoryPercent())
}

func TestStatsSampleMemoryPercentNoLimit(t *testing.T) {
	sample := &StatsSample{
		MemoryBytes: 100,
		MemoryLimit: 0,
	}

	assert.Equal(t, 0.0, sample.MemoryPercent())
}

func TestStatsHistoryNew(t *testing.T) {
	history := NewStatsHistory(100)
	assert.NotNil(t, history)
	assert.Equal(t, 100, history.MaxSamples)
}

func TestStatsHistoryAdd(t *testing.T) {
	history := NewStatsHistory(10)
	sample := &StatsSample{CPUPercent: 25.0}

	history.Add(sample)
	assert.Len(t, history.Samples, 1)
}

func TestStatsHistoryMaxSamples(t *testing.T) {
	history := NewStatsHistory(3)

	history.Add(&StatsSample{CPUPercent: 10.0})
	history.Add(&StatsSample{CPUPercent: 20.0})
	history.Add(&StatsSample{CPUPercent: 30.0})
	history.Add(&StatsSample{CPUPercent: 40.0})

	assert.Len(t, history.Samples, 3)
	assert.Equal(t, 20.0, history.Samples[0].CPUPercent)
	assert.Equal(t, 40.0, history.Samples[2].CPUPercent)
}

func TestStatsHistoryAverage(t *testing.T) {
	history := NewStatsHistory(10)

	history.Add(&StatsSample{CPUPercent: 10.0, MemoryBytes: 100})
	history.Add(&StatsSample{CPUPercent: 20.0, MemoryBytes: 200})
	history.Add(&StatsSample{CPUPercent: 30.0, MemoryBytes: 300})

	avg := history.Average()
	assert.Equal(t, 20.0, avg.CPUPercent)
	assert.Equal(t, int64(200), avg.MemoryBytes)
}

func TestStatsHistoryAverageEmpty(t *testing.T) {
	history := NewStatsHistory(10)

	avg := history.Average()
	assert.Equal(t, 0.0, avg.CPUPercent)
}

func TestStatsHistoryMax(t *testing.T) {
	history := NewStatsHistory(10)

	history.Add(&StatsSample{CPUPercent: 10.0, MemoryBytes: 100})
	history.Add(&StatsSample{CPUPercent: 50.0, MemoryBytes: 500})
	history.Add(&StatsSample{CPUPercent: 30.0, MemoryBytes: 300})

	max := history.Max()
	assert.Equal(t, 50.0, max.CPUPercent)
	assert.Equal(t, int64(500), max.MemoryBytes)
}

func TestStatsHistoryMin(t *testing.T) {
	history := NewStatsHistory(10)

	history.Add(&StatsSample{CPUPercent: 10.0, MemoryBytes: 100})
	history.Add(&StatsSample{CPUPercent: 50.0, MemoryBytes: 500})
	history.Add(&StatsSample{CPUPercent: 30.0, MemoryBytes: 300})

	min := history.Min()
	assert.Equal(t, 10.0, min.CPUPercent)
	assert.Equal(t, int64(100), min.MemoryBytes)
}

func TestStatsReportNew(t *testing.T) {
	report := NewStatsReport()
	assert.NotNil(t, report)
}

func TestStatsReportSummary(t *testing.T) {
	report := &StatsReport{
		ContainerID:   "abc123",
		Duration:      1 * time.Hour,
		SampleCount:   60,
		AvgCPUPercent: 25.5,
		MaxCPUPercent: 80.0,
		AvgMemory:     1024 * 1024 * 100,
		MaxMemory:     1024 * 1024 * 200,
	}

	summary := report.Summary()
	assert.Contains(t, summary, "CPU")
	assert.Contains(t, summary, "Memory")
}

func TestTrackerOptionsNew(t *testing.T) {
	opts := NewTrackerOptions()
	assert.NotNil(t, opts)
}

func TestTrackerOptionsDefaults(t *testing.T) {
	opts := NewTrackerOptions()
	assert.Equal(t, 1*time.Second, opts.Interval)
	assert.Equal(t, 3600, opts.MaxSamples)
}

func TestTrackerOptionsWithInterval(t *testing.T) {
	opts := NewTrackerOptions().WithInterval(5 * time.Second)
	assert.Equal(t, 5*time.Second, opts.Interval)
}

func TestTrackerOptionsWithMaxSamples(t *testing.T) {
	opts := NewTrackerOptions().WithMaxSamples(100)
	assert.Equal(t, 100, opts.MaxSamples)
}

func TestRecommendationNew(t *testing.T) {
	rec := &Recommendation{
		Type:        RecommendationTypeMemory,
		Description: "Consider increasing memory limit",
		Priority:    PriorityHigh,
	}

	assert.Equal(t, RecommendationTypeMemory, rec.Type)
	assert.Equal(t, PriorityHigh, rec.Priority)
}

func TestRecommendationTypeValues(t *testing.T) {
	assert.Equal(t, RecommendationType("memory"), RecommendationTypeMemory)
	assert.Equal(t, RecommendationType("cpu"), RecommendationTypeCPU)
	assert.Equal(t, RecommendationType("general"), RecommendationTypeGeneral)
}

func TestPriorityValues(t *testing.T) {
	assert.Equal(t, Priority("low"), PriorityLow)
	assert.Equal(t, Priority("medium"), PriorityMedium)
	assert.Equal(t, Priority("high"), PriorityHigh)
}

func TestGenerateRecommendationsHighMemory(t *testing.T) {
	report := &StatsReport{
		AvgCPUPercent:    50.0,
		MaxCPUPercent:    70.0,
		AvgMemoryPercent: 95.0,
		MaxMemoryPercent: 99.0,
	}

	recs := GenerateRecommendations(report)
	assert.True(t, len(recs) > 0)

	hasMemoryRec := false
	for _, r := range recs {
		if r.Type == RecommendationTypeMemory {
			hasMemoryRec = true
			break
		}
	}
	assert.True(t, hasMemoryRec)
}

func TestGenerateRecommendationsHighCPU(t *testing.T) {
	report := &StatsReport{
		AvgCPUPercent:    95.0,
		MaxCPUPercent:    100.0,
		AvgMemoryPercent: 50.0,
	}

	recs := GenerateRecommendations(report)
	assert.True(t, len(recs) > 0)

	hasCPURec := false
	for _, r := range recs {
		if r.Type == RecommendationTypeCPU {
			hasCPURec = true
			break
		}
	}
	assert.True(t, hasCPURec)
}

func TestGenerateRecommendationsHealthy(t *testing.T) {
	report := &StatsReport{
		AvgCPUPercent:    25.0,
		MaxCPUPercent:    50.0,
		AvgMemoryPercent: 30.0,
		MaxMemoryPercent: 40.0,
	}

	recs := GenerateRecommendations(report)
	assert.Len(t, recs, 0)
}

func TestStatsDisplayNew(t *testing.T) {
	display := NewStatsDisplay()
	assert.NotNil(t, display)
}

func TestStatsDisplayFormatSample(t *testing.T) {
	display := NewStatsDisplay()
	sample := &StatsSample{
		CPUPercent:  25.5,
		MemoryBytes: 1024 * 1024 * 100,
		MemoryLimit: 1024 * 1024 * 512,
	}

	output := display.FormatSample(sample)
	assert.Contains(t, output, "CPU")
	assert.Contains(t, output, "Memory")
}

func TestStatsDisplayFormatReport(t *testing.T) {
	display := NewStatsDisplay()
	report := &StatsReport{
		ContainerID:   "abc123",
		Duration:      1 * time.Hour,
		AvgCPUPercent: 25.5,
		MaxCPUPercent: 80.0,
	}

	output := display.FormatReport(report)
	assert.Contains(t, output, "abc123")
	assert.Contains(t, output, "CPU")
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatBytes(tt.bytes))
		})
	}
}
