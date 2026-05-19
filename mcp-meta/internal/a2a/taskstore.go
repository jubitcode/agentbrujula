package a2a

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// TransitionError is returned when an invalid state transition is attempted.
type TransitionError struct {
	TaskID string
	From   TaskState
	To     TaskState
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("task store: invalid transition %s→%s for task %s", e.From, e.To, e.TaskID)
}

// validTransitions defines the allowed state transitions per the A2A spec.
var validTransitions = map[TaskState][]TaskState{
	TaskStateSubmitted:     {TaskStateWorking, TaskStateFailed, TaskStateCanceled},
	TaskStateWorking:       {TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateInputRequired},
	TaskStateInputRequired: {TaskStateWorking, TaskStateFailed, TaskStateCanceled},
}

func isValidTransition(from, to TaskState) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false // terminal states have no outgoing transitions
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// TaskStoreConfig configures the in-memory task store.
type TaskStoreConfig struct {
	TTL             time.Duration // TTL for terminal tasks (default: 1h)
	CleanupInterval time.Duration // How often to run eviction (default: 5m)
}

// DefaultTaskStoreConfig returns sensible defaults.
func DefaultTaskStoreConfig() TaskStoreConfig {
	return TaskStoreConfig{
		TTL:             1 * time.Hour,
		CleanupInterval: 5 * time.Minute,
	}
}

// TaskStore manages A2A task lifecycle.
type TaskStore interface {
	Create(task *Task) error
	Get(taskID string) (*Task, error)
	Update(taskID string, fn func(*Task) error) error
	ListByContext(contextID string) ([]*Task, error)
	ListRecent(limit int) ([]*Task, error)
	Delete(taskID string) error
}

// CostEntry Cost Ledger — tracks per-tool costs by contextID for budget billing
// CostEntry records the cost of a single tool call.
type CostEntry struct {
	ToolFQN     string    `json:"tool"`
	PodName     string    `json:"pod"`
	CostStroops int64     `json:"cost_stroops"`
	Timestamp   time.Time `json:"timestamp"`
}

// CostLedger accumulates costs for all tool calls under a contextID.
type CostLedger struct {
	ContextID    string      `json:"context_id"`
	Entries      []CostEntry `json:"entries"`
	TotalStroops int64       `json:"total_stroops"`
}

// ByAgent returns a map of pod name → total cost in stroops.
func (l *CostLedger) ByAgent() map[string]int64 {
	m := make(map[string]int64)
	for _, e := range l.Entries {
		m[e.PodName] += e.CostStroops
	}
	return m
}

// CostReader provides read access to cost ledgers (used by API handlers).
type CostReader interface {
	GetCost(contextID string) *CostLedger
}

// TenantReader provides read access to context-tenant mappings (for cost isolation).
type TenantReader interface {
	GetContextTenant(contextID string) string
}

type taskEntry struct {
	task        *Task
	completedAt time.Time // set when task reaches terminal state
}

// MemoryTaskStore is an in-memory TaskStore with state transition validation.
type MemoryTaskStore struct {
	mu      sync.RWMutex
	tasks   map[string]*taskEntry
	costs   map[string]*CostLedger // contextID → cost ledger
	tenants map[string]string      // contextID → tenantID (for cost isolation)
	config  TaskStoreConfig
	stopCh  chan struct{}
}

// NewMemoryTaskStore creates an in-memory task store with TTL eviction.
func NewMemoryTaskStore(cfg TaskStoreConfig) *MemoryTaskStore {
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTaskStoreConfig().TTL
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = DefaultTaskStoreConfig().CleanupInterval
	}

	s := &MemoryTaskStore{
		tasks:   make(map[string]*taskEntry),
		costs:   make(map[string]*CostLedger),
		tenants: make(map[string]string),
		config:  cfg,
		stopCh:  make(chan struct{}),
	}

	go s.evictionLoop()
	return s
}

// Stop stops the background eviction goroutine.
func (s *MemoryTaskStore) Stop() {
	close(s.stopCh)
}

func (s *MemoryTaskStore) Create(task *Task) error {
	if task.ID == "" {
		return fmt.Errorf("task store: task id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[task.ID]; exists {
		return fmt.Errorf("task store: task %s already exists", task.ID)
	}

	task.Status.Timestamp = time.Now().UTC().Format(time.RFC3339)
	s.tasks[task.ID] = &taskEntry{task: task}
	return nil
}

func (s *MemoryTaskStore) Get(taskID string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task store: task %s not found", taskID)
	}
	return entry.task, nil
}

// Update atomically updates a task. The function fn receives the task and can
// modify it. State transitions are validated before the update is applied.
func (s *MemoryTaskStore) Update(taskID string, fn func(*Task) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task store: task %s not found", taskID)
	}

	oldState := entry.task.Status.State

	if err := fn(entry.task); err != nil {
		return err
	}

	newState := entry.task.Status.State
	if newState != oldState {
		if !isValidTransition(oldState, newState) {
			// Roll back
			entry.task.Status.State = oldState
			return &TransitionError{TaskID: taskID, From: oldState, To: newState}
		}
	}

	entry.task.Status.Timestamp = time.Now().UTC().Format(time.RFC3339)

	if newState.IsTerminal() && entry.completedAt.IsZero() {
		entry.completedAt = time.Now()
	}

	return nil
}

func (s *MemoryTaskStore) ListByContext(contextID string) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Task
	for _, entry := range s.tasks {
		if entry.task.ContextID == contextID {
			result = append(result, entry.task)
		}
	}
	return result, nil
}

func (s *MemoryTaskStore) ListRecent(limit int) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*Task, 0, len(s.tasks))
	for _, entry := range s.tasks {
		tasks = append(tasks, entry.task)
	}

	// Sort by timestamp descending (newest first)
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Status.Timestamp > tasks[j].Status.Timestamp
	})

	if limit > 0 && len(tasks) > limit {
		tasks = tasks[:limit]
	}

	return tasks, nil
}

func (s *MemoryTaskStore) Delete(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[taskID]; !ok {
		return fmt.Errorf("task store: task %s not found", taskID)
	}
	delete(s.tasks, taskID)
	return nil
}

// RecordCost CostTracker implementation
// RecordCost appends a cost entry for a tool call under the given contextID.
func (s *MemoryTaskStore) RecordCost(contextID, toolFQN, podName string, costStroops int64) {
	if contextID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ledger, ok := s.costs[contextID]
	if !ok {
		ledger = &CostLedger{ContextID: contextID}
		s.costs[contextID] = ledger
	}

	ledger.Entries = append(ledger.Entries, CostEntry{
		ToolFQN:     toolFQN,
		PodName:     podName,
		CostStroops: costStroops,
		Timestamp:   time.Now(),
	})
	ledger.TotalStroops += costStroops
}

// GetCost returns the cost ledger for a contextID, or nil if not found.
func (s *MemoryTaskStore) GetCost(contextID string) *CostLedger {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.costs[contextID]
}

// SetContextTenant records which tenant owns a given contextID.
func (s *MemoryTaskStore) SetContextTenant(contextID, tenantID string) {
	if contextID == "" || tenantID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[contextID] = tenantID
}

// GetContextTenant returns the tenant ID that owns a contextID, or "" if unknown.
func (s *MemoryTaskStore) GetContextTenant(contextID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tenants[contextID]
}

func (s *MemoryTaskStore) evictionLoop() {
	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.evict()
		}
	}
}

func (s *MemoryTaskStore) evict() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, entry := range s.tasks {
		if !entry.completedAt.IsZero() && now.Sub(entry.completedAt) > s.config.TTL {
			delete(s.tasks, id)
		}
	}
}
