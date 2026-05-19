package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"mcp-meta/internal/registry"
)

type Supervisor interface {
	EnsurePod(ctx context.Context, podName string, spec *registry.PodSpec) (PodInterface, error)
	EnsurePool(ctx context.Context, podName string, spec *registry.PodSpec) (*PodPool, error)
	GetPod(podName string) PodInterface
	GetPool(podName string) *PodPool
	TerminatePod(ctx context.Context, podName string) error
	Shutdown(ctx context.Context) error
	Status() map[string]PodStatus
	PoolStatus() map[string]PoolStatus
	PreWarmPods(ctx context.Context, specs []*registry.PodSpec) error
	SetInterPodHandler(handler InterPodCallHandler)
	SetInterPodHandlerWithContract(handler InterPodCallHandlerWithContract)
	SetRuntimeRegistry(registry *RuntimeRegistry)
	SubscribeProgress() (<-chan json.RawMessage, func())
	EmitProgress(event json.RawMessage)
}

// PoolStatus provides status information for a pod pool.
type PoolStatus struct {
	Name         string
	Size         int
	HealthyCount int
	Pending      int64
	Pods         []PodStatus
}

type supervisor struct {
	pools                       map[string]*PodPool
	mu                          sync.RWMutex
	logger                      *slog.Logger
	interPodHandler             InterPodCallHandler
	interPodHandlerWithContract InterPodCallHandlerWithContract
	runtimeRegistry             *RuntimeRegistry

	// Global progress bus — all pods emit progress here
	progressMu   sync.Mutex
	progressSubs map[int64]chan json.RawMessage
	nextSubID    int64
}

func New(logger *slog.Logger) Supervisor {
	s := &supervisor{
		pools:        make(map[string]*PodPool),
		progressSubs: make(map[int64]chan json.RawMessage),
		logger:       logger,
	}
	SetGlobalProgressHandler(s.EmitProgress)
	return s
}

// SubscribeProgress subscribes to progress notifications from all pods.
func (s *supervisor) SubscribeProgress() (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage, 64)
	s.progressMu.Lock()
	s.nextSubID++
	id := s.nextSubID
	s.progressSubs[id] = ch
	s.progressMu.Unlock()
	return ch, func() {
		s.progressMu.Lock()
		delete(s.progressSubs, id)
		s.progressMu.Unlock()
	}
}

// EmitProgress broadcasts a progress event to all subscribers.
func (s *supervisor) EmitProgress(event json.RawMessage) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	for _, ch := range s.progressSubs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *supervisor) SetRuntimeRegistry(registry *RuntimeRegistry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeRegistry = registry
}

func (s *supervisor) SetInterPodHandler(handler InterPodCallHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interPodHandler = handler
	for _, pool := range s.pools {
		pool.SetInterPodHandler(handler)
	}
}

func (s *supervisor) SetInterPodHandlerWithContract(handler InterPodCallHandlerWithContract) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interPodHandlerWithContract = handler
	for _, pool := range s.pools {
		pool.SetInterPodHandlerWithContract(handler)
	}
}

// EnsurePool ensures a pod pool exists and is started for the given pod specification.
func (s *supervisor) EnsurePool(ctx context.Context, podName string, spec *registry.PodSpec) (*PodPool, error) {
	s.mu.RLock()
	if pool, ok := s.pools[podName]; ok {
		s.mu.RUnlock()
		// Pool exists, check if it has healthy pods
		if pool.HealthyCount() > 0 {
			return pool, nil
		}
	} else {
		s.mu.RUnlock()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, ok := s.pools[podName]; ok {
		if pool.HealthyCount() > 0 {
			return pool, nil
		}
		// Pool has no healthy pods, shut it down and create a new one
		pool.Shutdown(ctx)
		delete(s.pools, podName)
	}

	pool := NewPodPool(podName, spec, s.logger, s.runtimeRegistry)
	if s.interPodHandlerWithContract != nil {
		pool.SetInterPodHandlerWithContract(s.interPodHandlerWithContract)
	} else if s.interPodHandler != nil {
		pool.SetInterPodHandler(s.interPodHandler)
	}

	if err := pool.Start(ctx); err != nil {
		return nil, fmt.Errorf("start pool %s: %w", podName, err)
	}

	s.pools[podName] = pool
	return pool, nil
}

// EnsurePod ensures a pod is available and returns one from the pool.
// This maintains backward compatibility with code that expects a single pod.
func (s *supervisor) EnsurePod(ctx context.Context, podName string, spec *registry.PodSpec) (PodInterface, error) {
	pool, err := s.EnsurePool(ctx, podName, spec)
	if err != nil {
		return nil, err
	}
	return pool.Acquire()
}

// GetPool returns the pool for the given pod name, or nil if not found.
func (s *supervisor) GetPool(podName string) *PodPool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pools[podName]
}

// GetPod returns a pod from the pool for the given pod name.
func (s *supervisor) GetPod(podName string) PodInterface {
	s.mu.RLock()
	pool, ok := s.pools[podName]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	pod, err := pool.Acquire()
	if err != nil {
		return nil
	}
	return pod
}

func (s *supervisor) TerminatePod(ctx context.Context, podName string) error {
	s.mu.Lock()
	pool, ok := s.pools[podName]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("pod not found: %s", podName)
	}
	delete(s.pools, podName)
	s.mu.Unlock()

	return pool.Shutdown(ctx)
}

func (s *supervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	pools := make([]*PodPool, 0, len(s.pools))
	for _, pool := range s.pools {
		pools = append(pools, pool)
	}
	s.pools = make(map[string]*PodPool)
	s.mu.Unlock()

	var firstErr error
	for _, pool := range pools {
		if err := pool.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *supervisor) Status() map[string]PodStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]PodStatus)
	for _, pool := range s.pools {
		for _, status := range pool.Status() {
			result[status.Name] = status
		}
	}
	return result
}

func (s *supervisor) PoolStatus() map[string]PoolStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]PoolStatus, len(s.pools))
	for name, pool := range s.pools {
		result[name] = PoolStatus{
			Name:         name,
			Size:         pool.Size(),
			HealthyCount: pool.HealthyCount(),
			Pending:      pool.PendingCount(),
			Pods:         pool.Status(),
		}
	}
	return result
}

func (s *supervisor) PreWarmPods(ctx context.Context, specs []*registry.PodSpec) error {
	if len(specs) == 0 {
		return nil
	}

	if s.logger != nil {
		s.logger.Info("prewarming pods", "count", len(specs))
	}

	var firstErr error
	for _, spec := range specs {
		timeout := spec.StartupTimeout.Duration
		if timeout == 0 {
			timeout = registry.DefaultStartupTimeout
		}

		podCtx, cancel := context.WithTimeout(ctx, timeout)
		pool, err := s.EnsurePool(podCtx, spec.Name, spec)
		cancel()

		if err != nil {
			if s.logger != nil {
				s.logger.Error("prewarm failed", "pod", spec.Name, "error", err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("prewarm %s: %w", spec.Name, err)
			}
			continue
		}

		// Ping one pod from the pool to verify it's working
		pod, acquireErr := pool.Acquire()
		if acquireErr != nil {
			if s.logger != nil {
				s.logger.Warn("prewarm acquire failed", "pod", spec.Name, "error", acquireErr)
			}
			continue
		}

		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		_, pingErr := pod.Call(pingCtx, "ping", nil)
		pingCancel()

		if pingErr != nil {
			if s.logger != nil {
				s.logger.Warn("prewarm ping failed", "pod", spec.Name, "error", pingErr)
			}
		} else if s.logger != nil {
			replicas := 1
			if spec.Scaling != nil && spec.Scaling.MinReplicas > 0 {
				replicas = spec.Scaling.MinReplicas
			}
			s.logger.Info("pod pool prewarmed", "pod", spec.Name, "replicas", replicas)
		}
	}

	return firstErr
}
