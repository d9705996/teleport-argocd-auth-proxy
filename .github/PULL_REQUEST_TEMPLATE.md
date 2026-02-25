## Summary

<!-- Describe what this PR does and why. -->

## Checklist

- [ ] `go test -race ./...` passes
- [ ] `golangci-lint run` reports no new issues
- [ ] If `internal/config/config.go` was changed: **README.md configuration table has been updated** to reflect any added/removed/renamed flags or env vars
- [ ] If `.goreleaser.yaml` was changed: no new deprecation warnings (`goreleaser check` CI job passes)
- [ ] Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `ci:`, etc.)
