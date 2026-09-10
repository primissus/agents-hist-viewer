# Agent instructions — chv

Claude Code, Cursor, OpenCode & Codex History Viewer (`chv`): Go CLI + Bubble Tea TUI. Indexes Claude Code, Cursor, OpenCode, and Codex transcripts, typed prompts, and plan markdown → SQLite FTS5 for global search.

User docs: [README.md](README.md). File for coding agents.

## Project context

- Architecture → .context/architecture.md
- Conventions → .context/conventions.md
- Decisions → .context/decisions.md
- Glossary → .context/glossary.md
- Workflow → .context/workflow.md
- Known issues → .context/known-issues.md

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

Entry: `cmd/chv/main.go` — subcommands `index` (`i`), `search` (`--semantic` for embedding search), `view` (`v`), `embed`, `ask`, `patterns`, `summarize`, `mcp`, default TUI.

Releases are tag-driven, not local: push a `vX.Y.Z` tag and `.github/workflows/release.yml` runs GoReleaser (`.goreleaser.yml`) to build Darwin/Linux archives, publish the GitHub release, and push the cask to `primissus/homebrew-tap` (needs the `HOMEBREW_TAP_GITHUB_TOKEN` secret). GoReleaser sets the `version` var in `cmd/chv/main.go` via `-ldflags "-X main.version=..."`.

## Architecture

Hexagonal layout. Deps point inward.

```
cmd/chv/              CLI wiring, flags, stdout/stderr
internal/
  domain/             models (Vendor, RecordKind), ShrinkConfig, search query compiler, embedding types, port interfaces
  app/                IndexService, SearchService, EmbedService, AskService, PatternService, SemanticSearchService, SummarizeService (use cases)
  config/             XDG paths (DB, search history, index.json incl. embed/chat model + ollama_url, manual archive)
  adapter/
    transcript/       ~/.claude/projects JSONL
    cursor/transcript ~/.cursor/projects agent-transcripts JSONL
    cursor/plan       ~/.cursor/plans/*.plan.md
    codex/transcript  $CODEX_HOME/sessions rollout JSONL
    opencode/transcript opencode.db (read-only SQLite)
    history/          ~/.claude/history.jsonl
    plan/             Claude PLAN.md/PROGRESS.md (fd scan + index.json dirs)
    sqlite/           FTS5 repository + inline schema migrations + embeddings table CRUD/search
    ollama/           HTTP client: domain.Embedder + domain.ChatModel against a local Ollama server
    cluster/          pure-Go k-means++ over []float32 (for chv patterns)
    mcp/              MCP stdio server (5 tools) over internal/app, driving adapter — app never imports it
    tui/              Bubble Tea UI
skills/
  chv-summarize/      Claude Code skill: agent writes the recap from `chv summarize --no-llm`'s condensed transcript
```

**Ports** (`internal/domain/ports.go`):

- `TranscriptSource` — list sessions, stream messages
- `TranscriptFingerprintSource` — optional transcript content fingerprint for skip logic
- `PromptLog` — list typed prompts
- `PlanSource` — list plan file paths to index
- `SearchRepository` — init, replace session, file hash, search, recent, session detail, `SessionsByIDs` (batch session lookup for semantic-search hydration)
- `Embedder` — embed texts into vectors (Ollama)
- `ChatModel` — answer a system+user prompt (Ollama)
- `EmbeddingRepository` — embeddings CRUD, nearest-neighbor + clustering reads, Bash command reads, message-by-rowid/neighbors
- `SummaryRepository` — cached per-(session, chat model) summaries: init table, get, put

New I/O/storage backends implement these interfaces; logic stays in `internal/app`.

## Data flow

1. **Index** (`IndexService.Run`): union sessions from all `TranscriptSource` adapters + orphaned history-only sessions → `ReplaceSession` per ID (full replace, idempotent). Then index Claude plans (fd/config) and Cursor plans (`~/.cursor/plans`), with content-hash skip via `file_hashes`.
2. **Search** (`SearchService`): trims and compiles user query syntax (`AND`/`OR`/groups/phrases/NOT/fuzzy) to FTS5; empty query returns nil.
3. **View** (`ViewService`): validates/detects a single Claude/Cursor/Codex JSONL transcript and builds `SessionDetail` directly; no DB writes.
4. **TUI** (`tui.Model`): home loads 100 recent sessions (`RecentQuery` supports path/type/vendor filters); `/` search hits full FTS (limit 500 unique sessions); query history in `config.SearchHistoryPath()`. `NewDetailApp` opens a prebuilt thread for `chv view`.

Index applies `domain.Shrink` to tool payloads (configurable cap; images dropped). Deep mode indexes text/thinking verbatim; quick mode indexes user text, tool results, and plans.

**Vendors** — `domain.Vendor`: `claude` (default), `claude-desktop`, `cursor`, `opencode`, `codex`. Stored on `sessions.vendor`. Empty filter = all vendors. Session ID prefixes: `cursor:`, `cursor-plan:`, `opencode:`, `codex:`, `plan:`.

**Schema migrations** — `sqlite.Repo.Init()` runs `CREATE TABLE IF NOT EXISTS` plus `ALTER TABLE` for legacy DBs. Migrations must add columns before indexes on those columns. `Init` is called from `IndexService` on index; users upgrade via `chv index`.

5. **Embed** (`EmbedService.Run`): fetches not-yet-embedded eligible blocks (user/assistant text, Bash `tool_use`) via `EmbeddingRepository.UnembeddedUnits`, cleans/truncates text, embeds in batches via `domain.Embedder`, commits per batch (resumable). `sqlite.Repo.ReplaceSession` preserves embeddings across re-index when `(seq, sha256(text))` is unchanged; FK cascade removes the rest.
6. **Ask** (`AskService.Ask`): embeds the question, brute-force `Nearest` cosine search (deduped, max 3/session), expands ±2 neighbor blocks per hit, asks `domain.ChatModel` to answer citing `[n]` unless `--no-llm`.
7. **Patterns** (`PatternService.Run`): clusters embeddings (`adapter/cluster`, auto-`k` via approximate silhouette) for skill candidates (user-prompt intents) and script candidates (Bash command n-grams, exact-match + embedding clusters); ranks by distinct sessions, drops <3-session clusters and trivial single commands, labels via chat model unless `--no-llm`.
8. **Semantic search** (`SemanticSearchService.Search`, `chv search --semantic`): same retrieval as step 6 minus the chat-model answer — embeds the query, brute-force `Nearest` (deduped, 1 hit/session), hydrates hits via `SessionsByIDs`, returns `[]domain.SearchHit` with `Score` = cosine similarity.
9. **Summarize-cache write** (`SummarizeService.SummarizeDetail`, `chv summarize`): `Condense` builds a deterministic transcript digest, hashed (SHA-256) as `source_hash`. Unless `--no-llm`, a cache hit on `(session_id, chat_model)` with a matching `source_hash` short-circuits the chat call; otherwise it calls `domain.ChatModel` and `PutSummary`s the result (upsert on `(session_id, model)`), so a changed session (different condensed text → different hash) transparently misses the cache and regenerates.
10. **MCP tool call** (`internal/adapter/mcp`, `chv mcp`): `cmdMCP` wires `Deps{Search, Semantic, Summarize}` from the same services above and calls `mcpadapter.Run`, which starts an SDK stdio server; each of the 5 registered tools (`search_history`, `semantic_search`, `list_sessions`, `get_session`, `summarize_session`) is a thin handler translating tool-call input into the corresponding `app` service call and back into a JSON DTO — no logic lives in the adapter beyond that translation and clamping (limits, kinds) already tolerant of missing deps (e.g. Ollama down → `semantic_search`/`summarize_session` return a tool error, not a crash).

Config: `index.json` also holds `embed_model` / `chat_model` / `ollama_url` (`internal/config/index_config.go`, `*OrDefault()` methods) — defaults `mxbai-embed-large`, `qwen3.6:35b-a3b`, `http://localhost:11434`.

## Conventions

- **Go 1.26+**, module `claude-code-hist-viewer`, no CGO (`modernc.org/sqlite`).
- Match style: minimal comments, small focused diffs, no over-abstraction.
- Tests: fakes w/ domain ports + temp SQLite DBs (`internal/app/service_test.go`, `internal/adapter/sqlite/repo_test.go`). Prefer `testdata/` JSONL fixtures for parser tests.
- TUI keys + help text stay in sync: update `internal/adapter/tui/keys.go`, `help_view.go`, README on binding changes.
- CLI flags in `cmd/chv/main.go` only; doc changes in README.
- MCP: nothing writes to stdout outside the SDK transport.

## Package notes

| Area | Notes |
|------|-------|
| `adapter/transcript` | `~/.claude/projects/<proj>/<id>.jsonl`, sidechains. `VendorClaude`. Title from `custom-title`/`ai-title`. |
| `adapter/cursor/transcript` | `~/.cursor/projects/<proj>/agent-transcripts/<id>/<id>.jsonl` + `subagents/*.jsonl`. IDs `cursor:<id>`. Project path decoded from folder name. |
| `adapter/cursor/plan` | `~/.cursor/plans/*.plan.md`. IDs `cursor-plan:<hash>`. Title from frontmatter `name`, then `# Heading`, then filename. |
| `adapter/codex/transcript` | `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl` envelopes. IDs `codex:<thread-id>`. Title from first user text. `LoadFile` for `chv view`; size+mtime fingerprint. |
| `adapter/opencode/transcript` | `opencode.db` read-only via `file:<path>?mode=ro`. Root sessions only (`parent_id IS NULL`); children → `IsSidechain`. IDs `opencode:<ses_id>`. Fingerprint = `time_updated` + message count. |
| `adapter/history` | Resolves `[Pasted text …]` placeholders via `pastedContents`. |
| `adapter/plan` | fd/walk scan for Claude `PLAN.md`/`PROGRESS.md` under `~/.claude` + configured `index.json` directories; `PlanSource.PlanPaths`. Manual `FromFile` for `chv index <file>`. |
| `adapter/claudesettings` | Reads Claude `plansDirectory` settings but is not wired into indexing yet; document `index.json` for external plan directories. |
| `adapter/sqlite` | FTS5 + BM25; search dedupes by session. `RecentQuery` filters `record_kind`, `project_path`, `vendor`. `embedding_repo.go`: `embeddings` table (blob vectors, L2-normalized, little-endian float32), brute-force cosine `Nearest`, `EmbeddingsWithContext` for clustering, `BashUnits` for n-gram mining. |
| `adapter/ollama` | `POST /api/embed` (batch) + `/api/chat` (non-streaming); 3x retry on 5xx; clear error + `ollama pull` hint on missing model. |
| `adapter/cluster` | k-means++ init + Lloyd's on squared-Euclidean over unit vectors (≡ cosine); `PickK` auto-selects `k` from a candidate set via approximate silhouette on a sample. |
| `adapter/tui` | Views: results, detail, filter (path/type/vendor + pagination), help. Clipboard via `pbcopy` (darwin) / `xclip` (linux). Filter `esc` closes; `c` clears active category in-modal. Results view: `space`/`ctrl+a` multi-select, `Y` copies `FilePath<TAB>ProjectPath` lines for the selection (or cursor item). |
| `adapter/mcp` | `github.com/modelcontextprotocol/go-sdk` stdio server. `server.go`: `NewServer`/`Run`, `Deps` (Search/Semantic/Summarize services, Version, OllamaURL, optional Logger — must log to stderr). `tools.go`: 5 tools as testable `handlers` methods + DTOs. Driving adapter — imports `internal/app`, never imported by it. |

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
