package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Tenant represents an authenticated API user.
type Tenant struct {
	ID        string
	Name      string
	Tier      string            // "free", "pro", "enterprise"
	Quotas    map[string]int64  // e.g., "calls_per_month": 1000
	Metadata  map[string]string // Additional tenant data
	CreatedAt time.Time
}

// tenantContextKey is the context key for tenant.
type tenantContextKey struct{}

// llmKeyContextKey is the context key for user-provided LLM API key.
type llmKeyContextKey struct{}

// WithLLMKey returns a context with the user-provided LLM key.
func WithLLMKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, llmKeyContextKey{}, key)
}

// GetTenant retrieves the tenant from context.
func GetTenant(ctx context.Context) *Tenant {
	if tenant, ok := ctx.Value(tenantContextKey{}).(*Tenant); ok {
		return tenant
	}
	return nil
}

// Authenticator authenticates HTTP requests.
type Authenticator interface {
	// Authenticate extracts and validates credentials from the request.
	// Returns the authenticated tenant or an error.
	Authenticate(r *http.Request) (*Tenant, error)
}

// NoOpAuthenticator allows all requests (for development).
type NoOpAuthenticator struct{}

func (NoOpAuthenticator) Authenticate(*http.Request) (*Tenant, error) {
	return &Tenant{
		ID:   "anonymous",
		Name: "Anonymous",
		Tier: "free",
	}, nil
}

// KeyAuthenticator APIKeyAuthenticator authenticates using API keys.
type KeyAuthenticator struct {
	mu     sync.RWMutex
	keys   map[string]*Tenant // API key -> tenant
	header string             // Header name for API key
}

// NewAPIKeyAuthenticator creates a new API key authenticator.
func NewAPIKeyAuthenticator(header string) *KeyAuthenticator {
	if header == "" {
		header = "X-API-Key"
	}
	return &KeyAuthenticator{
		keys:   make(map[string]*Tenant),
		header: header,
	}
}

// RegisterKey registers an API key for a tenant.
func (a *KeyAuthenticator) RegisterKey(apiKey string, tenant *Tenant) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.keys[apiKey] = tenant
}

// GetTenantByID finds a tenant by ID across all registered keys.
func (a *KeyAuthenticator) GetTenantByID(tenantID string) *Tenant {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tenant := range a.keys {
		if tenant.ID == tenantID {
			return tenant
		}
	}
	return nil
}

// RevokeKey removes an API key.
func (a *KeyAuthenticator) RevokeKey(apiKey string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.keys, apiKey)
}

// Authenticate validates the API key from the request.
func (a *KeyAuthenticator) Authenticate(r *http.Request) (*Tenant, error) {
	// Check header first
	apiKey := r.Header.Get(a.header)

	// Fallback to Authorization header (Bearer token)
	if apiKey == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
		}
	}

	if apiKey == "" {
		return nil, errors.New("missing API key")
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	// Constant-time comparison to prevent timing attacks
	for key, tenant := range a.keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(apiKey)) == 1 {
			return tenant, nil
		}
	}

	return nil, errors.New("invalid API key")
}

// RateLimiter controls request rates.
type RateLimiter interface {
	// Allow checks if a request should be allowed.
	// Returns true if allowed, or false with a retry-after duration.
	Allow(tenantID string, path string) (allowed bool, retryAfter time.Duration)
}

// NoOpRateLimiter allows all requests.
type NoOpRateLimiter struct{}

func (NoOpRateLimiter) Allow(string, string) (bool, time.Duration) {
	return true, 0
}

// TokenBucketLimiter implements a token bucket rate limiter.
type TokenBucketLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64       // tokens per second
	burst   int           // max tokens
	cleanup time.Duration // cleanup interval for old buckets
}

type bucket struct {
	tokens     float64
	lastUpdate time.Time
}

// NewTokenBucketLimiter creates a new token bucket rate limiter.
func NewTokenBucketLimiter(rate float64, burst int) *TokenBucketLimiter {
	l := &TokenBucketLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		cleanup: 10 * time.Minute,
	}
	go l.cleanupLoop()
	return l
}

// Allow checks if a request should be allowed.
func (l *TokenBucketLimiter) Allow(tenantID, path string) (bool, time.Duration) {
	key := tenantID + ":" + path
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{
			tokens:     float64(l.burst),
			lastUpdate: time.Now(),
		}
		l.buckets[key] = b
	}

	now := time.Now()
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.tokens = min(float64(l.burst), b.tokens+elapsed*l.rate)
	b.lastUpdate = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// Calculate retry-after
	retryAfter := time.Duration((1-b.tokens)/l.rate*1000) * time.Millisecond
	return false, retryAfter
}

// cleanupLoop removes stale buckets.
func (l *TokenBucketLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanup)
	for range ticker.C {
		l.mu.Lock()
		threshold := time.Now().Add(-l.cleanup)
		for key, b := range l.buckets {
			if b.lastUpdate.Before(threshold) {
				delete(l.buckets, key)
			}
		}
		l.mu.Unlock()
	}
}

// TieredRateLimiter applies different limits based on tenant tier.
type TieredRateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*TokenBucketLimiter // tier -> limiter
	getTier  func(tenantID string) string   // function to get tenant tier
	defaults *TokenBucketLimiter
}

// TierConfig defines rate limits for a tier.
type TierConfig struct {
	Rate  float64 // requests per second
	Burst int     // burst capacity
}

// NewTieredRateLimiter creates a tiered rate limiter.
func NewTieredRateLimiter(tiers map[string]TierConfig, getTier func(string) string) *TieredRateLimiter {
	l := &TieredRateLimiter{
		limiters: make(map[string]*TokenBucketLimiter),
		getTier:  getTier,
		defaults: NewTokenBucketLimiter(10, 20), // 10 req/s, burst 20
	}

	for tier, cfg := range tiers {
		l.limiters[tier] = NewTokenBucketLimiter(cfg.Rate, cfg.Burst)
	}

	return l
}

// Allow checks if a request should be allowed based on tenant tier.
func (l *TieredRateLimiter) Allow(tenantID, path string) (bool, time.Duration) {
	tier := "free"
	if l.getTier != nil {
		tier = l.getTier(tenantID)
	}

	l.mu.RLock()
	limiter, ok := l.limiters[tier]
	l.mu.RUnlock()

	if !ok {
		limiter = l.defaults
	}

	return limiter.Allow(tenantID, path)
}
