package proxy_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"
	"os"

	"github.com/d9705996/teleport-argocd-auth-proxy/internal/config"
	"github.com/d9705996/teleport-argocd-auth-proxy/internal/jwt"
	"github.com/d9705996/teleport-argocd-auth-proxy/internal/proxy"
)

// fakeVerifier implements proxy.TokenVerifier for testing.
type fakeVerifier struct {
	claims *jwt.Claims
	err    error
}

func (f *fakeVerifier) Verify(_ context.Context, _ string) (*jwt.Claims, error) {
	return f.claims, f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func defaultConfig(backendURL string) *config.Config {
	return &config.Config{
		ListenAddr:          ":8080",
		BackendURL:          backendURL,
		TeleportCluster:     "teleport.example.com",
		JWTHeader:           "Teleport-Jwt-Assertion",
		UserHeader:          "X-Remote-User",
		GroupHeader:         "X-Remote-Group",
		StripTeleportHeader: true,
	}
}

func TestHandler_MissingJWT(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not be called when JWT is missing")
	}))
	defer backend.Close()

	cfg := defaultConfig(backend.URL)
	h, err := proxy.New(cfg, &fakeVerifier{}, testLogger())
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/some/path", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandler_InvalidJWT(t *testing.T) {
	backendCalled := false

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled = true
	}))
	defer backend.Close()

	cfg := defaultConfig(backend.URL)
	v := &fakeVerifier{err: errors.New("signature invalid")}
	h, err := proxy.New(cfg, v, testLogger())
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/path", nil)
	req.Header.Set("Teleport-Jwt-Assertion", "bad.token.here")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if backendCalled {
		t.Error("backend was called for invalid JWT")
	}
}

func TestHandler_ValidJWT_HeadersForwarded(t *testing.T) {
	receivedUser := ""
	receivedGroups := []string{}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUser = r.Header.Get("X-Remote-User")
		receivedGroups = r.Header.Values("X-Remote-Group")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := defaultConfig(backend.URL)
	v := &fakeVerifier{claims: &jwt.Claims{Username: "alice", Roles: []string{"admin", "dev"}}}
	h, err := proxy.New(cfg, v, testLogger())
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.Header.Set("Teleport-Jwt-Assertion", "valid.jwt.token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if receivedUser != "alice" {
		t.Errorf("expected X-Remote-User=alice, got %q", receivedUser)
	}
	if len(receivedGroups) != 2 {
		t.Errorf("expected 2 X-Remote-Group values, got %d: %v", len(receivedGroups), receivedGroups)
	}
}

func TestHandler_StripsTeleportHeader(t *testing.T) {
	teleportHeaderPresent := false

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		teleportHeaderPresent = r.Header.Get("Teleport-Jwt-Assertion") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := defaultConfig(backend.URL)
	cfg.StripTeleportHeader = true
	v := &fakeVerifier{claims: &jwt.Claims{Username: "alice", Roles: []string{"admin"}}}
	h, err := proxy.New(cfg, v, testLogger())
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Teleport-Jwt-Assertion", "the.jwt.token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if teleportHeaderPresent {
		t.Error("Teleport-Jwt-Assertion should have been stripped before forwarding")
	}
}

func TestHandler_StripsClientXRemoteHeaders(t *testing.T) {
	// The Dex authproxy connector requires the proxy to strip any X-Remote-*
	// headers injected by the client.
	injectedUser := ""

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only the proxy-set value should reach the backend.
		injectedUser = r.Header.Get("X-Remote-User")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := defaultConfig(backend.URL)
	v := &fakeVerifier{claims: &jwt.Claims{Username: "real-user", Roles: nil}}
	h, err := proxy.New(cfg, v, testLogger())
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Teleport-Jwt-Assertion", "valid.jwt")
	req.Header.Set("X-Remote-User", "evil-user") // client-injected, must be stripped
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if injectedUser != "real-user" {
		t.Errorf("expected X-Remote-User=real-user (from JWT), got %q", injectedUser)
	}
}

func TestHandler_HealthCheck(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not be reached for /healthz")
	}))
	defer backend.Close()

	cfg := defaultConfig(backend.URL)
	h, err := proxy.New(cfg, &fakeVerifier{}, testLogger())
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for /healthz, got %d", rec.Code)
	}
}
