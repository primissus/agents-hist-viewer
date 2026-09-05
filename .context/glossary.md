# Glossary & entities

## Domain terms
- **Session** → one chat conversation (Claude Code/Desktop/Cursor) or one plan document, one row in `sessions`
- **Transcript** → the JSONL file backing a chat session (e.g. `~/.claude/projects/<proj>/<id>.jsonl`)
- **Block** → a parsed unit inside a message: `text`, `thinking`, `tool_use`, `tool_result`, `image` (`BlockKind`)
- **Message** → one UUID-addressed block with role (`user`/`assistant`), kind, timestamp, sequence; the search unit in `messages`
- **Sidechain** → Cursor subagent transcript messages (`subagents/*.jsonl`) merged into the parent session
- **Vendor** → product that produced the session: `claude` (default), `claude-desktop`, `cursor`
- **RecordKind** → `chat` vs `plan` — distinguishes conversations from plan documents in filters
- **Orphaned session** → session known only from `history.jsonl` typed prompts (no transcript file)
- **Shrink** → indexing transform: cap tool payloads (`ToolPayloadCap`, default 2000 runes), drop images
- **Depth** → `deep` (default: all block kinds) vs `quick` (user text + tool results + plans only)
- **Fingerprint** → content hash of a transcript/plan file used to skip unchanged files
- **FTS / FTS5** → SQLite full-text search virtual table (`messages_fts`), tokenizer `unicode61 remove_diacritics 2`
- **BM25** → default FTS5 ranking used for search ordering
- **Embed unit** → a message block selected as a candidate for embedding: user/assistant `text`, or a Bash `tool_use` command (`domain.EmbedUnit`)
- **Embedding** → an L2-normalized `[]float32` vector for one embed unit under a given model, stored in the `embeddings` table (`domain.Embedding`)
- **Cluster** → a group of embeddings produced by k-means++ (`internal/adapter/cluster`), used by `chv patterns` to find repeated intents/commands
- **Candidate** (skill/script) → a ranked, chat-model-labeled cluster or command n-gram surfaced by `chv patterns`, optionally written as `SKILL-CANDIDATE-<slug>.md` / `SCRIPT-CANDIDATE-<slug>.md`
- **RAG** → retrieval-augmented generation: `chv ask` retrieves relevant embed units, then asks a chat model to answer citing them
- **Ollama** → the local model runtime chv talks to for embeddings (`mxbai-embed-large` default) and chat (`qwen3.6:35b-a3b` default), via `internal/adapter/ollama`

## Main entities
- **Session** → `id, title, project_path, git_branch, started_at, ended_at, message_count, file_path, has_transcript, record_kind, vendor`
- **Message** → `uuid, parent_uuid, session_id → Session, role, kind, tool_name, source, timestamp, seq, is_sidechain, text`
- **Prompt** → typed prompt from `history.jsonl` (`session_id, project, text, timestamp, seq`)
- **Plan** → plan markdown (`id, title, project_path, file_path, content, mod_time, vendor`)
- **SearchHit** → flattened search result: session meta + message uuid/role/kind/snippet/score
- **SessionDetail** → `Session` + `[]Message`, built by `ViewService` directly from JSONL (no DB)

## Acronyms & internal names
- **chv** → the tool itself; binary name and XDG dir (`~/.local/share/chv/`)
- **index.json** → user config listing extra plan directories to index (XDG)
- **file_hashes** → SQLite table: `path → (session_id, content_hash, updated_at)` for skip logic
- **RecentQuery** → home-view query with `Limit, RecordKind, ProjectPath, Vendor` filters
- **palette** → TUI package that derives a terminal color scheme (`adapter/tui/palette`)
