# Agent instructions — chv

Claude Code & Cursor History Viewer (`chv`): Go CLI + Bubble Tea TUI. Indexes Claude Code + Cursor transcripts, typed prompts, and plan markdown → SQLite FTS5 for global search.

User docs: [README.md](README.md). File for coding agents.

## Commands

```sh
make build          # go build -o chv ./cmd/chv
make test           # go test ./...
make check          # vet + test
make dev            # hot reload (requires make dev-setup)
make install        # go install ./cmd/chv
make install-cron   # optional: cron every 4h for chv index
make uninstall-cron # remove chv-managed cron entry
```

Entry: `cmd/chv/main.go` — subcommands `index` (`i`), `search`, `view` (`v`), default TUI.

## Architecture

Hexagonal layout. Deps point inward.

```
cmd/chv/              CLI wiring, flags, stdout/stderr
internal/
  domain/             models (Vendor, RecordKind), ShrinkConfig, search query compiler, port interfaces
  app/                IndexService, SearchService (use cases)
  config/             XDG paths (DB, search history, index.json, manual archive)
  adapter/
    transcript/       ~/.claude/projects JSONL
    cursor/transcript ~/.cursor/projects agent-transcripts JSONL
    cursor/plan       ~/.cursor/plans/*.plan.md
    history/          ~/.claude/history.jsonl
    plan/             Claude PLAN.md/PROGRESS.md (fd scan + index.json dirs)
    sqlite/           FTS5 repository + inline schema migrations
    tui/              Bubble Tea UI
```

**Ports** (`internal/domain/ports.go`):

- `TranscriptSource` — list sessions, stream messages
- `TranscriptFingerprintSource` — optional transcript content fingerprint for skip logic
- `PromptLog` — list typed prompts
- `PlanSource` — list plan file paths to index
- `SearchRepository` — init, replace session, file hash, search, recent, session detail

New I/O/storage backends implement these interfaces; logic stays in `internal/app`.

## Data flow

1. **Index** (`IndexService.Run`): union sessions from all `TranscriptSource` adapters + orphaned history-only sessions → `ReplaceSession` per ID (full replace, idempotent). Then index Claude plans (fd/config) and Cursor plans (`~/.cursor/plans`), with content-hash skip via `file_hashes`.
2. **Search** (`SearchService`): trims and compiles user query syntax (`AND`/`OR`/groups/phrases/NOT/fuzzy) to FTS5; empty query returns nil.
3. **View** (`ViewService`): validates/detects a single Claude/Cursor JSONL transcript and builds `SessionDetail` directly; no DB writes.
4. **TUI** (`tui.Model`): home loads 100 recent sessions (`RecentQuery` supports path/type/vendor filters); `/` search hits full FTS (limit 500 unique sessions); query history in `config.SearchHistoryPath()`. `NewDetailApp` opens a prebuilt thread for `chv view`.

Index applies `domain.Shrink` to tool payloads (configurable cap; images dropped). Deep mode indexes text/thinking verbatim; quick mode indexes user text, tool results, and plans.

**Vendors** — `domain.Vendor`: `claude` (default), `cursor`. Stored on `sessions.vendor`. Empty filter = all vendors. Session ID prefixes: `cursor:`, `cursor-plan:`, `plan:`.

**Schema migrations** — `sqlite.Repo.Init()` runs `CREATE TABLE IF NOT EXISTS` plus `ALTER TABLE` for legacy DBs. Migrations must add columns before indexes on those columns. `Init` is called from `IndexService` on index; users upgrade via `chv index`.

## Conventions

- **Go 1.26+**, module `claude-code-hist-viewer`, no CGO (`modernc.org/sqlite`).
- Match style: minimal comments, small focused diffs, no over-abstraction.
- Tests: fakes w/ domain ports + temp SQLite DBs (`internal/app/service_test.go`, `internal/adapter/sqlite/repo_test.go`). Prefer `testdata/` JSONL fixtures for parser tests.
- TUI keys + help text stay in sync: update `internal/adapter/tui/keys.go`, `help_view.go`, README on binding changes.
- CLI flags in `cmd/chv/main.go` only; doc changes in README.

## Package notes

| Area | Notes |
|------|-------|
| `adapter/transcript` | `~/.claude/projects/<proj>/<id>.jsonl`, sidechains. `VendorClaude`. Title from `custom-title`/`ai-title`. |
| `adapter/cursor/transcript` | `~/.cursor/projects/<proj>/agent-transcripts/<id>/<id>.jsonl` + `subagents/*.jsonl`. IDs `cursor:<id>`. Project path decoded from folder name. |
| `adapter/cursor/plan` | `~/.cursor/plans/*.plan.md`. IDs `cursor-plan:<hash>`. Title from frontmatter `name`, then `# Heading`, then filename. |
| `adapter/history` | Resolves `[Pasted text …]` placeholders via `pastedContents`. |
| `adapter/plan` | fd/walk scan for Claude `PLAN.md`/`PROGRESS.md` under `~/.claude` + configured `index.json` directories; `PlanSource.PlanPaths`. Manual `FromFile` for `chv index <file>`. |
| `adapter/claudesettings` | Reads Claude `plansDirectory` settings but is not wired into indexing yet; document `index.json` for external plan directories. |
| `adapter/sqlite` | FTS5 + BM25; search dedupes by session. `RecentQuery` filters `record_kind`, `project_path`, `vendor`. |
| `adapter/tui` | Views: results, detail, filter (path/type/vendor + pagination), help. Clipboard via `pbcopy` (darwin) / `xclip` (linux). Filter `esc` closes; `c` clears active category in-modal. |

## What to avoid

- No `chv` binary or user DB files in commits.
- No CGO SQLite or heavy deps without reason.
- No XDG path changes without updating `internal/config/config.go` + README.
- No user doc duplication — link README.

## Verifying changes

```sh
make check
go build -o chv ./cmd/chv   # must compile
```

Parser/repo changes: add/extend tests. TUI: manual unless Bubble Tea model tests added.
