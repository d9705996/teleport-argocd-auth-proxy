# ── Stage 1: obtain CA certificates and a minimal passwd/group ──────────────
# Alpine is used solely to pull in the system CA bundle and the nobody user
# entries. Nothing from Alpine ends up in the final image except these files.
FROM alpine:3 AS base
RUN apk --no-cache add ca-certificates

# ── Stage 2: scratch – absolute minimum runtime ───────────────────────────────
FROM scratch

# CA certificates are required for TLS verification of the Teleport JWKS
# endpoint (https://<cluster>/.well-known/jwks.json).
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# /etc/passwd and /etc/group allow the process to resolve "nobody" (UID 65534)
# without a libc – scratch has no resolver of its own.
COPY --from=base /etc/passwd /etc/passwd
COPY --from=base /etc/group  /etc/group

# The binary is injected by goreleaser (CGO_ENABLED=0, fully static).
# With dockers_v2, goreleaser places each platform's artifacts under
# $TARGETPLATFORM/ in the build context, so the ARG is required.
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/teleport-argocd-auth-proxy /teleport-argocd-auth-proxy

USER nobody

EXPOSE 8080

ENTRYPOINT ["/teleport-argocd-auth-proxy"]
