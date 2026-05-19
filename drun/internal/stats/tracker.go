package stats

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/client"
)

type RecommendationType string

const (
	RecommendationTypeMemory  RecommendationType = "memory"
	RecommendationTypeCPU     RecommendationType = "cpu"
	RecommendationTypeGeneral RecommendationType = "general"
)

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

type Tracker struct {
	client  *client.Client
	history map[string]*StatsHistory
	mu      sync.RWMutex
}

func NewTracker(dockerClient *client.Client) *Tracker {
	return &Tracker{
		client:  dockerClient,
		history: make(map[string]*StatsHistory),
	}
}

type StatsSample struct {
	Timestamp     time.Time
	CPUPercent    float64
	MemoryBytes   int64
	MemoryLimit   int64
	NetworkRx     int64
	NetworkTx     int64
	BlockRead     int64
	BlockWrite    int64
	PIDs          int
}

func (s *StatsSample) MemoryPercent() float64 {
	if s.MemoryLimit == 0 {
		return 0
	}
	return float64(s.MemoryBytes) / float64(s.MemoryLimit) * 100
}

type StatsHistory struct {
	Samples    []*StatsSample
	MaxSamples int
	mu         sync.RWMutex
}

func NewStatsHistory(maxSamples int) *StatsHistory {
	return &StatsHistory{
		Samples:    make([]*StatsSample, 0),
		MaxSamples: maxSamples,
	}
}

func (h *StatsHistory) Add(sample *StatsSample) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.Samples = append(h.Samples, sample)
	if len(h.Samples) > h.MaxSamples {
		h.Samples = h.Samples[1:]
	}
}

func (h *StatsHistory) Average() *StatsSample {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.Samples) == 0 {
		return &StatsSample{}
	}

	var totalCPU float64
	var totalMem int64
	for _, s := range h.Samples {
		totalCPU += s.CPUPercent
		totalMem += s.MemoryBytes
	}

	count := float64(len(h.Samples))
	return &StatsSample{
		CPUPercent:  totalCPU / count,
		MemoryBytes: totalMem / int64(len(h.Samples)),
	}
}

func (h *StatsHistory) Max() *StatsSample {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.Samples) == 0 {
		return &StatsSample{}
	}

	max := &StatsSample{}
	for _, s := range h.Samples {
		if s.CPUPercent > max.CPUPercent {
			max.CPUPercent = s.CPUPercent
		}
		if s.MemoryBytes > max.MemoryBytes {
			max.MemoryBytes = s.MemoryBytes
		}
	}

	return max
}

func (h *StatsHistory) Min() *StatsSample {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.Samples) == 0 {
		return &StatsSample{}
	}

	min := &StatsSample{
		CPUPercent:  h.Samples[0].CPUPercent,
		MemoryBytes: h.Samples[0].MemoryBytes,
	}
	for _, s := range h.Samples {
		if s.CPUPercent < min.CPUPercent {
			min.CPUPercent = s.CPUPercent
		}
		if s.MemoryBytes < min.MemoryBytes {
			min.MemoryBytes = s.MemoryBytes
		}
	}

	return min
}

type StatsReport struct {
	ContainerID      string
	Duration         time.Duration
	SampleCount      int
	AvgCPUPercent    float64
	MaxCPUPercent    float64
	MinCPUPercent    float64
	AvgMemory        int64
	MaxMemory        int64
	MinMemory        int64
	AvgMemoryPercent float64
	MaxMemoryPercent float64
}

func NewStatsReport() *StatsReport {
	return &StatsReport{}
}

func (r *StatsReport) Summary() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Container: %s\n", r.ContainerID))
	sb.WriteString(fmt.Sprintf("Duration: %s\n", r.Duration))
	sb.WriteString(fmt.Sprintf("Samples: %d\n", r.SampleCount))
	sb.WriteString(fmt.Sprintf("CPU: avg=%.1f%%, max=%.1f%%\n", r.AvgCPUPercent, r.MaxCPUPercent))
	sb.WriteString(fmt.Sprintf("Memory: avg=%s, max=%s\n", FormatBytes(r.AvgMemory), FormatBytes(r.MaxMemory)))

	return sb.String()
}

type TrackerOptions struct {
	Interval   time.Duration
	MaxSamples int
}

func NewTrackerOptions() *TrackerOptions {
	return &TrackerOptions{
		Interval:   1 * time.Second,
		MaxSamples: 3600,
	}
}

func (o *TrackerOptions) WithInterval(d time.Duration) *TrackerOptions {
	o.Interval = d
	return o
}

func (o *TrackerOptions) WithMaxSamples(n int) *TrackerOptions {
	o.MaxSamples = n
	return o
}

type Recommendation struct {
	Type        RecommendationType
	Description string
	Priority    Priority
}

func GenerateRecommendations(report *StatsReport) []*Recommendation {
	var recs []*Recommendation

	if report.AvgMemoryPercent > 90 || report.MaxMemoryPercent > 95 {
		recs = append(recs, &Recommendation{
			Type:        RecommendationTypeMemory,
			Description: "Memory usage is consistently high. Consider increasing memory limit.",
			Priority:    PriorityHigh,
		})
	}

	if report.AvgCPUPercent > 90 || report.MaxCPUPercent > 95 {
		recs = append(recs, &Recommendation{
			Type:        RecommendationTypeCPU,
			Description: "CPU usage is consistently high. Consider allocating more CPU resources.",
			Priority:    PriorityHigh,
		})
	}

	return recs
}

type StatsDisplay struct{}

func NewStatsDisplay() *StatsDisplay {
	return &StatsDisplay{}
}

func (d *StatsDisplay) FormatSample(sample *StatsSample) string {
	return fmt.Sprintf("CPU: %.1f%%, Memory: %s (%.1f%%)",
		sample.CPUPercent,
		FormatBytes(sample.MemoryBytes),
		sample.MemoryPercent())
}

func (d *StatsDisplay) FormatReport(report *StatsReport) string {
	return report.Summary()
}

func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func (t *Tracker) Start(ctx context.Context, containerID string, opts *TrackerOptions) error {
	if t.client == nil {
		return fmt.Errorf("no Docker client available")
	}

	if opts == nil {
		opts = NewTrackerOptions()
	}

	t.mu.Lock()
	t.history[containerID] = NewStatsHistory(opts.MaxSamples)
	t.mu.Unlock()

	return nil
}

func (t *Tracker) GetHistory(containerID string) *StatsHistory {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.history[containerID]
}

func (t *Tracker) GenerateReport(containerID string) *StatsReport {
	history := t.GetHistory(containerID)
	if history == nil {
		return NewStatsReport()
	}

	avg := history.Average()
	max := history.Max()
	min := history.Min()

	return &StatsReport{
		ContainerID:   containerID,
		SampleCount:   len(history.Samples),
		AvgCPUPercent: avg.CPUPercent,
		MaxCPUPercent: max.CPUPercent,
		MinCPUPercent: min.CPUPercent,
		AvgMemory:     avg.MemoryBytes,
		MaxMemory:     max.MemoryBytes,
		MinMemory:     min.MemoryBytes,
	}
}
