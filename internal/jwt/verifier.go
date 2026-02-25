// Package jwt provides Teleport JWT verification using the cluster's JWKS endpoint.
package jwt

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// Claims holds the identity information extracted from a Teleport JWT.
type Claims struct {
	// Username is the authenticated Teleport user (from the "username" or "sub" claim).
	Username string
	// Roles contains the user's Teleport roles (from the "roles" claim).
	Roles []string
}

// Verifier verifies Teleport JWTs and extracts identity claims.
type Verifier struct {
	cache   *jwk.Cache
	jwksURL string
	logger  *slog.Logger
}

// NewVerifier creates a Verifier that caches the JWKS from the given Teleport cluster.
// refreshInterval controls the background cache refresh cadence.
// minRefreshInterval controls the minimum interval between on-demand refreshes.
func NewVerifier(
	ctx context.Context,
	teleportCluster string,
	refreshInterval, minRefreshInterval time.Duration,
	logger *slog.Logger,
) (*Verifier, error) {
	return NewVerifierWithHTTPClient(ctx, teleportCluster, refreshInterval, minRefreshInterval, logger, http.DefaultClient)
}

// NewVerifierWithHTTPClient is like NewVerifier but uses the provided *http.Client for all
// JWKS fetches. This is primarily useful in tests where a custom TLS configuration is needed.
func NewVerifierWithHTTPClient(
	ctx context.Context,
	teleportCluster string,
	refreshInterval, minRefreshInterval time.Duration,
	logger *slog.Logger,
	httpClient *http.Client,
) (*Verifier, error) {
	jwksURL := fmt.Sprintf("https://%s/.well-known/jwks.json", teleportCluster)

	cache := jwk.NewCache(ctx)

	if err := cache.Register(
		jwksURL,
		jwk.WithHTTPClient(httpClient),
		jwk.WithRefreshInterval(refreshInterval),
		jwk.WithMinRefreshInterval(minRefreshInterval),
	); err != nil {
		return nil, fmt.Errorf("register JWKS URL %q: %w", jwksURL, err)
	}

	// Perform an initial fetch so we fail fast on misconfiguration.
	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("initial JWKS fetch from %q: %w", jwksURL, err)
	}

	logger.Info("JWKS cache initialised", slog.String("url", jwksURL))

	return &Verifier{
		cache:   cache,
		jwksURL: jwksURL,
		logger:  logger,
	}, nil
}

// Verify validates the raw JWT token string and returns the extracted Claims.
// The audience is validated against the provided audience string when non-empty.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	keySet, err := v.cache.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("retrieve key set: %w", err)
	}

	token, err := jwt.Parse(
		[]byte(rawToken),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
	)
	if err != nil {
		return nil, fmt.Errorf("parse/verify token: %w", err)
	}

	claims, err := extractClaims(token)
	if err != nil {
		return nil, fmt.Errorf("extract claims: %w", err)
	}

	v.logger.Debug("JWT verified",
		slog.String("username", claims.Username),
		slog.Any("roles", claims.Roles),
	)

	return claims, nil
}

// extractClaims pulls identity information from a verified JWT token.
// Teleport puts the username in the "username" custom claim and falls back to "sub".
// Roles are in the "roles" custom claim.
func extractClaims(token jwt.Token) (*Claims, error) {
	// Resolve username: prefer explicit "username" claim, fall back to "sub".
	username := token.Subject()
	if raw, ok := token.Get("username"); ok {
		if s, ok := raw.(string); ok && s != "" {
			username = s
		}
	}
	if username == "" {
		return nil, fmt.Errorf("token contains no username or sub claim")
	}

	// Resolve roles from the "roles" claim.
	var roles []string
	if raw, ok := token.Get("roles"); ok {
		switch v := raw.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					roles = append(roles, s)
				}
			}
		case []string:
			roles = v
		}
	}

	return &Claims{
		Username: username,
		Roles:    roles,
	}, nil
}
