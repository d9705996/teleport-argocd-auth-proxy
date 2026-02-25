FROM gcr.io/distroless/static-debian12:nonroot

COPY teleport-argocd-auth-proxy /teleport-argocd-auth-proxy

EXPOSE 8080

ENTRYPOINT ["/teleport-argocd-auth-proxy"]
