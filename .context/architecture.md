# Architecture

## In one sentence
`chv` indexes Claude Code, Claude Desktop, and Cursor session transcripts plus plan markdown files into a local SQLite FTS5 database, and provides a Bubble Tea TUI + CLI for full-text searching and viewing them.

## Stack
- Language / runtime: Go 1.26+ (module `claude-code-hist-viewer`), no CGO
- Main framework: charmbracelet bubbletea (TUI), bubbles, lipgloss
- Database: SQLite via `modernc.org/sqlite` (pure Go), FTS5 with `unicode61 remove_diacritics 2` tokenizer
- External services: none — reads local files only (`~/.claude`, `~/.cursor`, XDG dirs)

## Folder map
- `cmd/chv/` → CLI wiring, flags, stdout/stderr
- `cmd/cronctl/` → cron install/uninstall helper
- `internal/domain/` → models (`model.go`), search query lexer/parser/compiler, Shrink + IndexOptions, port interfaces (`ports.go`)
- `internal/app/` → use cases: IndexService, SearchService, ViewService
- `internal/config/` → XDG paths (DB, search history, `index.json`, manual archive)
- `internal/adapter/transcript/` → Claude Code `~/.claude/projects/**/*.jsonl` parser + source
- `internal/adapter/claudedesktop/transcript/` → Claude Desktop transcripts
- `internal/adapter/cursor/transcript/` → Cursor `agent-transcripts/<id>/<id>.jsonl` + `subagents/*.jsonl`
- `internal/adapter/cursor/plan/` → Cursor `~/.cursor/plans/*.plan.md`
- `internal/adapter/history/` → `~/.claude/history.jsonl` typed prompts, resolves `[Pasted text …]` placeholders
- `internal/adapter/plan/` → Claude `PLAN.md`/`PROGRESS.md` scan (fd/index.json dirs) + manual `FromFile`
- `internal/adapter/claudesettings/` → reads Claude `plansDirectory` settings (not wired into indexing yet)
- `internal/adapter/sqlite/` → FTS5 repository + inline schema migrations
- `internal/adapter/tui/` → Bubble Tea UI (views: results, detail, filter, help; palette, clipboard)

## Data flow
`IndexService.Run` unions sessions from all `TranscriptSource` adapters plus orphaned history-only sessions, then `ReplaceSession` per ID (full replace, idempotent, content-hash skip via `file_hashes`). Plans (Claude + Cursor) are indexed the same way. Search: user query → `domain.CompileSearchQuery` (custom lexer/parser → FTS5 MATCH string) → `SearchRepository.Search`, deduped by session, BM25-ranked. `chv view` builds a `SessionDetail` directly from one JSONL file — no DB writes. TUI home loads 100 recent sessions via `RecentQuery` (path/type/vendor filters).

## What does NOT exist (and should not be created)
- No HTTP server, no API, no remote sync
- No ORM — raw SQL against SQLite
- No cache layer (content-hash skip in `file_hashes` is the only skip mechanism)
- No config UI — config is `index.json` + CLI flags
