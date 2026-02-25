// Package config provides configuration loading via CLI flags and environment variables.
package config

import (
	"time"

	"github.com/alecthomas/kong"
)

// Config holds all runtime configuration for the proxy.
type Config struct {
	// ListenAddr is the address to listen for incoming requests on.
	ListenAddr string `help:"Address to listen on." env:"LISTEN_ADDR" default:":8080" name:"listen-addr"`

	// BackendURL is the ArgoCD / Dex authproxy backend to forward verified requests to.
	BackendURL string `help:"Backend URL to proxy requests to (e.g. ArgoCD/Dex endpoint)." env:"BACKEND_URL" required:"" name:"backend-url"`

	// TeleportCluster is the public FQDN of the Teleport Proxy Service.
	// Used to construct the JWKS URL: https://<cluster>/.well-known/jwks.json
	TeleportCluster string `help:"Teleport cluster public address (e.g. teleport.example.com)." env:"TELEPORT_CLUSTER" required:"" name:"teleport-cluster"`

	// JWTHeader is the request header that carries the Teleport JWT.
	JWTHeader string `help:"Header name carrying the Teleport JWT." env:"JWT_HEADER" default:"Teleport-Jwt-Assertion" name:"jwt-header"`

	// UserHeader is the header written to the backend with the authenticated username.
	UserHeader string `help:"Header name to set for the authenticated user." env:"USER_HEADER" default:"X-Remote-User" name:"user-header"`

	// GroupHeader is the header written to the backend with the user's groups/roles.
	GroupHeader string `help:"Header name to set for the authenticated groups." env:"GROUP_HEADER" default:"X-Remote-Group" name:"group-header"`

	// JWKSRefreshInterval controls how often the JWKS cache is refreshed.
	JWKSRefreshInterval time.Duration `help:"How often to refresh the JWKS key cache." env:"JWKS_REFRESH_INTERVAL" default:"15m" name:"jwks-refresh-interval"`

	// JWKSMinRefreshInterval is the minimum wait between JWKS refreshes triggered by cache miss.
	JWKSMinRefreshInterval time.Duration `help:"Minimum interval between JWKS refresh attempts on cache miss." env:"JWKS_MIN_REFRESH_INTERVAL" default:"5m" name:"jwks-min-refresh-interval"`

	// ReadTimeout is the HTTP server read timeout.
	ReadTimeout time.Duration `help:"HTTP server read timeout." env:"READ_TIMEOUT" default:"10s" name:"read-timeout"`

	// WriteTimeout is the HTTP server write timeout.
	WriteTimeout time.Duration `help:"HTTP server write timeout." env:"WRITE_TIMEOUT" default:"30s" name:"write-timeout"`

	// ShutdownTimeout is the graceful shutdown timeout.
	ShutdownTimeout time.Duration `help:"Graceful shutdown timeout." env:"SHUTDOWN_TIMEOUT" default:"10s" name:"shutdown-timeout"`

	// LogLevel sets the minimum log level (debug, info, warn, error).
	LogLevel string `help:"Minimum log level (debug, info, warn, error)." env:"LOG_LEVEL" default:"info" name:"log-level" enum:"debug,info,warn,error"`

	// StripTeleportHeader removes the Teleport-Jwt-Assertion header before forwarding.
	StripTeleportHeader bool `help:"Strip the Teleport JWT header before forwarding to backend." env:"STRIP_TELEPORT_HEADER" default:"true" name:"strip-teleport-header" negatable:""`

	// TLSInsecureSkipVerify disables TLS certificate verification for JWKS fetches.
	// WARNING: This should only be used in development/staging environments where the
	// Teleport cluster certificate does not match the cluster's public address.
	TLSInsecureSkipVerify bool `help:"Disable TLS certificate verification for JWKS fetches (insecure, use with caution)." env:"TLS_INSECURE_SKIP_VERIFY" default:"false" name:"tls-insecure-skip-verify"`
}

// Load parses CLI flags and environment variables into a Config, then returns it.
// args should typically be os.Args[1:].
func Load(args []string) (*Config, error) {
	cfg := &Config{}
	parser, err := kong.New(cfg,
		kong.Name("teleport-argocd-auth-proxy"),
		kong.Description("JWT-verifying reverse proxy between Gravitational Teleport and ArgoCD/Dex authproxy."),
		kong.UsageOnError(),
	)
	if err != nil {
		return nil, err
	}
	if _, err = parser.Parse(args); err != nil {
		return nil, err
	}
	return cfg, nil
}
