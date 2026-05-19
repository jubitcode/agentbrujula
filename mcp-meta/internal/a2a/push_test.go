package a2a

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPushNotifier_SetGetDeleteConfig(t *testing.T) {
	n := NewPushNotifier(DefaultPushNotifierConfig(), nil)

	// Set
	err := n.SetConfig("task-1", &PushConfig{URL: "http://example.com/hook"})
	if err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	// Get
	cfg := n.GetConfig("task-1")
	if cfg == nil || cfg.URL != "http://example.com/hook" {
		t.Errorf("GetConfig = %v", cfg)
	}

	// Get nonexistent
	if n.GetConfig("nonexistent") != nil {
		t.Error("expected nil for nonexistent config")
	}

	// Delete
	n.DeleteConfig("task-1")
	if n.GetConfig("task-1") != nil {
		t.Error("expected nil after delete")
	}
}

func TestPushNotifier_SetConfig_EmptyURL(t *testing.T) {
	n := NewPushNotifier(DefaultPushNotifierConfig(), nil)
	err := n.SetConfig("task-1", &PushConfig{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestPushNotifier_SetConfig_NilConfig(t *testing.T) {
	n := NewPushNotifier(DefaultPushNotifierConfig(), nil)
	err := n.SetConfig("task-1", nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

// setConfigDirect bypasses URL validation for test servers on localhost.
func setConfigDirect(n *PushNotifier, taskID string, cfg *PushConfig) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.configs[taskID] = cfg
}

func TestPushNotifier_Notify_Success(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		received = buf
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := NewPushNotifier(PushNotifierConfig{
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
		MaxDelay:   10 * time.Millisecond,
		Timeout:    5 * time.Second,
	}, nil)

	setConfigDirect(n, "task-1", &PushConfig{URL: srv.URL})

	task := &Task{
		ID:     "task-1",
		Status: TaskStatus{State: TaskStateCompleted},
	}

	err := n.Notify("task-1", task)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if len(received) == 0 {
		t.Fatal("webhook did not receive payload")
	}

	var got Task
	json.Unmarshal(received, &got)
	if got.ID != "task-1" {
		t.Errorf("received task ID = %q", got.ID)
	}
}

func TestPushNotifier_Notify_WithBearerToken(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := NewPushNotifier(PushNotifierConfig{
		MaxRetries: 0,
		Timeout:    5 * time.Second,
	}, nil)

	setConfigDirect(n, "task-1", &PushConfig{URL: srv.URL, Token: "my-secret-token"})
	n.Notify("task-1", &Task{ID: "task-1"})

	if authHeader != "Bearer my-secret-token" {
		t.Errorf("Authorization = %q, want 'Bearer my-secret-token'", authHeader)
	}
}

func TestPushNotifier_Notify_Retry_ThenSuccess(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := NewPushNotifier(PushNotifierConfig{
		MaxRetries: 3,
		BaseDelay:  time.Millisecond,
		MaxDelay:   5 * time.Millisecond,
		Timeout:    5 * time.Second,
	}, nil)

	setConfigDirect(n, "task-1", &PushConfig{URL: srv.URL})

	err := n.Notify("task-1", &Task{ID: "task-1"})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts (2 fails + 1 success), got %d", got)
	}
}

func TestPushNotifier_Notify_ExhaustedRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500) // always fail
	}))
	defer srv.Close()

	n := NewPushNotifier(PushNotifierConfig{
		MaxRetries: 2,
		BaseDelay:  time.Millisecond,
		MaxDelay:   5 * time.Millisecond,
		Timeout:    5 * time.Second,
	}, nil)

	setConfigDirect(n, "task-1", &PushConfig{URL: srv.URL})

	err := n.Notify("task-1", &Task{ID: "task-1"})
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
}

func TestPushNotifier_Notify_NoPushConfigured(t *testing.T) {
	n := NewPushNotifier(DefaultPushNotifierConfig(), nil)

	// No config set — Notify should be a no-op
	err := n.Notify("task-1", &Task{ID: "task-1"})
	if err != nil {
		t.Fatalf("expected nil error when no push configured, got: %v", err)
	}
}

// ============================================================
// V5: SSRF prevention — validateWebhookURL
// ============================================================

func TestValidateWebhookURL_RejectsLocalhost(t *testing.T) {
	err := validateWebhookURL("http://127.0.0.1:9876/health")
	if err == nil {
		t.Fatal("expected error for localhost URL")
	}
}

func TestValidateWebhookURL_RejectsMetadataService(t *testing.T) {
	err := validateWebhookURL("http://169.254.169.254/latest/")
	if err == nil {
		t.Fatal("expected error for metadata service URL")
	}
}

func TestValidateWebhookURL_RejectsPrivateIPs(t *testing.T) {
	cases := []string{
		"http://10.0.0.1:8080",
		"http://192.168.1.1",
		"http://172.16.0.1",
	}
	for _, u := range cases {
		if err := validateWebhookURL(u); err == nil {
			t.Errorf("expected error for private IP URL %s", u)
		}
	}
}

func TestValidateWebhookURL_AcceptsPublicURLs(t *testing.T) {
	err := validateWebhookURL("https://example.com/webhook")
	if err != nil {
		t.Fatalf("expected no error for public URL, got: %v", err)
	}
}

func TestValidateWebhookURL_RejectsNonHTTP(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"gopher://evil.com",
		"ftp://files.example.com/data",
	}
	for _, u := range cases {
		if err := validateWebhookURL(u); err == nil {
			t.Errorf("expected error for non-HTTP scheme URL %s", u)
		}
	}
}

func TestPushNotifier_SetConfig_RejectsSSRF(t *testing.T) {
	n := NewPushNotifier(DefaultPushNotifierConfig(), nil)
	err := n.SetConfig("task-1", &PushConfig{URL: "http://127.0.0.1:9876/health"})
	if err == nil {
		t.Fatal("expected error for SSRF URL via SetConfig")
	}
}

func TestPushNotifier_Notify_UnreachableURL(t *testing.T) {
	n := NewPushNotifier(PushNotifierConfig{
		MaxRetries: 0,
		Timeout:    100 * time.Millisecond,
	}, nil)

	// Bypass SetConfig validation to test network-level unreachability.
	setConfigDirect(n, "task-1", &PushConfig{URL: "http://127.0.0.1:1"}) // unreachable port

	err := n.Notify("task-1", &Task{ID: "task-1"})
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}
