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

	// Dex reads a single X-Remote-Group header; multiple values can be set via
	// multiple headers with the same name, which net/http handles transparently.
	r.Header.Del(h.cfg.GroupHeader)
	for _, role := range claims.Roles {
		r.Header.Add(h.cfg.GroupHeader, role)
	}

	h.rp.ServeHTTP(w, r)
}
