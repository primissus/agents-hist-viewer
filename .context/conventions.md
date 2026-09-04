# Code conventions

## Style
- Formatting: `gofmt` (via `go vet` / `make check`); Go 1.26+ features allowed
- Naming: exported Go identifiers; session ID prefixes `cursor:`, `cursor-plan:`, `plan:`; kebab-case XDG filenames (`chv.db`, `search_history`)
- Imports: stdlib first, then third-party, then `claude-code-hist-viewer/internal/...`; local aliases only for name clashes (e.g. `desktoptranscript`, `cursortranscript`)
- Comments: minimal — doc comments on exported types/functions, no narration

## Patterns we DO use
- Hexagonal layout: adapters implement `internal/domain/ports.go` interfaces; deps point inward; use-case logic stays in `internal/app`
- Errors as values; `debugf`-style optional diagnostics (io.Writer may be nil)
- Full-replace indexing (`ReplaceSession`), idempotent; content hashes in `file_hashes` to skip unchanged files
- Platform split files: `drain_unix.go`/`drain_other.go` (and `query_unix.go`/`query_other.go`) with build tags
- Inline SQLite schema migrations in `internal/adapter/sqlite/schema.go`

## FORBIDDEN patterns
- No CGO / cgo-enabled SQLite drivers (use `modernc.org/sqlite`)
- No heavy deps without reason (bubbletea ecosystem + modernc are the allowed set)
- No business logic in `cmd/` — CLI flags live only in `cmd/chv/main.go`
- No changes to XDG paths without updating `internal/config/config.go` + README
- No user-doc duplication — README is the single user doc; AGENTS.md links it

## Tests
- Where they live: `_test.go` next to code; fakes built from domain ports + temp SQLite DBs (`internal/app/service_test.go`, `internal/adapter/sqlite/repo_test.go`)
- What must always be tested: parser changes (use `testdata/` JSONL fixtures), query compiler, shrink/depth logic; TUI is manual unless Bubble Tea model tests added

## Commits
- Format: [PENDING: no commit history beyond initial commit — state your convention]
- One logical change per commit; no `chv` binary or user DB files in commits
