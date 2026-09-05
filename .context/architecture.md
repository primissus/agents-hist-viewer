# Architecture

## In one sentence
`chv` indexes Claude Code, Claude Desktop, Cursor, OpenCode, and Codex session transcripts plus plan markdown files into a local SQLite FTS5 database, provides a Bubble Tea TUI + CLI for full-text searching and viewing them, and optionally embeds/mines that same corpus locally via Ollama for semantic search, RAG Q&A, and usage-pattern (skill/script) candidates.

## Stack
- Language / runtime: Go 1.26+ (module `claude-code-hist-viewer`), no CGO
- Main framework: charmbracelet bubbletea (TUI), bubbles, lipgloss
- Database: SQLite via `modernc.org/sqlite` (pure Go), FTS5 with `unicode61 remove_diacritics 2` tokenizer
- External services: Ollama (`http://localhost:11434` by default) — the only allowed local HTTP dependency, used solely by `chv embed`/`ask`/`patterns`; otherwise reads local files only (`~/.claude`, `~/.cursor`, `~/.codex`/`$CODEX_HOME`, XDG dirs, OpenCode SQLite DB)

## Folder map
- `cmd/chv/` → CLI wiring, flags, stdout/stderr
- `cmd/cronctl/` → cron install/uninstall helper
- `internal/domain/` → models (`model.go`), search query lexer/parser/compiler, Shrink + IndexOptions, embedding types (`embedding.go`), port interfaces (`ports.go`)
- `internal/app/` → use cases: IndexService, SearchService, ViewService, EmbedService, AskService, PatternService, pattern report renderer (`report.go`)
- `internal/config/` → XDG paths (DB, search history, `index.json`, manual archive); `index.json` also carries `embed_model`/`chat_model`/`ollama_url`
- `internal/adapter/transcript/` → Claude Code `~/.claude/projects/**/*.jsonl` parser + source
- `internal/adapter/claudedesktop/transcript/` → Claude Desktop transcripts
- `internal/adapter/cursor/transcript/` → Cursor `agent-transcripts/<id>/<id>.jsonl` + `subagents/*.jsonl`
- `internal/adapter/cursor/plan/` → Cursor `~/.cursor/plans/*.plan.md`
- `internal/adapter/codex/transcript/` → Codex `$CODEX_HOME/sessions/**/rollout-*.jsonl` envelopes
- `internal/adapter/opencode/transcript/` → OpenCode `opencode.db` (read-only SQLite)
- `internal/adapter/history/` → `~/.claude/history.jsonl` typed prompts, resolves `[Pasted text …]` placeholders
- `internal/adapter/plan/` → Claude `PLAN.md`/`PROGRESS.md` scan (fd/index.json dirs) + manual `FromFile`
- `internal/adapter/claudesettings/` → reads Claude `plansDirectory` settings (not wired into indexing yet)
- `internal/adapter/sqlite/` → FTS5 repository + inline schema migrations + `embeddings` table CRUD/search (`embedding_repo.go`)
- `internal/adapter/ollama/` → HTTP client implementing `domain.Embedder` + `domain.ChatModel` against a local Ollama server
- `internal/adapter/cluster/` → pure-Go k-means++ over `[]float32` (cosine via L2-normalized vectors + squared Euclidean)
- `internal/adapter/tui/` → Bubble Tea UI (views: results, detail, filter, help; palette, clipboard)

## Data flow
`IndexService.Run` unions sessions from all `TranscriptSource` adapters plus orphaned history-only sessions, then `ReplaceSession` per ID (full replace, idempotent, content-hash skip via `file_hashes`). Plans (Claude + Cursor) are indexed the same way. Search: user query → `domain.CompileSearchQuery` (custom lexer/parser → FTS5 MATCH string) → `SearchRepository.Search`, deduped by session, BM25-ranked. `chv view` builds a `SessionDetail` directly from one JSONL file — no DB writes. TUI home loads 100 recent sessions via `RecentQuery` (path/type/vendor filters).

**Embeddings / RAG / patterns**: `chv embed` selects eligible message blocks (user/assistant text, Bash `tool_use` commands) not yet embedded for the configured model, embeds them in batches via `EmbedService` → `domain.Embedder` (Ollama), and stores L2-normalized `[]float32` vectors as little-endian blobs in the `embeddings` table (brute-force cosine search — sufficient under ~50k vectors; a `VectorSearch`-style port swap to `coder/hnsw` is the intended path past that). `ReplaceSession` preserves embeddings across a re-index when a message's `(seq, sha256(text))` is unchanged (cascade-deletes them otherwise via FK). `chv ask` embeds the question, does brute-force `Nearest` (deduped, max 3 hits/session), expands ±2 neighboring blocks per hit, and — unless `--no-llm` — asks `domain.ChatModel` (Ollama) to answer citing `[n]`. `chv patterns` clusters embeddings (`adapter/cluster` k-means++, auto-`k` via approximate silhouette) for skill candidates (repeated user-prompt intents) and, for script candidates, both clusters embedded Bash commands and exact-matches Bash-command n-grams (2–4, no embeddings needed) — ranked by distinct sessions, labeled by the chat model unless `--no-llm`.

## What does NOT exist (and should not be created)
- No HTTP server, no API, no remote sync — except outbound calls to a local Ollama server for `embed`/`ask`/`patterns`
- No ORM — raw SQL against SQLite
- No cache layer beyond content-hash skip (`file_hashes`) and the `embeddings.text_hash` reattach-on-reindex check
- No config UI — config is `index.json` + CLI flags
- No vector DB / external embedding API — brute-force cosine in SQLite, Ollama only
