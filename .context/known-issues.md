# Known issues (gotchas)

> The traps that already bit you. Each one saves an hour of the agent's time
> (and yours).

## `claudesettings` reads `plansDirectory` but does nothing
- **Happens when:** you expect Claude's configured plans directory to be indexed automatically
- **Real cause:** `adapter/claudesettings/plan_dirs.go` parses the setting but is not wired into `IndexService`'s plan sources
- **Fix:** don't rely on it; document `index.json` directories for external plan dirs (see AGENTS.md package notes)

## Schema migrations order matters
- **Happens when:** adding a column to `sessions`/`messages` for a legacy DB
- **Real cause:** `CREATE INDEX` on a not-yet-added column fails on upgrade
- **Fix:** migrations must add columns before creating indexes on those columns (`internal/adapter/sqlite/schema.go`); users upgrade via `chv index`

## AGENTS.md vendor list is behind the model
- **Happens when:** filtering by vendor in code/tests
- **Real cause:** `domain/model.go` defines `claude`, `claude-desktop`, `cursor` (with `adapter/claudedesktop/transcript`), but AGENTS.md's vendor paragraph mentions only `claude`/`cursor`
- **Fix:** treat `model.go` as truth; update the docs when touching vendor handling

## TUI `c` key is overloaded across views
- **Happens when:** users or docs confuse `c` behavior
- **Real cause:** in filter view `c` clears the active filter category; in search mode `c` clears the search box (`keys.go`)
- **Fix:** keep help text view-specific (it is); don't "unify" the binding without updating `keys.go` + `help_view.go` + README together

## A stray stdout write anywhere reachable from `chv mcp` would silently break MCP clients
- **Happens when:** touching any code path `chv mcp` can call — `internal/app`, `internal/adapter/sqlite`, `internal/adapter/ollama`, or a future dependency — and it grows a `fmt.Print*`/`log.Print*` (or any other write) to stdout
- **Real cause:** stdout is the MCP protocol channel for `chv mcp` (the SDK's `StdioTransport` frames JSON-RPC over it); any extra byte on stdout corrupts the stream for the client, and the failure mode is silent/confusing on the client side rather than a chv crash
- **Fix:** this was checked/grepped clean at implementation time (`cmdMCP` also defensively redirects the stdlib `log` package to stderr), but it's a standing risk for any future change — new code on this path must log to stderr, and it's worth a grep for stray `os.Stdout`/`fmt.Print`/bare `log.Print` before shipping changes that touch the `chv mcp` call graph

## `summaries` table has no foreign key to `sessions`, on purpose
- **Happens when:** you go looking for referential integrity between `summaries.session_id` and `sessions.id` and don't find it
- **Real cause:** not an oversight — see `.context/decisions.md` `[summarize]`: `ReplaceSession` deletes+reinserts sessions on every re-index, so an `ON DELETE CASCADE` FK would wipe the summary cache on every routine reindex; `InitSummaries` instead sweeps orphaned rows explicitly, and staleness is detected via `source_hash`, not via FK/rowid mechanics
- **Fix:** don't "fix" this by adding a FK; if you touch `summaries`, keep the explicit orphan sweep in `InitSummaries` and the `source_hash` check in `SummarizeService`

## Things that look broken but are intentional
- The `chv` binary at the repo root: build output, gitignored (`/chv`), don't commit it
- `go vet` is the only lint target (`make lint` = vet) — no golangci-lint by design
- `has_transcript` false for plan/history-only sessions — intended, not missing data
- Search returning fewer results than you'd expect on empty/whitespace query — empty query compiles to nil intentionally (no match-all search)
