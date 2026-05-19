package a2a

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestStore() *MemoryTaskStore {
	return NewMemoryTaskStore(TaskStoreConfig{
		TTL:             100 * time.Millisecond,
		CleanupInterval: 50 * time.Millisecond,
	})
}

func TestTaskStore_CreateAndGet(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	task := &Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status:    TaskStatus{State: TaskStateSubmitted},
	}

	if err := s.Create(task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get("task-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "task-1" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Status.State != TaskStateSubmitted {
		t.Errorf("State = %q", got.Status.State)
	}
	if got.Status.Timestamp == "" {
		t.Error("expected timestamp to be set")
	}
}

func TestTaskStore_Create_DuplicateID(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	task := &Task{ID: "dup", Status: TaskStatus{State: TaskStateSubmitted}}
	s.Create(task)

	err := s.Create(&Task{ID: "dup", Status: TaskStatus{State: TaskStateSubmitted}})
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestTaskStore_Create_EmptyID(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	err := s.Create(&Task{ID: "", Status: TaskStatus{State: TaskStateSubmitted}})
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestTaskStore_Get_NotFound(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	_, err := s.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestTaskStore_ValidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from TaskState
		to   TaskState
	}{
		{"submitted→working", TaskStateSubmitted, TaskStateWorking},
		{"submitted→failed", TaskStateSubmitted, TaskStateFailed},
		{"submitted→canceled", TaskStateSubmitted, TaskStateCanceled},
		{"working→completed", TaskStateWorking, TaskStateCompleted},
		{"working→failed", TaskStateWorking, TaskStateFailed},
		{"working→canceled", TaskStateWorking, TaskStateCanceled},
		{"working→input-required", TaskStateWorking, TaskStateInputRequired},
		{"input-required→working", TaskStateInputRequired, TaskStateWorking},
		{"input-required→failed", TaskStateInputRequired, TaskStateFailed},
		{"input-required→canceled", TaskStateInputRequired, TaskStateCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore()
			defer s.Stop()

			task := &Task{ID: "t", Status: TaskStatus{State: tt.from}}
			s.Create(task)

			err := s.Update("t", func(task *Task) error {
				task.Status.State = tt.to
				return nil
			})
			if err != nil {
				t.Fatalf("valid transition %s→%s failed: %v", tt.from, tt.to, err)
			}

			got, _ := s.Get("t")
			if got.Status.State != tt.to {
				t.Errorf("state = %q, want %q", got.Status.State, tt.to)
			}
		})
	}
}

func TestTaskStore_InvalidTransitions(t *testing.T) {
	tests := []struct {
		name string
		from TaskState
		to   TaskState
	}{
		{"submitted→completed", TaskStateSubmitted, TaskStateCompleted},
		{"submitted→input-required", TaskStateSubmitted, TaskStateInputRequired},
		{"completed→working", TaskStateCompleted, TaskStateWorking},
		{"completed→failed", TaskStateCompleted, TaskStateFailed},
		{"failed→working", TaskStateFailed, TaskStateWorking},
		{"canceled→working", TaskStateCanceled, TaskStateWorking},
		{"working→submitted", TaskStateWorking, TaskStateSubmitted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore()
			defer s.Stop()

			task := &Task{ID: "t", Status: TaskStatus{State: tt.from}}
			s.Create(task)

			err := s.Update("t", func(task *Task) error {
				task.Status.State = tt.to
				return nil
			})
			if err == nil {
				t.Fatalf("invalid transition %s→%s should have failed", tt.from, tt.to)
			}

			var transErr *TransitionError
			if !errors.As(err, &transErr) {
				t.Fatalf("expected TransitionError, got %T: %v", err, err)
			}
			if transErr.From != tt.from || transErr.To != tt.to {
				t.Errorf("TransitionError from=%s to=%s", transErr.From, transErr.To)
			}

			// State should be rolled back
			got, _ := s.Get("t")
			if got.Status.State != tt.from {
				t.Errorf("state not rolled back: got %q, want %q", got.Status.State, tt.from)
			}
		})
	}
}

func TestTaskStore_Update_NotFound(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	err := s.Update("nonexistent", func(task *Task) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestTaskStore_Update_FnError(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	s.Create(&Task{ID: "t", Status: TaskStatus{State: TaskStateSubmitted}})

	err := s.Update("t", func(task *Task) error {
		return errors.New("application error")
	})
	if err == nil || err.Error() != "application error" {
		t.Fatalf("expected application error, got: %v", err)
	}
}

func TestTaskStore_ListByContext(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	s.Create(&Task{ID: "t1", ContextID: "ctx-A", Status: TaskStatus{State: TaskStateSubmitted}})
	s.Create(&Task{ID: "t2", ContextID: "ctx-A", Status: TaskStatus{State: TaskStateWorking}})
	s.Create(&Task{ID: "t3", ContextID: "ctx-B", Status: TaskStatus{State: TaskStateSubmitted}})

	tasks, err := s.ListByContext("ctx-A")
	if err != nil {
		t.Fatalf("ListByContext: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks for ctx-A, got %d", len(tasks))
	}

	tasks, err = s.ListByContext("ctx-B")
	if err != nil {
		t.Fatalf("ListByContext: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task for ctx-B, got %d", len(tasks))
	}

	tasks, err = s.ListByContext("ctx-C")
	if err != nil {
		t.Fatalf("ListByContext: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for ctx-C, got %d", len(tasks))
	}
}

func TestTaskStore_Delete(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	s.Create(&Task{ID: "t1", Status: TaskStatus{State: TaskStateSubmitted}})

	if err := s.Delete("t1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Get("t1")
	if err == nil {
		t.Fatal("expected error after delete")
	}

	// Delete nonexistent
	if err := s.Delete("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent delete")
	}
}

func TestTaskStore_TTLEviction(t *testing.T) {
	s := NewMemoryTaskStore(TaskStoreConfig{
		TTL:             50 * time.Millisecond,
		CleanupInterval: 20 * time.Millisecond,
	})
	defer s.Stop()

	// Create and complete a task
	s.Create(&Task{ID: "t1", Status: TaskStatus{State: TaskStateSubmitted}})
	s.Update("t1", func(task *Task) error {
		task.Status.State = TaskStateWorking
		return nil
	})
	s.Update("t1", func(task *Task) error {
		task.Status.State = TaskStateCompleted
		return nil
	})

	// Create a non-terminal task
	s.Create(&Task{ID: "t2", Status: TaskStatus{State: TaskStateSubmitted}})

	// Wait for eviction
	time.Sleep(100 * time.Millisecond)

	// Completed task should be evicted
	_, err := s.Get("t1")
	if err == nil {
		t.Error("expected completed task to be evicted after TTL")
	}

	// Non-terminal task should still exist
	_, err = s.Get("t2")
	if err != nil {
		t.Error("non-terminal task should not be evicted")
	}
}

func TestTaskStore_ConcurrentAccess(t *testing.T) {
	s := newTestStore()
	defer s.Stop()

	s.Create(&Task{ID: "t1", Status: TaskStatus{State: TaskStateSubmitted}})

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	// Concurrent readers
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Get("t1")
			if err != nil {
				errs <- err
			}
		}()
	}

	// Concurrent writers (only one transition should succeed)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Update("t1", func(task *Task) error {
				if task.Status.State == TaskStateSubmitted {
					task.Status.State = TaskStateWorking
				}
				return nil
			})
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	got, _ := s.Get("t1")
	if got.Status.State != TaskStateWorking {
		t.Errorf("expected working state after concurrent updates, got %q", got.Status.State)
	}
}

func TestTaskState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    TaskState
		terminal bool
	}{
		{TaskStateSubmitted, false},
		{TaskStateWorking, false},
		{TaskStateInputRequired, false},
		{TaskStateCompleted, true},
		{TaskStateFailed, true},
		{TaskStateCanceled, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}
