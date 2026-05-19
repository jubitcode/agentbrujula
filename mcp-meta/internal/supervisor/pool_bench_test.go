package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mcp-meta/internal/registry"
)

func BenchmarkPodPool(b *testing.B) {
	cases := []struct {
		name     string
		replicas int
	}{
		{"single", 1},
		{"pool-2", 2},
		{"pool-4", 4},
		{"pool-8", 8},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			spec := &registry.PodSpec{
				Name:    "bench-pod",
				Command: []string{"go", "run", "../../test/testpod/main.go"},
				CallTimeout: registry.Duration{
					Duration: 30 * time.Second,
				},
				ShutdownTimeout: registry.Duration{
					Duration: 5 * time.Second,
				},
			}
			if tc.replicas > 1 {
				spec.Scaling = &registry.ScalingSpec{
					MinReplicas: tc.replicas,
					MaxReplicas: tc.replicas,
				}
			}

			pool := NewPodPool("bench-pod", spec, nil, nil)
			ctx := context.Background()

			if err := pool.Start(ctx); err != nil {
				b.Fatalf("failed to start pool: %v", err)
			}
			defer pool.Shutdown(ctx)

			// Warm up
			pod, _ := pool.Acquire()
			pod.Call(ctx, "ping", nil)

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					pod, err := pool.Acquire()
					if err != nil {
						b.Error(err)
						continue
					}
					_, err = pod.Call(ctx, "echo", json.RawMessage(`{"x":1}`))
					if err != nil {
						b.Error(err)
					}
				}
			})
		})
	}
}

// TestPoolThroughputComparison runs a throughput comparison test
func TestPoolThroughputComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping throughput test in short mode")
	}

	configs := []struct {
		name     string
		replicas int
	}{
		{"single", 1},
		{"pool-2", 2},
		{"pool-4", 4},
		{"pool-8", 8},
	}

	const (
		numRequests  = 500
		numWorkers   = 50
	)

	results := make(map[string]struct {
		duration   time.Duration
		throughput float64
	})

	for _, cfg := range configs {
		spec := &registry.PodSpec{
			Name:    "throughput-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
			ShutdownTimeout: registry.Duration{
				Duration: 5 * time.Second,
			},
		}
		if cfg.replicas > 1 {
			spec.Scaling = &registry.ScalingSpec{
				MinReplicas: cfg.replicas,
				MaxReplicas: cfg.replicas,
			}
		}

		pool := NewPodPool("throughput-test", spec, nil, nil)
		ctx := context.Background()

		if err := pool.Start(ctx); err != nil {
			t.Fatalf("failed to start pool for %s: %v", cfg.name, err)
		}

		// Warm up
		pod, _ := pool.Acquire()
		pod.Call(ctx, "ping", nil)

		// Run concurrent requests
		var wg sync.WaitGroup
		var success, failed atomic.Int64
		requestCh := make(chan int, numRequests)

		// Fill request channel
		for i := 0; i < numRequests; i++ {
			requestCh <- i
		}
		close(requestCh)

		start := time.Now()

		// Start workers
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range requestCh {
					pod, err := pool.Acquire()
					if err != nil {
						failed.Add(1)
						continue
					}
					_, err = pod.Call(ctx, "echo", json.RawMessage(`{"test":true}`))
					if err != nil {
						failed.Add(1)
					} else {
						success.Add(1)
					}
				}
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		pool.Shutdown(ctx)

		throughput := float64(success.Load()) / elapsed.Seconds()
		results[cfg.name] = struct {
			duration   time.Duration
			throughput float64
		}{elapsed, throughput}

		t.Logf("%s: %d requests in %v (%.1f req/sec), %d failed",
			cfg.name, success.Load(), elapsed.Round(time.Millisecond), throughput, failed.Load())
	}

	// Print comparison
	t.Log("\n=== Throughput Comparison ===")
	t.Logf("%-10s %12s %12s %10s", "Config", "Duration", "Throughput", "Speedup")
	t.Logf("%-10s %12s %12s %10s", "------", "--------", "----------", "-------")

	baseThroughput := results["single"].throughput
	for _, cfg := range configs {
		r := results[cfg.name]
		speedup := r.throughput / baseThroughput
		t.Logf("%-10s %12v %10.1f/s %9.2fx",
			cfg.name, r.duration.Round(time.Millisecond), r.throughput, speedup)
	}
}

// TestPoolLatencyComparison measures p50/p99 latency
func TestPoolLatencyComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency test in short mode")
	}

	configs := []struct {
		name     string
		replicas int
	}{
		{"single", 1},
		{"pool-4", 4},
	}

	const numRequests = 200

	for _, cfg := range configs {
		spec := &registry.PodSpec{
			Name:    "latency-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 30 * time.Second,
			},
			ShutdownTimeout: registry.Duration{
				Duration: 5 * time.Second,
			},
		}
		if cfg.replicas > 1 {
			spec.Scaling = &registry.ScalingSpec{
				MinReplicas: cfg.replicas,
				MaxReplicas: cfg.replicas,
			}
		}

		pool := NewPodPool("latency-test", spec, nil, nil)
		ctx := context.Background()

		if err := pool.Start(ctx); err != nil {
			t.Fatalf("failed to start pool for %s: %v", cfg.name, err)
		}

		// Warm up
		pod, _ := pool.Acquire()
		pod.Call(ctx, "ping", nil)

		// Collect latencies
		latencies := make([]time.Duration, 0, numRequests)
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				start := time.Now()
				pod, err := pool.Acquire()
				if err != nil {
					return
				}
				_, err = pod.Call(ctx, "echo", json.RawMessage(`{"x":1}`))
				if err != nil {
					return
				}
				elapsed := time.Since(start)

				mu.Lock()
				latencies = append(latencies, elapsed)
				mu.Unlock()
			}()
		}

		wg.Wait()
		pool.Shutdown(ctx)

		// Sort and compute percentiles
		sortDurations(latencies)

		p50 := latencies[len(latencies)*50/100]
		p95 := latencies[len(latencies)*95/100]
		p99 := latencies[len(latencies)*99/100]

		t.Logf("%s: p50=%v, p95=%v, p99=%v",
			cfg.name, p50.Round(100*time.Microsecond),
			p95.Round(100*time.Microsecond),
			p99.Round(100*time.Microsecond))
	}
}

func sortDurations(d []time.Duration) {
	for i := 0; i < len(d); i++ {
		for j := i + 1; j < len(d); j++ {
			if d[j] < d[i] {
				d[i], d[j] = d[j], d[i]
			}
		}
	}
}

// TestSlowWorkloadComparison compares single vs pooled with simulated work delay
func TestSlowWorkloadComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow workload test in short mode")
	}

	configs := []struct {
		name     string
		replicas int
	}{
		{"single", 1},
		{"pool-4", 4},
		{"pool-8", 8},
	}

	const numRequests = 100 // Each takes ~100ms, so 100 requests = 10s if single

	t.Log("Slow Workload Comparison (100ms simulated work per request)")
	t.Logf("%-10s %12s %12s %10s", "Config", "Duration", "Throughput", "Speedup")
	t.Logf("%-10s %12s %12s %10s", "------", "--------", "----------", "-------")

	var baseDuration time.Duration

	for _, cfg := range configs {
		spec := &registry.PodSpec{
			Name:    "slow-test",
			Command: []string{"go", "run", "../../test/testpod/main.go"},
			CallTimeout: registry.Duration{
				Duration: 60 * time.Second,
			},
			ShutdownTimeout: registry.Duration{
				Duration: 5 * time.Second,
			},
		}
		if cfg.replicas > 1 {
			spec.Scaling = &registry.ScalingSpec{
				MinReplicas: cfg.replicas,
				MaxReplicas: cfg.replicas,
			}
		}

		pool := NewPodPool("slow-test", spec, nil, nil)
		ctx := context.Background()

		if err := pool.Start(ctx); err != nil {
			t.Fatalf("failed to start pool for %s: %v", cfg.name, err)
		}

		// Warm up
		for i := 0; i < cfg.replicas; i++ {
			pod, _ := pool.Acquire()
			pod.Call(ctx, "ping", nil)
		}

		// Run concurrent "slow" requests (100ms each)
		var wg sync.WaitGroup
		var success atomic.Int64

		start := time.Now()

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pod, err := pool.Acquire()
				if err != nil {
					return
				}
				// "slow" method sleeps for 100ms
				_, err = pod.Call(ctx, "slow", nil)
				if err == nil {
					success.Add(1)
				}
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		pool.Shutdown(ctx)

		if cfg.name == "single" {
			baseDuration = elapsed
		}

		speedup := float64(baseDuration) / float64(elapsed)
		throughput := float64(success.Load()) / elapsed.Seconds()

		t.Logf("%-10s %12v %10.1f/s %9.2fx",
			cfg.name, elapsed.Round(time.Millisecond), throughput, speedup)
	}
}

// TestHighConcurrencyLoad simulates 1000 concurrent requests
func TestHighConcurrencyLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping high concurrency test in short mode")
	}

	spec := &registry.PodSpec{
		Name:    "load-test",
		Command: []string{"go", "run", "../../test/testpod/main.go"},
		CallTimeout: registry.Duration{
			Duration: 60 * time.Second,
		},
		ShutdownTimeout: registry.Duration{
			Duration: 5 * time.Second,
		},
		Scaling: &registry.ScalingSpec{
			MinReplicas: 8,
			MaxReplicas: 8,
		},
	}

	pool := NewPodPool("load-test", spec, nil, nil)
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(ctx)

	// Warm up all replicas
	for i := 0; i < 8; i++ {
		pod, _ := pool.Acquire()
		pod.Call(ctx, "ping", nil)
	}

	const numRequests = 1000
	var wg sync.WaitGroup
	var success, failed atomic.Int64
	replicaCounts := make(map[string]*atomic.Int64)

	start := time.Now()

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			pod, release, err := pool.AcquireWithTracking()
			if err != nil {
				failed.Add(1)
				return
			}
			defer release()

			result, err := pod.Call(ctx, "echo", json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)))
			if err != nil {
				failed.Add(1)
				return
			}

			var resp map[string]interface{}
			json.Unmarshal(result, &resp)
			if replica, ok := resp["handled_by"].(string); ok {
				if _, exists := replicaCounts[replica]; !exists {
					replicaCounts[replica] = &atomic.Int64{}
				}
				replicaCounts[replica].Add(1)
			}
			success.Add(1)
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("High Concurrency Load Test (8 replicas)")
	t.Logf("  Requests: %d total, %d success, %d failed", numRequests, success.Load(), failed.Load())
	t.Logf("  Duration: %v", elapsed.Round(time.Millisecond))
	t.Logf("  Throughput: %.1f req/sec", float64(success.Load())/elapsed.Seconds())
	t.Log("  Distribution:")
	for replica, count := range replicaCounts {
		t.Logf("    %s: %d requests (%.1f%%)", replica, count.Load(), float64(count.Load())/float64(numRequests)*100)
	}

	if failed.Load() > 10 {
		t.Errorf("too many failures: %d", failed.Load())
	}
}
