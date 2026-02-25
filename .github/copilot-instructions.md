# GitHub Copilot – repository instructions

These instructions apply to all Copilot interactions in this repository.

## Configuration changes

Whenever you add, remove, or rename a flag/environment variable in
`internal/config/config.go` you **must** also update the Configuration
table in `README.md`. Each row must match the pattern:

| `--flag-name` | `ENV_VAR` | `default` | description |

No PR should land where `config.go` defines a flag that is absent from the
README table, and vice-versa.

## goreleaser config

After any edit to `.goreleaser.yaml`, verify there are no deprecated options by
mentally cross-checking against https://goreleaser.com/deprecations.  The CI
`goreleaser-check` workflow also runs `goreleaser check` on every PR — ensure
it passes before merging.

## PR checklist

Before raising or merging a PR, confirm all items in
`.github/PULL_REQUEST_TEMPLATE.md` are ticked. In particular:

- README config table kept in sync with `config.go`.
- No new goreleaser deprecation warnings introduced.
- `go test -race ./...` passes locally.
- `golangci-lint run` reports no new issues.

## Commit messages

All commits must follow the Conventional Commits specification
(`feat:`, `fix:`, `docs:`, `ci:`, `refactor:`, `chore:`, etc.) as enforced
by the `commitlint` workflow. PR titles must follow the same convention.
