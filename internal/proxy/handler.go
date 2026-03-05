// Package proxy implements the HTTP reverse-proxy handler.
// It verifies the Teleport JWT, strips X-Remote-* headers from the incoming
// request (per the Dex authproxy connector requirement), then injects the
// authenticated username and groups before forwarding to the backend.
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/d9705996/teleport-argocd-auth-proxy/internal/config"
	"github.com/d9705996/teleport-argocd-auth-proxy/internal/jwt"
)

// TokenVerifier is the interface used by Handler to validate JWT tokens.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (*jwt.Claims, error)
}

// Handler is an http.Handler that authenticates requests via Teleport JWTs
// before proxying them to the ArgoCD/Dex authproxy backend.
type Handler struct {
	rp       *httputil.ReverseProxy
	verifier TokenVerifier
	cfg      *config.Config
	logger   *slog.Logger
}

// New creates a Handler that proxies to backendURL using the provided verifier.
func New(cfg *config.Config, verifier TokenVerifier, logger *slog.Logger) (*Handler, error) {
	target, err := url.Parse(cfg.BackendURL)
	if err != nil {
		return nil, fmt.Errorf("parse backend URL %q: %w", cfg.BackendURL, err)
	}

	rp := httputil.NewSingleHostReverseProxy(target)

	// Wrap the default director so we can adjust headers after the base director runs.
	baseDirector := rp.Director
	rp.Director = func(req *http.Request) {
		baseDirector(req)
		// Preserve the original Host header so backends behind virtual hosting work.
		req.Host = target.Host
	}

	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("proxy error",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}

	return &Handler{
		rp:       rp,
		verifier: verifier,
		cfg:      cfg,
		logger:   logger,
	}, nil
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ── Security: strip any X-Remote-* headers the client may have injected.
	// The Dex authproxy connector spec explicitly requires this for ANY URL path,
	// before the request is forwarded to Dex. Apply unconditionally so that paths
	// handled locally (e.g. /healthz) never propagate these headers either.
	for key := range r.Header {
		if strings.HasPrefix(strings.ToLower(key), "x-remote-") {
			r.Header.Del(key)
		}
	}

	// Health probe -- no auth required.
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}

	// ── OIDC public endpoints pass-through (no JWT required).
	// The OIDC discovery document and JWKS endpoint are public by spec (RFC 8414 /
	// OpenID Connect Discovery). ArgoCD/Kargo server pods call these endpoints
	// internally to initialise their OIDC provider and verify token signatures.
	// Requiring a Teleport JWT here would prevent that internal call from
	// succeeding (the server has no Teleport session), causing repeated
	// "Initializing OIDC provider" retries and "failed to verify signature" errors.
	if isOIDCPublicPath(r.URL.Path) {
		h.rp.ServeHTTP(w, r)
		return
	}

	// ── Extract the Teleport JWT.
	rawToken := r.Header.Get(h.cfg.JWTHeader)
	if rawToken == "" {
		h.logger.Warn("request missing JWT header",
			slog.String("header", h.cfg.JWTHeader),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("path", r.URL.Path),
		)
		http.Error(w, "unauthorized: missing JWT", http.StatusUnauthorized)
		return
	}

	// ── Verify the JWT.
	claims, err := h.verifier.Verify(r.Context(), rawToken)
	if err != nil {
		h.logger.Warn("JWT verification failed",
			slog.String("error", err.Error()),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("path", r.URL.Path),
		)
		http.Error(w, "unauthorized: invalid JWT", http.StatusUnauthorized)
		return
	}

	h.logger.Info("request authenticated",
		slog.String("username", claims.Username),
		slog.Any("roles", claims.Roles),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)

	// ── Remove the JWT header before forwarding if configured.
	if h.cfg.StripTeleportHeader {
		r.Header.Del(h.cfg.JWTHeader)
	}

	// ── Inject identity headers expected by Dex authproxy connector.
	r.Header.Set(h.cfg.UserHeader, claims.Username)

	// Dex authproxy connector uses Header.Get() which returns only the first
	// header value. Join all roles as a comma-separated string in a single
	// header so Dex splits them correctly into individual groups.
	if len(claims.Roles) > 0 {
		r.Header.Set(h.cfg.GroupHeader, strings.Join(claims.Roles, ","))
	} else {
		r.Header.Del(h.cfg.GroupHeader)
	}

	h.rp.ServeHTTP(w, r)
}

// isOIDCPublicPath returns true for OIDC discovery and JWKS endpoints that
// must be accessible without authentication. These paths contain only public
// key material and metadata — no secrets are exposed.
//
//   - /.well-known/openid-configuration  (standard OIDC discovery)
//   - /dex/.well-known/openid-configuration  (Dex embedded in ArgoCD/Kargo)
//   - /api/dex/.well-known/openid-configuration  (ArgoCD Dex proxy prefix)
//   - /keys  (Dex JWKS endpoint)
//   - /dex/keys
//   - /api/dex/keys
func isOIDCPublicPath(path string) bool {
	publicSuffixes := []string{
		"/.well-known/openid-configuration",
		"/keys",
	}
	for _, suffix := range publicSuffixes {
		if path == suffix || strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}
