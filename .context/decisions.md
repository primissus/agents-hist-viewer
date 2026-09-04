# Decisions made

> One entry per decision. What matters is the "why" and what was rejected.
> Inferred from code + AGENTS.md (single initial commit — no commit history to cite).

## [initial] · Hexagonal layout (ports & adapters)
- **Decision:** `internal/domain` defines port interfaces (`TranscriptSource`, `SearchRepository`, ...); adapters implement them; use cases live in `internal/app`.
- **Why:** swap data sources (Claude Code, Claude Desktop, Cursor) and storage backends without touching logic.
- **Rejected:** monolithic packages; adapters importing `app`.
- **Status:** current

## [initial] · Pure-Go SQLite (modernc.org/sqlite), FTS5 + BM25
- **Decision:** no CGO; FTS5 virtual table with `unicode61 remove_diacritics 2`; results ranked by BM25, deduped by session.
- **Why:** zero CGO setup pain; FTS5 gives full-text + ranking for free.
- **Rejected:** CGO mattn/go-sqlite3; full-text search outside SQLite.
- **Status:** current

## [initial] · Custom search query compiler → FTS5 MATCH
- **Decision:** hand-written lexer/parser (`domain/search_query.go`) supporting `AND`/`OR`/groups/phrases/NOT/fuzzy (`term~`, leading `~`), compiled to one FTS5 MATCH string.
- **Why:** FTS5's default query syntax is limited; fuzzy via OR-of-variants (exact/prefix/edit-distance-1, capped at 20 variants).
- **Rejected:** regex, external query libraries, one-term-only search.
- **Status:** current

## [initial] · Full-replace indexing with content-hash skip
- **Decision:** every index run re-reads sources but skips unchanged files via SHA content hash stored in `file_hashes`; sessions are fully replaced per ID (idempotent).
- **Why:** simple, safe against partial writes; `--force` bypasses skips.
- **Rejected:** incremental append/diff indexing (complex for transcripts).
- **Status:** current

## [initial] · Shrink + depth by default
- **Decision:** index applies `domain.Shrink` (tool payloads capped at 2000 runes, images dropped) and `IndexDepthDeep` by default; `--quick` indexes user text + tool results + plans only.
- **Why:** keep DB small and search relevant; CLI flags `--shrink-cap`, `--no-shrink`, `--quick`, `--deep` adjust.
- **Rejected:** raw verbatim indexing of everything, always.
- **Status:** current

## [initial] · Multi-vendor session IDs with prefixes
- **Decision:** `sessions.vendor` column (`claude` default, `claude-desktop`, `cursor`); ID prefixes `cursor:`, `cursor-plan:`, `plan:` for non-Claude sources; empty vendor filter = all.
- **Why:** one DB holds all agents; filters by vendor/type/path in TUI.
- **Rejected:** separate DBs per vendor.
- **Status:** current

## [initial] · XDG data dir for user state
- **Decision:** `~/.local/share/chv/` (respecting `XDG_DATA_HOME`) for `chv.db`, `search_history`, `manual/`, `index.json`.
- **Why:** standard, out of the repo, survives uninstall.
- **Rejected:** dotfiles in `$HOME`, repo-local state.
- **Status:** current
