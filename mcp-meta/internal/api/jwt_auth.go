package api

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// JWTAuthenticator validates JWT tokens against a Keycloak JWKS endpoint.
type JWTAuthenticator struct {
	jwksURL     string
	issuer      string
	audience    string
	mu          sync.RWMutex
	jwksCache   map[string]*rsa.PublicKey // kid → public key
	cacheExpiry time.Time
	cacheTTL    time.Duration
	httpClient  *http.Client
}

// NewJWTAuthenticator creates a JWT authenticator for Keycloak.
// keycloakURL: e.g., "http://localhost:8443"
// realm: e.g., "mcp-meta"
// clientID: used as audience validation
func NewJWTAuthenticator(keycloakURL, realm, clientID string) *JWTAuthenticator {
	a := &JWTAuthenticator{
		jwksURL:    fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", keycloakURL, realm),
		issuer:     fmt.Sprintf("%s/realms/%s", keycloakURL, realm),
		audience:   clientID,
		jwksCache:  make(map[string]*rsa.PublicKey),
		cacheTTL:   10 * time.Minute,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	// Fetch the actual issuer from OIDC discovery — Keycloak's internal hostname
	// may differ from the URL we use to reach it (e.g., Docker: keycloak:8180 vs localhost:8180).
	discoveryURL := fmt.Sprintf("%s/realms/%s/.well-known/openid-configuration", keycloakURL, realm)
	resp, err := a.httpClient.Get(discoveryURL)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var disc struct {
				Issuer string `json:"issuer"`
			}
			if json.NewDecoder(resp.Body).Decode(&disc) == nil && disc.Issuer != "" {
				a.issuer = disc.Issuer
			}
		}
	}

	return a
}

// Authenticate extracts and validates a JWT Bearer token from the request.
func (a *JWTAuthenticator) Authenticate(r *http.Request) (*Tenant, error) {
	// Extract Bearer token
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		// Also check X-API-Key for backward compat (API keys pass through)
		return nil, errors.New("missing Bearer token")
	}
	tokenStr := strings.TrimPrefix(auth, "Bearer ")
	if tokenStr == "" {
		return nil, errors.New("empty Bearer token")
	}

	// Parse JWT (header.payload.signature)
	claims, err := a.validateToken(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	// Map claims to Tenant
	tenant := &Tenant{
		ID:        claimString(claims, "sub"),
		Name:      claimString(claims, "preferred_username"),
		Tier:      "free", // default
		CreatedAt: time.Now(),
		Metadata:  make(map[string]string),
	}

	if tenant.Name == "" {
		tenant.Name = claimString(claims, "email")
	}

	// Extract tier from realm_access.roles
	if realmAccess, ok := claims["realm_access"].(map[string]any); ok {
		if roles, ok := realmAccess["roles"].([]any); ok {
			roleStrs := make([]string, 0, len(roles))
			for _, role := range roles {
				if s, ok := role.(string); ok {
					roleStrs = append(roleStrs, s)
					if s == "paid" || s == "pro" || s == "enterprise" {
						tenant.Tier = s
					}
				}
			}
			tenant.Metadata["roles"] = strings.Join(roleStrs, ",")
		}
	}

	// Extract groups
	if groups, ok := claims["groups"].([]any); ok {
		groupStrs := make([]string, 0, len(groups))
		for _, g := range groups {
			if s, ok := g.(string); ok {
				groupStrs = append(groupStrs, s)
			}
		}
		tenant.Metadata["groups"] = strings.Join(groupStrs, ",")
	}

	// Store email
	if email := claimString(claims, "email"); email != "" {
		tenant.Metadata["email"] = email
	}

	return tenant, nil
}

func (a *JWTAuthenticator) validateToken(tokenStr string) (map[string]any, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed JWT: expected 3 parts")
	}

	// Decode header
	headerBytes, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var header struct {
		Kid string `json:"kid"`
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported algorithm: %s", header.Alg)
	}

	// Get public key from JWKS
	pubKey, err := a.getPublicKey(header.Kid)
	if err != nil {
		return nil, fmt.Errorf("get public key: %w", err)
	}

	// Verify signature
	signedContent := parts[0] + "." + parts[1]
	signature, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	hash := sha256.Sum256([]byte(signedContent))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	// Decode payload
	payloadBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	// Validate expiration
	if exp, ok := claims["exp"].(float64); ok {
		if time.Unix(int64(exp), 0).Before(time.Now()) {
			return nil, errors.New("token expired")
		}
	}

	// Validate issuer
	if iss, ok := claims["iss"].(string); ok && a.issuer != "" {
		if iss != a.issuer {
			return nil, fmt.Errorf("invalid issuer: got %q, expected %q", iss, a.issuer)
		}
	}

	// Validate audience
	if a.audience != "" {
		switch aud := claims["aud"].(type) {
		case string:
			if aud != a.audience {
				return nil, fmt.Errorf("invalid audience: got %q, expected %q", aud, a.audience)
			}
		case []any:
			found := false
			for _, v := range aud {
				if s, ok := v.(string); ok && s == a.audience {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("audience %q not found in token", a.audience)
			}
		default:
			return nil, errors.New("missing or invalid audience claim")
		}
	}

	return claims, nil
}

func (a *JWTAuthenticator) getPublicKey(kid string) (*rsa.PublicKey, error) {
	a.mu.RLock()
	if key, ok := a.jwksCache[kid]; ok && time.Now().Before(a.cacheExpiry) {
		a.mu.RUnlock()
		return key, nil
	}
	a.mu.RUnlock()

	// Fetch JWKS
	if err := a.refreshJWKS(); err != nil {
		return nil, err
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	key, ok := a.jwksCache[kid]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", kid)
	}
	return key, nil
}

func (a *JWTAuthenticator) refreshJWKS() error {
	resp, err := a.httpClient.Get(a.jwksURL)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read JWKS: %w", err)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("parse JWKS: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.jwksCache = make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}

		nBytes, err := base64URLDecode(key.N)
		if err != nil {
			continue
		}
		eBytes, err := base64URLDecode(key.E)
		if err != nil {
			continue
		}

		n := new(big.Int).SetBytes(nBytes)
		e := int(new(big.Int).SetBytes(eBytes).Int64())

		a.jwksCache[key.Kid] = &rsa.PublicKey{N: n, E: e}
	}

	a.cacheExpiry = time.Now().Add(a.cacheTTL)
	return nil
}

func base64URLDecode(s string) ([]byte, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}

func claimString(claims map[string]any, key string) string {
	if v, ok := claims[key].(string); ok {
		return v
	}
	return ""
}

// CompositeAuthenticator tries multiple authenticators in order.
// First one that succeeds wins. Allows API keys + JWT to coexist.
type CompositeAuthenticator struct {
	authenticators []Authenticator
}

// NewCompositeAuthenticator creates an authenticator that tries each in order.
func NewCompositeAuthenticator(auths ...Authenticator) *CompositeAuthenticator {
	return &CompositeAuthenticator{authenticators: auths}
}

func (c *CompositeAuthenticator) Authenticate(r *http.Request) (*Tenant, error) {
	var lastErr error
	for _, auth := range c.authenticators {
		tenant, err := auth.Authenticate(r)
		if err == nil {
			return tenant, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
