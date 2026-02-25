package jwt_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"

	internaljwt "github.com/d9705996/teleport-argocd-auth-proxy/internal/jwt"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// setupTLSJWKSServer creates an RSA key pair and starts a TLS test JWKS server.
// It responds to any path with the JWKS document.
func setupTLSJWKSServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	pubKey, err := jwk.PublicKeyOf(privKey)
	if err != nil {
		t.Fatalf("build JWK public key: %v", err)
	}
	if err := pubKey.Set(jwk.KeyIDKey, "test-key-id"); err != nil {
		t.Fatalf("set key ID: %v", err)
	}
	if err := pubKey.Set(jwk.AlgorithmKey, jwa.RS256); err != nil {
		t.Fatalf("set algorithm: %v", err)
	}

	keySet := jwk.NewSet()
	if err := keySet.AddKey(pubKey); err != nil {
		t.Fatalf("add key to set: %v", err)
	}
	jwksBytes, err := json.Marshal(keySet)
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksBytes)
	}))
	t.Cleanup(srv.Close)

	return srv, privKey
}

// buildToken creates a signed Teleport-style JWT using the provided private key.
func buildToken(t *testing.T, privKey *rsa.PrivateKey, username string, roles []string, validFor time.Duration) string {
	t.Helper()

	tok, err := jwt.NewBuilder().
		Subject(username).
		Claim("username", username).
		Claim("roles", roles).
		IssuedAt(time.Now()).
		Expiration(time.Now().Add(validFor)).
		NotBefore(time.Now().Add(-time.Second)).
		Build()
	if err != nil {
		t.Fatalf("build JWT: %v", err)
	}

	privJWK, err := jwk.FromRaw(privKey)
	if err != nil {
		t.Fatalf("build private JWK: %v", err)
	}
	if err := privJWK.Set(jwk.KeyIDKey, "test-key-id"); err != nil {
		t.Fatalf("set key ID on private key: %v", err)
	}
	if err := privJWK.Set(jwk.AlgorithmKey, jwa.RS256); err != nil {
		t.Fatalf("set algorithm on private key: %v", err)
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privJWK))
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return string(signed)
}

// newTestVerifier is a helper that creates a Verifier with intervals satisfying
// the httprc library minimum refresh-window constraint (>= 15 minutes).
func newTestVerifier(t *testing.T, tlsSrv *httptest.Server) *internaljwt.Verifier {
	t.Helper()
	host := tlsSrv.Listener.Addr().String()
	v, err := internaljwt.NewVerifierWithHTTPClient(
		context.Background(),
		host,
		20*time.Minute,
		15*time.Minute,
		testLogger(),
		tlsSrv.Client(),
	)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	return v
}

func TestVerifier_ValidToken(t *testing.T) {
	tlsSrv, privKey := setupTLSJWKSServer(t)
	v := newTestVerifier(t, tlsSrv)

	rawToken := buildToken(t, privKey, "alice", []string{"admin", "dev"}, time.Hour)

	ctx := context.Background()
	claims, err := v.Verify(ctx, rawToken)
	if err != nil {
		t.Fatalf("verify valid token: %v", err)
	}
	if claims.Username != "alice" {
		t.Errorf("expected username %q, got %q", "alice", claims.Username)
	}
	if len(claims.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d: %v", len(claims.Roles), claims.Roles)
	}
}

func TestVerifier_NoRoles(t *testing.T) {
	tlsSrv, privKey := setupTLSJWKSServer(t)
	v := newTestVerifier(t, tlsSrv)

	rawToken := buildToken(t, privKey, "bob", nil, time.Hour)

	ctx := context.Background()
	claims, err := v.Verify(ctx, rawToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Username != "bob" {
		t.Errorf("expected username %q, got %q", "bob", claims.Username)
	}
	if len(claims.Roles) != 0 {
		t.Errorf("expected 0 roles, got %d", len(claims.Roles))
	}
}

func TestVerifier_ExpiredToken(t *testing.T) {
	tlsSrv, privKey := setupTLSJWKSServer(t)
	v := newTestVerifier(t, tlsSrv)

	rawToken := buildToken(t, privKey, "bob", []string{"viewer"}, -time.Minute)

	_, err := v.Verify(context.Background(), rawToken)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestVerifier_InvalidSignature(t *testing.T) {
	tlsSrv, _ := setupTLSJWKSServer(t)
	v := newTestVerifier(t, tlsSrv)

	// Build a token with a different (untrusted) key.
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	rawToken := buildToken(t, otherKey, "eve", []string{"admin"}, time.Hour)

	_, err := v.Verify(context.Background(), rawToken)
	if err == nil {
		t.Fatal("expected error for invalid signature, got nil")
	}
}
