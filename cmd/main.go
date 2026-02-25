package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/d9705996/teleport-argocd-auth-proxy/internal/config"
	"github.com/d9705996/teleport-argocd-auth-proxy/internal/jwt"
	"github.com/d9705996/teleport-argocd-auth-proxy/internal/proxy"
)

// buildVersion is set at link time by goreleaser via -X ...cmd.buildVersion=<tag>.
var buildVersion = "dev"

// buildCommit and buildDate are also injected by goreleaser.
var buildCommit = "none"
var buildDate = "unknown"

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		// kong already printed a usage message; just surface the raw error.
		_, _ = fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 1
	}

	logger := buildLogger(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ── Build the HTTP client used for JWKS fetches.
	jwksHTTPClient := http.DefaultClient
	if cfg.TLSInsecureSkipVerify {
		logger.Warn("TLS certificate verification disabled for JWKS fetches — do not use in production")
		jwksHTTPClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
		}
	}

	// ── Build the JWKS verifier (performs an initial key fetch).
	verifier, err := jwt.NewVerifierWithHTTPClient(
		ctx,
		cfg.TeleportCluster,
		cfg.JWKSRefreshInterval,
		cfg.JWKSMinRefreshInterval,
		logger,
		jwksHTTPClient,
	)
	if err != nil {
		logger.Error("failed to initialise JWT verifier", slog.String("error", err.Error()))
		return 1
	}

	// ── Build the proxy handler.
	handler, err := proxy.New(cfg, verifier, logger)
	if err != nil {
		logger.Error("failed to create proxy handler", slog.String("error", err.Error()))
		return 1
	}

	// ── Start the HTTP server.
	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("starting proxy",
			slog.String("listen_addr", cfg.ListenAddr),
			slog.String("backend_url", cfg.BackendURL),
			slog.String("teleport_cluster", cfg.TeleportCluster),
		)
		serverErr <- srv.ListenAndServe()
	}()

	select {
	case err = <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.String("error", err.Error()))
			return 1
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections",
			slog.Duration("timeout", cfg.ShutdownTimeout),
		)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err = srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
			return 1
		}
	}

	logger.Info("server stopped")
	return 0
}

// buildLogger returns a JSON slog.Logger at the configured minimum level.
func buildLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     l,
		AddSource: l == slog.LevelDebug,
	})
	return slog.New(h).With(
		slog.String("service", "teleport-argocd-auth-proxy"),
		slog.String("version", buildVersion),
		slog.String("commit", buildCommit),
		slog.String("build_date", buildDate),
	)
}
