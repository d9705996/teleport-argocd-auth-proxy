# teleport-argocd-auth-proxy

[![CI – Lint](https://github.com/d9705996/teleport-argocd-auth-proxy/actions/workflows/lint.yml/badge.svg)](https://github.com/d9705996/teleport-argocd-auth-proxy/actions/workflows/lint.yml)
[![CI – Test](https://github.com/d9705996/teleport-argocd-auth-proxy/actions/workflows/test.yml/badge.svg)](https://github.com/d9705996/teleport-argocd-auth-proxy/actions/workflows/test.yml)
[![Release](https://github.com/d9705996/teleport-argocd-auth-proxy/actions/workflows/release.yml/badge.svg)](https://github.com/d9705996/teleport-argocd-auth-proxy/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/d9705996/teleport-argocd-auth-proxy)](https://goreportcard.com/report/github.com/d9705996/teleport-argocd-auth-proxy)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Container](https://img.shields.io/badge/ghcr.io-d9705996%2Fteleport--argocd--auth--proxy-blue)](https://github.com/d9705996/teleport-argocd-auth-proxy/pkgs/container/teleport-argocd-auth-proxy)

A lightweight JWT-verifying reverse proxy that bridges [Gravitational Teleport](https://goteleport.com/) and [ArgoCD](https://argo-cd.readthedocs.io/) / [Dex](https://dexidp.io/).

## What it does

Teleport signs a short-lived JWT and attaches it to every request it proxies (`Teleport-Jwt-Assertion` header). This service sits between Teleport and the Dex [`authproxy` connector](https://dexidp.io/docs/connectors/authproxy/), and for every inbound request it:

1. **Fetches and caches** the Teleport cluster's JWKS endpoint (`https://<cluster>/.well-known/jwks.json`) with automatic background refresh.
2. **Verifies** the JWT cryptographic signature and expiry.
3. **Extracts** the `username` / `sub` claim and the `roles` claim from the verified token.
4. **Strips** any client-injected `X-Remote-*` headers (required by the Dex authproxy connector spec).
5. **Injects** `X-Remote-User` and `X-Remote-Group` headers and **forwards** the request to the configured backend.

```
Browser → Teleport Proxy → teleport-argocd-auth-proxy → ArgoCD server
                                  ↑ verifies JWT via JWKS      ↓ (Dex authproxy connector reads X-Remote-User / X-Remote-Group)
                                                           Dex issues OIDC token → ArgoCD UI
```

> **Note on `X-Remote-User`**: Dex treats the value of `X-Remote-User` as the user's **email address** (see [Dex authproxy connector docs](https://dexidp.io/docs/connectors/authproxy/)). Teleport usernames are typically email addresses for SSO-backed clusters; if yours are not, configure ArgoCD RBAC to match by username rather than email.

## Why it is useful

ArgoCD uses Dex as its OIDC provider. The Dex `authproxy` connector delegates authentication to a front-end proxy via plain HTTP headers — but it has no built-in way to trust a cryptographically signed token. This proxy fills that gap: it turns Teleport's signed JWT into the `X-Remote-User` / `X-Remote-Group` headers that Dex expects, so your Teleport identities and roles flow directly into ArgoCD without a separate IdP.

## Getting started

### Prerequisites

| Requirement | Version |
|---|---|
| Go | 1.22+ |
| A running Teleport cluster | 13+ |
| ArgoCD with Dex `authproxy` connector configured | any |

### Run with Docker

```sh
docker run --rm \
  -e TELEPORT_CLUSTER=teleport.example.com \
  -e BACKEND_URL=http://argocd-server.argocd.svc.cluster.local:8080 \
  -p 8080:8080 \
  ghcr.io/d9705996/teleport-argocd-auth-proxy:latest
```

### Run from source

```sh
git clone https://github.com/d9705996/teleport-argocd-auth-proxy.git
cd teleport-argocd-auth-proxy
go run ./cmd/main.go \
  --teleport-cluster teleport.example.com \
  --backend-url http://argocd-server.argocd.svc.cluster.local:8080
```

### Kubernetes (example)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: teleport-argocd-auth-proxy
  namespace: argocd
spec:
  replicas: 1
  selector:
    matchLabels:
      app: teleport-argocd-auth-proxy
  template:
    metadata:
      labels:
        app: teleport-argocd-auth-proxy
    spec:
      containers:
        - name: proxy
          image: ghcr.io/d9705996/teleport-argocd-auth-proxy:latest
          env:
            - name: TELEPORT_CLUSTER
              value: "teleport.example.com"
            - name: BACKEND_URL
              value: "http://argocd-server.argocd.svc.cluster.local:8080"
          ports:
            - containerPort: 8080
          readinessProbe:
            httpGet:
              path: /healthz
              port: 8080
```

## Configuration

All flags can be set via environment variables. CLI flags take precedence.

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--listen-addr` | `LISTEN_ADDR` | `:8080` | Address to listen on |
| `--backend-url` | `BACKEND_URL` | *(required)* | ArgoCD server URL to proxy to (e.g. `http://argocd-server.argocd.svc.cluster.local:8080`) |
| `--teleport-cluster` | `TELEPORT_CLUSTER` | *(required)* | Teleport cluster FQDN (used to build the JWKS URL) |
| `--jwt-header` | `JWT_HEADER` | `Teleport-Jwt-Assertion` | Header carrying the Teleport JWT |
| `--user-header` | `USER_HEADER` | `X-Remote-User` | Header injected for the authenticated user |
| `--group-header` | `GROUP_HEADER` | `X-Remote-Group` | Header injected for the user's roles/groups |
| `--jwks-refresh-interval` | `JWKS_REFRESH_INTERVAL` | `15m` | Background JWKS cache refresh cadence |
| `--jwks-min-refresh-interval` | `JWKS_MIN_REFRESH_INTERVAL` | `5m` | Minimum interval between on-demand JWKS refreshes |
| `--read-timeout` | `READ_TIMEOUT` | `10s` | HTTP server read timeout |
| `--write-timeout` | `WRITE_TIMEOUT` | `30s` | HTTP server write timeout |
| `--shutdown-timeout` | `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout |
| `--log-level` | `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `--strip-teleport-header` / `--no-strip-teleport-header` | `STRIP_TELEPORT_HEADER` | `true` | Strip the JWT header before forwarding |

### Dex `authproxy` connector example

```yaml
connectors:
  - type: authproxy
    id: teleport
    name: Teleport
    config:
      userHeader: X-Remote-User
      groupHeader: X-Remote-Group
```

### Teleport app config example

```yaml
app_service:
  enabled: true
  apps:
    - name: argocd
      uri: http://teleport-argocd-auth-proxy.argocd.svc.cluster.local:8080
      public_addr: argocd.teleport.example.com
      rewrite:
        headers:
          - "Teleport-Jwt-Assertion: {{internal.jwt}}"
```

## Health check

`GET /healthz` responds with `200 OK` and body `ok`. No authentication is required — suitable for liveness and readiness probes.

## Logging

All log output is structured JSON written to stdout, using Go's `log/slog` package.

```json
{"time":"2026-02-25T10:00:00Z","level":"INFO","service":"teleport-argocd-auth-proxy","version":"v1.0.0","msg":"request authenticated","username":"alice","roles":["admin"],"method":"GET","path":"/"}
```

Set `LOG_LEVEL=debug` to include per-request JWKS cache details and source locations.

## Project structure

```
cmd/            ← binary entry point & server lifecycle
internal/
  config/       ← configuration (kong – CLI flags + env vars)
  jwt/          ← JWKS-backed JWT verifier
  proxy/        ← reverse proxy handler
.github/
  workflows/
    lint.yml    ← golangci-lint on PRs targeting main
    test.yml    ← go test -race on PRs targeting main
    lint.yml    ← golangci-lint on PRs targeting main
    test.yml    ← go test -race on PRs targeting main
    release.yml ← goreleaser on v*.*.* tags → GHCR
    pr-title.yml     ← enforce Conventional Commits on PR titles
    commitlint.yml   ← enforce Conventional Commits on individual commits
commitlint.config.cjs  ← commitlint rule set
.goreleaser.yaml
Dockerfile      ← distroless/static-debian12 image
```

## Contributing

Contributions are welcome. Please open an issue first to discuss significant changes.

1. Fork the repository and create a feature branch from `main`.
2. Ensure `go test -race ./...` passes and `golangci-lint run` reports no new issues.
3. Submit a pull request targeting `main` — CI will run lint and tests automatically.

## Getting help

- **Bugs / feature requests** — open a [GitHub Issue](https://github.com/d9705996/teleport-argocd-auth-proxy/issues).
- **Teleport documentation** — [Application Access JWT guide](https://goteleport.com/docs/enroll-resources/application-access/jwt/introduction/).
- **Dex authproxy connector** — [dexidp.io docs](https://dexidp.io/docs/connectors/authproxy/).

## License

[MIT](LICENSE)
