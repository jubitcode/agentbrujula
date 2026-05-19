// Package api provides the HTTP API gateway for mcp-meta.
// It exposes REST endpoints for tool registry operations and tool execution.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"mcp-meta/internal/a2a"
	"mcp-meta/internal/correlation"
	"mcp-meta/internal/deploy"
	"mcp-meta/internal/mcp"
	"mcp-meta/internal/registry"
	"mcp-meta/internal/router"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server is the HTTP API gateway server.
type Server struct {
	registry   registry.Registry
	router     router.Router
	mcpHandler *mcp.Handler
	agentIndex  *a2a.AgentIndex
	a2aHandler  *a2a.Handler
	taskStore   a2a.TaskStore
	deployer    *deploy.Deployer
	logger     *slog.Logger
	httpServer *http.Server

	// Middleware components
	auth           Authenticator
	rateLimiter    RateLimiter
	allowedOrigins map[string]bool
	failTracker    *authFailTracker
}

// Config holds server configuration.
type Config struct {
	Host string
	Port int

	// Optional middleware
	Authenticator Authenticator
	RateLimiter   RateLimiter

	// Optional A2A support
	AgentIndex *a2a.AgentIndex
	A2AHandler *a2a.Handler
	TaskStore  a2a.TaskStore

	// Optional marketplace deploy support
	Deployer *deploy.Deployer

	// CORS allowed origins (e.g., "http://localhost:3002")
	AllowedOrigins []string
}

// NewServer creates a new API gateway server.
func NewServer(reg registry.Registry, rtr router.Router, mcpHandler *mcp.Handler, logger *slog.Logger, cfg Config) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	origins := make(map[string]bool)
	for _, o := range cfg.AllowedOrigins {
		origins[o] = true
	}

	s := &Server{
		registry:       reg,
		router:         rtr,
		mcpHandler:     mcpHandler,
		agentIndex:     cfg.AgentIndex,
		a2aHandler:     cfg.A2AHandler,
		taskStore:      cfg.TaskStore,
		deployer:       cfg.Deployer,
		logger:         logger.With("component", "api-server"),
		auth:           cfg.Authenticator,
		rateLimiter:    cfg.RateLimiter,
		allowedOrigins: origins,
		failTracker:    newAuthFailTracker(5, 15*time.Minute),
	}

	// Wire cost tracker to router if task store supports it
	if ct, ok := cfg.TaskStore.(router.CostTracker); ok {
		rtr.SetCostTracker(ct)
	}

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      s.applyMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      600 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s
}

// Start starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("starting API server", "addr", s.httpServer.Addr)

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down API server")
	return s.httpServer.Shutdown(ctx)
}

// setupRoutes configures all HTTP routes.
func (s *Server) setupRoutes(mux *http.ServeMux) {
	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)

	// API v1 routes
	mux.HandleFunc("GET /v1/tools", s.handleListTools)
	mux.HandleFunc("POST /v1/tools", s.handlePublishTool)
	mux.HandleFunc("GET /v1/tools/{namespace}/{name}", s.handleGetTool)
	mux.HandleFunc("GET /v1/tools/{namespace}/{name}/versions", s.handleGetToolVersions)

	// Execution
	mux.HandleFunc("POST /v1/execute", s.handleExecute)
	mux.HandleFunc("POST /v1/tools/{namespace}/{name}/call", s.handleCallTool)

	// MCP protocol (SSE-based)
	mux.HandleFunc("GET /v1/mcp", s.handleMCPSSE)
	mux.HandleFunc("POST /v1/mcp", s.handleMCPMessage)

	// Warm pool status (admin)
	mux.HandleFunc("GET /v1/admin/pools", s.handlePoolStatus)

	// Prometheus metrics
	mux.Handle("GET /metrics", promhttp.Handler())

	// A2A endpoints
	mux.HandleFunc("GET /.well-known/agent-card.json", s.handleAgentCard)
	mux.HandleFunc("POST /a2a", s.handleA2AJSONRPC)
	mux.HandleFunc("GET /v1/tasks", s.handleListTasks)
	mux.HandleFunc("GET /v1/cost/{contextID}", s.handleGetCost)
	mux.HandleFunc("POST /a2a/stream", s.handleA2AStream)

	// Marketplace endpoints
	mux.HandleFunc("POST /v1/marketplace/deploy", s.handleDeploy)
	mux.HandleFunc("GET /v1/marketplace/agents", s.handleListDeployedAgents)
	mux.HandleFunc("GET /v1/marketplace/agents/{name}", s.handleGetDeployedAgent)
	mux.HandleFunc("DELETE /v1/marketplace/agents/{name}", s.handleUndeployAgent)

	// CORS preflight handlers (OPTIONS requests)
	mux.HandleFunc("OPTIONS /v1/tools", s.handleOptions)
	mux.HandleFunc("OPTIONS /v1/tools/{namespace}/{name}", s.handleOptions)
	mux.HandleFunc("OPTIONS /v1/tools/{namespace}/{name}/versions", s.handleOptions)
	mux.HandleFunc("OPTIONS /v1/tools/{namespace}/{name}/call", s.handleOptions)
	mux.HandleFunc("OPTIONS /v1/execute", s.handleOptions)
	mux.HandleFunc("OPTIONS /v1/mcp", s.handleOptions)
	mux.HandleFunc("OPTIONS /v1/admin/pools", s.handleOptions)
}

// handleOptions handles CORS preflight requests.
func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	// CORS headers are set by middleware, just return 204
	w.WriteHeader(http.StatusNoContent)
}

// applyMiddleware wraps the handler with middleware.
func (s *Server) applyMiddleware(handler http.Handler) http.Handler {
	// Apply middleware in reverse order (last applied runs first)
	h := handler

	// Logging middleware
	h = s.loggingMiddleware(h)

	// Authentication — always applied, never optional (P1-8).
	// If no authenticator is configured, generate an ephemeral API key
	// so the server never runs completely unauthenticated.
	if s.auth == nil {
		randomKey := generateEphemeralKey()
		s.logger.Warn("no auth configured — generated ephemeral API key", "key", randomKey)
		fmt.Fprintf(os.Stderr, "EPHEMERAL_API_KEY=%s\n", randomKey)
		ephAuth := NewAPIKeyAuthenticator("X-API-Key")
		ephAuth.RegisterKey(randomKey, &Tenant{ID: "ephemeral", Name: "Ephemeral", Tier: "paid"})
		s.auth = ephAuth
	}

	// Scope enforcement (runs after auth, before rate-limit)
	h = s.scopeMiddleware(h)

	// Correlation ID middleware
	h = s.correlationMiddleware(h)

	// Rate limiting (if configured)
	if s.rateLimiter != nil {
		h = s.rateLimitMiddleware(h)
	}

	h = s.authMiddleware(h)

	// CORS middleware (before auth so preflight works)
	h = s.corsMiddleware(h)

	// Recovery middleware (always first/outermost)
	h = s.recoveryMiddleware(h)

	return h
}

// corsMiddleware handles CORS headers for browser requests.
// Only whitelisted origins receive CORS headers; unknown origins are blocked by the browser.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs HTTP requests.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		correlation.ContextLogger(r.Context(), s.logger).Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", time.Since(start),
			"remote", r.RemoteAddr,
		)
	})
}

// recoveryMiddleware recovers from panics.
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.logger.Error("panic recovered", "error", err, "path", r.URL.Path)
				s.writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// correlationMiddleware assigns a unique request ID to each request.
func (s *Server) correlationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = correlation.NewID()
		}
		ctx := correlation.WithID(r.Context(), id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// skipMiddleware returns true for paths that should bypass auth/rate-limit.
func skipMiddleware(path string) bool {
	return path == "/health" || path == "/ready" || path == "/.well-known/agent-card.json"
}

// paidOnlyPaths require a paid tier key.
func isPaidOnlyPath(path string) bool {
	return strings.HasPrefix(path, "/v1/marketplace/deploy")
}

// scopeMiddleware enforces tier-based access control.
func (s *Server) scopeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipMiddleware(r.URL.Path) || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		if isPaidOnlyPath(r.URL.Path) {
			tenant := GetTenant(r.Context())
			if tenant != nil && tenant.Tier == "free" {
				s.writeError(w, http.StatusForbidden, "deploy requires a paid API key")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// authMiddleware handles authentication.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipMiddleware(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// /metrics: allow from localhost only, no full auth required
		if r.URL.Path == "/metrics" {
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			if host == "127.0.0.1" || host == "::1" {
				next.ServeHTTP(w, r)
				return
			}
			s.writeError(w, http.StatusForbidden, "metrics only available from localhost")
			return
		}

		// Check if IP is blocked from too many auth failures
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if s.failTracker != nil && s.failTracker.isBlocked(ip) {
			w.Header().Set("Retry-After", "900")
			s.writeError(w, http.StatusTooManyRequests, "too many failed auth attempts")
			return
		}

		tenant, err := s.auth.Authenticate(r)
		if err != nil {
			if s.failTracker != nil {
				s.failTracker.recordFailure(ip)
			}
			s.writeError(w, http.StatusUnauthorized, "unauthorized: "+err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), tenantContextKey{}, tenant)

		// Extract and sanitize user-provided LLM key (BYOK)
		if llmKey := r.Header.Get("X-LLM-Key"); llmKey != "" {
			sanitized := strings.Map(func(r rune) rune {
				if r < 32 || r == 127 {
					return -1
				}
				return r
			}, llmKey)
			if sanitized != "" {
				ctx = WithLLMKey(ctx, sanitized)
			}
		}

		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rateLimitMiddleware handles rate limiting.
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limit for health checks and metrics
		if skipMiddleware(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		tenant := GetTenant(r.Context())
		if tenant == nil {
			tenant = &Tenant{ID: "anonymous"}
		}

		allowed, retryAfter := s.rateLimiter.Allow(tenant.ID, r.URL.Path)
		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
			s.writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying ResponseWriter if it supports flushing (needed for SSE).
func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode response", "error", err)
	}
}

// writeError writes a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

// parseJSON parses JSON request body.
func (s *Server) parseJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is empty")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// generateEphemeralKey creates a cryptographically random API key for use
// when no authenticator is explicitly configured.
func generateEphemeralKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "eph-" + hex.EncodeToString(b)
}

// extractPathParams extracts path parameters from the URL.
// For pattern "/v1/tools/{namespace}/{name}", returns namespace and name.
func extractPathParams(r *http.Request) (namespace, name string) {
	// Go 1.22+ uses PathValue for pattern matching
	namespace = r.PathValue("namespace")
	name = r.PathValue("name")

	// Fallback for older patterns - parse from URL
	if namespace == "" || name == "" {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/tools/"), "/")
		if len(parts) >= 2 {
			namespace = parts[0]
			name = parts[1]
		}
	}
	return
}
