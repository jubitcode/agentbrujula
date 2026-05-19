package api

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func generateTestRSAKey() *rsa.PrivateKey {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	return key
}

func signJWT(key *rsa.PrivateKey, kid string, claims map[string]any) string {
	header := base64URLEncode(mustJSON(map[string]string{"kid": kid, "alg": "RS256", "typ": "JWT"}))
	payload := base64URLEncode(mustJSON(claims))
	signed := header + "." + payload

	hash := sha256.Sum256([]byte(signed))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])

	return signed + "." + base64URLEncode(sig)
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func startMockJWKS(key *rsa.PrivateKey, kid string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}) // 65537

		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{
				{"kid": kid, "kty": "RSA", "alg": "RS256", "n": n, "e": e},
			},
		})
	}))
}

func TestJWT_ValidToken(t *testing.T) {
	key := generateTestRSAKey()
	kid := "test-key-1"
	jwks := startMockJWKS(key, kid)
	defer jwks.Close()

	// JWKS URL is at /realms/test/protocol/openid-connect/certs
	// But our mock serves at root, so construct the authenticator manually
	auth := &JWTAuthenticator{
		jwksURL:    jwks.URL,
		issuer:     "http://keycloak.test/realms/test",
		audience:   "mcp-meta",
		jwksCache:  make(map[string]*rsa.PublicKey),
		cacheTTL:   10 * time.Minute,
		httpClient: jwks.Client(),
	}

	token := signJWT(key, kid, map[string]any{
		"sub":                "user-123",
		"preferred_username": "testuser",
		"email":              "test@example.com",
		"iss":                "http://keycloak.test/realms/test",
		"aud":                "mcp-meta",
		"exp":                float64(time.Now().Add(1 * time.Hour).Unix()),
		"realm_access": map[string]any{
			"roles": []any{"user", "paid"},
		},
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	tenant, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	if tenant.ID != "user-123" {
		t.Errorf("ID = %q, want user-123", tenant.ID)
	}
	if tenant.Name != "testuser" {
		t.Errorf("Name = %q, want testuser", tenant.Name)
	}
	if tenant.Tier != "paid" {
		t.Errorf("Tier = %q, want paid", tenant.Tier)
	}
	if tenant.Metadata["email"] != "test@example.com" {
		t.Errorf("email = %q", tenant.Metadata["email"])
	}
	if tenant.Metadata["roles"] == "" {
		t.Error("expected roles in metadata")
	}
}

func TestJWT_ExpiredToken(t *testing.T) {
	key := generateTestRSAKey()
	kid := "test-key-1"
	jwks := startMockJWKS(key, kid)
	defer jwks.Close()

	auth := &JWTAuthenticator{
		jwksURL:    jwks.URL,
		issuer:     "",
		jwksCache:  make(map[string]*rsa.PublicKey),
		cacheTTL:   10 * time.Minute,
		httpClient: jwks.Client(),
	}

	token := signJWT(key, kid, map[string]any{
		"sub": "user-123",
		"exp": float64(time.Now().Add(-1 * time.Hour).Unix()), // expired
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	_, err := auth.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestJWT_WrongIssuer(t *testing.T) {
	key := generateTestRSAKey()
	kid := "test-key-1"
	jwks := startMockJWKS(key, kid)
	defer jwks.Close()

	auth := &JWTAuthenticator{
		jwksURL:    jwks.URL,
		issuer:     "http://keycloak.test/realms/correct",
		jwksCache:  make(map[string]*rsa.PublicKey),
		cacheTTL:   10 * time.Minute,
		httpClient: jwks.Client(),
	}

	token := signJWT(key, kid, map[string]any{
		"sub": "user-123",
		"iss": "http://keycloak.test/realms/wrong",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	_, err := auth.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestJWT_TamperedToken(t *testing.T) {
	key := generateTestRSAKey()
	kid := "test-key-1"
	jwks := startMockJWKS(key, kid)
	defer jwks.Close()

	auth := &JWTAuthenticator{
		jwksURL:    jwks.URL,
		jwksCache:  make(map[string]*rsa.PublicKey),
		cacheTTL:   10 * time.Minute,
		httpClient: jwks.Client(),
	}

	token := signJWT(key, kid, map[string]any{
		"sub": "user-123",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	// Tamper with the payload
	parts := fmt.Sprintf("%s", token)
	tampered := parts[:len(parts)/2] + "TAMPERED" + parts[len(parts)/2:]

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tampered)

	_, err := auth.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestJWT_MissingBearerToken(t *testing.T) {
	auth := &JWTAuthenticator{
		jwksCache: make(map[string]*rsa.PublicKey),
	}

	req := httptest.NewRequest("GET", "/", nil)
	_, err := auth.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for missing Bearer token")
	}
}

func TestJWT_MalformedToken(t *testing.T) {
	auth := &JWTAuthenticator{
		jwksCache: make(map[string]*rsa.PublicKey),
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.jwt.token")

	_, err := auth.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestJWT_FreeTierByDefault(t *testing.T) {
	key := generateTestRSAKey()
	kid := "test-key-1"
	jwks := startMockJWKS(key, kid)
	defer jwks.Close()

	auth := &JWTAuthenticator{
		jwksURL:    jwks.URL,
		jwksCache:  make(map[string]*rsa.PublicKey),
		cacheTTL:   10 * time.Minute,
		httpClient: jwks.Client(),
	}

	// Token with no "paid" role
	token := signJWT(key, kid, map[string]any{
		"sub": "user-456",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
		"realm_access": map[string]any{
			"roles": []any{"user"},
		},
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	tenant, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if tenant.Tier != "free" {
		t.Errorf("Tier = %q, want free (default for users without paid role)", tenant.Tier)
	}
}

func TestCompositeAuth_FallsThrough(t *testing.T) {
	// API key auth succeeds, JWT fails — API key wins
	apiAuth := NewAPIKeyAuthenticator("X-API-Key")
	apiAuth.RegisterKey("test-key", &Tenant{ID: "api-user", Tier: "paid"})

	jwtAuth := &JWTAuthenticator{
		jwksCache: make(map[string]*rsa.PublicKey),
	}

	composite := NewCompositeAuthenticator(apiAuth, jwtAuth)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "test-key")

	tenant, err := composite.Authenticate(req)
	if err != nil {
		t.Fatalf("Composite auth failed: %v", err)
	}
	if tenant.ID != "api-user" {
		t.Errorf("ID = %q, want api-user", tenant.ID)
	}
}

func TestCompositeAuth_AllFail(t *testing.T) {
	apiAuth := NewAPIKeyAuthenticator("X-API-Key")
	jwtAuth := &JWTAuthenticator{jwksCache: make(map[string]*rsa.PublicKey)}

	composite := NewCompositeAuthenticator(apiAuth, jwtAuth)

	req := httptest.NewRequest("GET", "/", nil)
	// No key, no Bearer token
	_, err := composite.Authenticate(req)
	if err == nil {
		t.Fatal("expected error when all authenticators fail")
	}
}
