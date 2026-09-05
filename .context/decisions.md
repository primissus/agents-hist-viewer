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

## [embed] · SQLite blob vectors + brute-force cosine, no vector DB
- **Decision:** store `[]float32` vectors as little-endian blobs in an `embeddings` table (`internal/adapter/sqlite/embedding_repo.go`); nearest-neighbor search loads matching rows and computes cosine in Go, no ANN index.
- **Why:** scale on this machine is ~10-40k embeddable blocks; brute-force cosine is <50ms at 50k×1024 — a vector DB (Postgres/pgvector) or ANN library adds a daemon or dependency for no measurable benefit at this scale. `Nearest`/`EmbeddingsWithContext` sit behind `domain.EmbeddingRepository` so a `coder/hnsw`-backed adapter can replace brute force later without touching `EmbedService`/`AskService`/`PatternService`.
- **Rejected:** Postgres+pgvector (daemon, no benefit under ~1M vectors), an ANN library from day one (premature), a separate vector-only sidecar DB.
- **Status:** current

## [embed] · Ollama as the one allowed local HTTP dependency
- **Decision:** `internal/adapter/ollama` implements `domain.Embedder` and `domain.ChatModel` against a local Ollama server (`http://localhost:11434` by default); default models `mxbai-embed-large` (1024d embeddings) and `qwen3.6:35b-a3b` (chat), overridable via flags or `index.json`.
- **Why:** keeps `chv embed`/`ask`/`patterns` fully local and free (no API keys, no data leaving the machine), and Ollama was already the locally-available model runtime.
- **Rejected:** a cloud embedding/chat API (breaks the "your data stays local" property of the tool), bundling a model runtime into `chv` itself.
- **Status:** current

## [embed] · Embeddings survive re-index via (seq, text_hash) reattach
- **Decision:** `ReplaceSession` reads existing `embeddings` rows for a session's current messages before deleting them, and re-inserts the same vectors against the new rowids where `(seq, sha256(text))` still matches after re-insert; the FK `ON DELETE CASCADE` on `embeddings.message_rowid` cleans up everything else.
- **Why:** message rowids aren't stable across a full-replace re-index (SQLite reuses freed rowids), and re-embedding unchanged content on every cron re-index would be wasteful and slow.
- **Rejected:** a stable non-rowid message identity column (bigger schema change); always re-embedding after any re-index (simple but wasteful).
- **Status:** current

## [embed] · Pure-Go k-means++ over L2-normalized vectors, no clustering library
- **Decision:** `internal/adapter/cluster` implements k-means++ init + Lloyd's algorithm using squared Euclidean distance on L2-normalized vectors (equivalent to maximizing cosine similarity for unit vectors), plus an approximate silhouette score to auto-pick `k` from a candidate set.
- **Why:** keeps the "pure Go, no heavy deps" constraint; the corpus size here (thousands, not millions, of vectors) doesn't need a specialized clustering library, and the approximation is only used to rank a handful of candidate `k` values, not for final cluster quality guarantees.
- **Rejected:** a Go ML/clustering package, computing exact (full pairwise) silhouette.
- **Status:** current
