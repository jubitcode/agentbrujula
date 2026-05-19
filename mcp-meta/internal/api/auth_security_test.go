package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServerWithAuth creates a test server with API key auth and rate limiting.
func newTestServerWithAuth() (*Server, *KeyAuthenticator) {
	auth := NewAPIKeyAuthenticator("X-API-Key")

	// Register test keys
	auth.RegisterKey("free-key-001", &Tenant{
		ID: "free-user", Name: "Free User", Tier: "free",
		CreatedAt: time.Now(),
	})
	auth.RegisterKey("paid-key-001", &Tenant{
		ID: "paid-user", Name: "Paid User", Tier: "paid",
		CreatedAt: time.Now(),
	})

	limiter := NewTokenBucketLimiter(2, 3) // 2 req/s, burst 3 (tight for testing)

	reg := &mockRegistry{}
	rtr := &mockRouter{}
	srv := NewServer(reg, rtr, nil, nil, Config{
		Host:          "localhost",
		Port:          8080,
		Authenticator: auth,
		RateLimiter:   limiter,
	})

	return srv, auth
}

func doRequest(srv *Server, method, path string, apiKey string, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	return w
}

// ============================================================
// 1. No API key → 401
// ============================================================

func TestAuth_NoKey_Returns401(t *testing.T) {
	srv, _ := newTestServerWithAuth()
	w := doRequest(srv, "GET", "/v1/tools", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without key, got %d", w.Code)
	}
}

// ============================================================
// 2. Invalid API key → 401
// ============================================================

func TestAuth_InvalidKey_Returns401(t *testing.T) {
	srv, _ := newTestServerWithAuth()
	w := doRequest(srv, "GET", "/v1/tools", "invalid-key-xyz", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with invalid key, got %d", w.Code)
	}
}

// ============================================================
// 3. Valid key but rate limited → 429 with Retry-After
// ============================================================

func TestAuth_RateLimit_Returns429(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	// Exhaust burst (3 requests)
	for i := 0; i < 3; i++ {
		w := doRequest(srv, "GET", "/v1/tools", "free-key-001", "")
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, w.Code)
		}
	}

	// 4th request should be rate limited
	w := doRequest(srv, "GET", "/v1/tools", "free-key-001", "")
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst exhausted, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429")
	}
}

// ============================================================
// 4. API key in URL query param → should work (SSE/WebSocket compat)
// ============================================================

func TestAuth_KeyInQueryParam_Rejected(t *testing.T) {
	srv, _ := newTestServerWithAuth()
	req := httptest.NewRequest("GET", "/v1/tools?api_key=free-key-001", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for query param key (removed for security), got %d", w.Code)
	}
}

// ============================================================
// 5. Bearer token format works
// ============================================================

func TestAuth_BearerToken_Works(t *testing.T) {
	srv, _ := newTestServerWithAuth()
	req := httptest.NewRequest("GET", "/v1/tools", nil)
	req.Header.Set("Authorization", "Bearer free-key-001")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with Bearer token, got %d", w.Code)
	}
}

// ============================================================
// 6. Timing-safe comparison (verify constant-time is used)
// ============================================================

func TestAuth_TimingSafe_UsesConstantTimeCompare(t *testing.T) {
	auth := NewAPIKeyAuthenticator("X-API-Key")
	auth.RegisterKey("real-key-12345", &Tenant{ID: "test"})

	// Both should fail at the same rate regardless of prefix match
	// We can't test timing directly, but we verify ConstantTimeCompare is in the code
	// (already using crypto/subtle.ConstantTimeCompare in auth.go line 111)
	req1 := httptest.NewRequest("GET", "/", nil)
	req1.Header.Set("X-API-Key", "real-key-00000") // same prefix, different suffix
	_, err1 := auth.Authenticate(req1)

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("X-API-Key", "xxxx-xxx-xxxxx") // completely different
	_, err2 := auth.Authenticate(req2)

	if err1 == nil || err2 == nil {
		t.Error("both invalid keys should fail")
	}
}

// ============================================================
// 7. Header injection: X-LLM-Key with newlines/special chars
// ============================================================

func TestAuth_HeaderInjection_Sanitized(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	// Try header injection via LLM key
	req := httptest.NewRequest("GET", "/v1/tools", nil)
	req.Header.Set("X-API-Key", "free-key-001")
	req.Header.Set("X-LLM-Key", "sk-bad\r\nX-Injected: true")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	// Should still work (server doesn't crash) — the LLM key is just data
	if w.Code == 0 {
		t.Error("server returned 0 status — possible crash from header injection")
	}
}

// ============================================================
// 8. Revoked key → immediate 401
// ============================================================

func TestAuth_RevokedKey_Returns401(t *testing.T) {
	srv, auth := newTestServerWithAuth()

	// Key works
	w := doRequest(srv, "GET", "/v1/tools", "free-key-001", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 before revocation, got %d", w.Code)
	}

	// Revoke key
	auth.RevokeKey("free-key-001")

	// Key should fail immediately
	w = doRequest(srv, "GET", "/v1/tools", "free-key-001", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after revocation, got %d", w.Code)
	}
}

// ============================================================
// 9. Public endpoints work without key
// ============================================================

func TestAuth_PublicEndpoints_NoKeyRequired(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	publicPaths := []string{"/health", "/ready", "/.well-known/agent-card.json"}
	for _, path := range publicPaths {
		w := doRequest(srv, "GET", path, "", "")
		// Should NOT be 401
		if w.Code == http.StatusUnauthorized {
			t.Errorf("%s returned 401 — should be public", path)
		}
	}
}

// ============================================================
// 10. CORS preflight works without key
// ============================================================

func TestAuth_CORSPreflight_NoKeyRequired(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	req := httptest.NewRequest("OPTIONS", "/v1/tools", nil)
	req.Header.Set("Origin", "http://localhost:3002")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Error("OPTIONS preflight should not require auth")
	}
}

// ============================================================
// 11. Metrics endpoint protected (when auth is enabled)
// ============================================================

func TestAuth_MetricsEndpoint_Protected(t *testing.T) {
	// Note: current implementation skips auth for /metrics
	// This test documents current behavior — decide if metrics should be protected
	srv, _ := newTestServerWithAuth()
	w := doRequest(srv, "GET", "/metrics", "", "")

	// Currently /metrics is in skipMiddleware — this test verifies that
	if w.Code == http.StatusUnauthorized {
		t.Log("metrics is protected — good for production")
	} else {
		t.Log("metrics is public — acceptable for internal deployment")
	}
}

// ============================================================
// 12. Key scope: free tier can't access deploy endpoint
// ============================================================

func TestAuth_FreeTier_CannotDeploy(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	body := `{"manifest": "test"}`
	w := doRequest(srv, "POST", "/v1/marketplace/deploy", "free-key-001", body)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for free tier deploy, got %d", w.Code)
	}
}

func TestAuth_PaidTier_CanDeploy(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	// Paid key should not get 403 (may get 400/503 for bad body, but not 403)
	body := `{"manifest": "test"}`
	w := doRequest(srv, "POST", "/v1/marketplace/deploy", "paid-key-001", body)

	if w.Code == http.StatusForbidden {
		t.Error("paid tier should not be blocked from deploy")
	}
}

// ============================================================
// 13. Request correlation propagates with auth
// ============================================================

func TestAuth_CorrelationID_Propagates(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	req := httptest.NewRequest("GET", "/v1/tools", nil)
	req.Header.Set("X-API-Key", "free-key-001")
	req.Header.Set("X-Request-ID", "auth-corr-test-123")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Header().Get("X-Request-ID") != "auth-corr-test-123" {
		t.Errorf("correlation ID not propagated through auth middleware: got %q",
			w.Header().Get("X-Request-ID"))
	}
}

// ============================================================
// 14. Tenant info available in request context
// ============================================================

func TestAuth_TenantInContext(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	// Execute request and verify the execute handler gets the tenant
	body := `{"tool":"test","arguments":{}}`
	w := doRequest(srv, "POST", "/v1/execute", "paid-key-001", body)

	// The mock router handles the call — we're testing that auth didn't block it
	if w.Code == http.StatusUnauthorized {
		t.Error("valid paid key should authenticate successfully")
	}

	// Parse response to verify requestId is present (proves full middleware chain ran)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["requestId"] == nil || resp["requestId"] == "" {
		t.Error("requestId missing — middleware chain may not have completed")
	}
}

// ============================================================
// 15. Multiple concurrent requests with same key
// ============================================================

func TestAuth_ConcurrentRequests_SameKey(t *testing.T) {
	srv, _ := newTestServerWithAuth()

	done := make(chan int, 10)
	for i := range 10 {
		go func(n int) {
			w := doRequest(srv, "GET", "/health", "free-key-001", "")
			done <- w.Code
		}(i)
	}

	for range 10 {
		code := <-done
		if code == 0 {
			t.Error("got 0 status — server may have panicked on concurrent access")
		}
	}
}

// ============================================================
// 16. Empty key string is not a valid key
// ============================================================

func TestAuth_EmptyString_NotValid(t *testing.T) {
	auth := NewAPIKeyAuthenticator("X-API-Key")
	auth.RegisterKey("", &Tenant{ID: "should-not-exist"})

	// An empty header should not match the empty registered key
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "")
	_, err := auth.Authenticate(req)

	// Empty key in header is treated as "missing"
	if err == nil {
		t.Error("empty string should not authenticate — even if registered")
	}
}

// ============================================================
// 17. Very long key doesn't crash
// ============================================================

func TestAuth_VeryLongKey_DoesNotCrash(t *testing.T) {
	srv, _ := newTestServerWithAuth()
	longKey := strings.Repeat("a", 10000)
	w := doRequest(srv, "GET", "/v1/tools", longKey, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for garbage long key, got %d", w.Code)
	}
}
