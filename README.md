# chv — Claude Code, Claude Desktop, Cursor, OpenCode & Codex History Viewer

Globally search across Claude Code, Claude Desktop, Cursor, OpenCode, and Codex session transcripts, typed prompts, and plan documents. Find what you said, which session it was in, and browse the full conversation.

## Install

```sh
brew install primissus/tap/chv
```

## Build

```sh
go build -o chv ./cmd/chv
# or
make build
make install   # installs to $GOPATH/bin
```

Requires Go 1.26+. No CGO — pure Go SQLite (`modernc.org/sqlite`).

## Releases

Pushing a `vX.Y.Z` tag triggers GitHub Actions (GoReleaser): it cross-compiles Darwin/Linux `amd64`/`arm64` archives, publishes the GitHub release, and updates the `primissus/homebrew-tap` cask. The Homebrew tap push needs a `HOMEBREW_TAP_GITHUB_TOKEN` repo secret.

## First run

```sh
chv index          # scan Claude Code, Claude Desktop, Cursor, OpenCode, and Codex sources
chv search "term"  # search immediately from the CLI
chv view ./chat.jsonl # open one transcript without indexing
chv                # launch interactive TUI
```

## Local RAG & pattern mining (optional)

`chv embed`, `chv search --semantic`, `chv ask`, `chv patterns`, and `chv summarize` add semantic search, question-answering, usage-pattern mining, and session recaps over your own indexed history — entirely local, via [Ollama](https://ollama.com). `chv mcp` exposes the same search/summarize surface to MCP clients (e.g. Claude Code) over stdio.

```sh
ollama pull mxbai-embed-large   # embedding model (1024d, default)
ollama pull qwen3.6:35b-a3b     # chat model (default; override via --chat or config)

chv index --deep    # Bash tool_use blocks are only indexed at --deep
chv embed           # embed all not-yet-embedded eligible message blocks
chv ask "when did I set up the homebrew tap"
chv patterns --kind all --top 10 --out ./candidates
```

Requires an Ollama server reachable at `http://localhost:11434` (override with `--ollama` or `ollama_url` in `index.json`). Nothing else leaves your machine.

The index DB is stored at `${XDG_DATA_HOME:-~/.local/share}/chv/chv.db`.

## Commands

### `chv index` / `chv i`

Scans **Claude Code** transcripts, **Claude Desktop** agent-session metadata, typed prompts, and plan markdown, plus **Cursor** agent transcripts and Cursor plan files, **OpenCode** sessions, and **Codex** rollout transcripts, into the FTS5 search index. Progress is printed to stderr (`Indexing N/M…`); a summary goes to stdout:

```
Sessions: 42  Orphaned: 3  Plans: 2  Skipped: 10  Messages: 1284  Elapsed: 1.2s
```

| Source | Vendor | Record kind |
|--------|--------|-------------|
| `~/.claude/projects` JSONL + sidechains | `claude` | chat |
| `~/Library/Application Support/Claude/claude-code-sessions/**/local_*.json` linked by `cliSessionId` to `~/.claude/projects` JSONL | `claude-desktop` | chat |
| `~/.claude/history.jsonl` (orphaned prompts) | `claude` | chat |
| `PLAN.md` / `PROGRESS.md` under `~/.claude` + `index.json` dirs | `claude` | plan |
| `~/.cursor/projects/.../agent-transcripts` | `cursor` | chat |
| `~/.cursor/plans/*.plan.md` | `cursor` | plan |
| `~/.local/share/opencode/opencode.db` (XDG-aware, read-only) | `opencode` | chat |
| `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl` | `codex` | chat |

Session IDs are namespaced to avoid collisions: Claude Code chats use the transcript UUID; Claude Desktop agent sessions reuse the linked Claude Code transcript UUID so they replace the plain Claude Code metadata instead of duplicating the same conversation; Claude plans use `plan:<hash>`; Cursor chats use `cursor:<chat-id>`; Cursor plans use `cursor-plan:<hash>`; OpenCode sessions use `opencode:<session-id>`; Codex threads use `codex:<thread-id>`.

**Upgrading** — schema changes (e.g. adding `vendor`) are applied automatically when you run `chv index`. If you upgraded chv and the TUI errors on an old DB, run `chv index` once before searching.

Plan files (`PLAN.md`, `PROGRESS.md` by default) are discovered with [`fd`](https://github.com/sharkdp/fd) under `~/.claude` and any directories listed in the index config. The scanner matches basenames case-insensitively, skips `*.original.md`, and falls back to a Go directory walk if `fd` is not installed. Cursor plans are scanned from `~/.cursor/plans/*.plan.md` (title from YAML frontmatter `name`, then `# Heading`, then filename). Content-hash skip avoids re-indexing unchanged plan files, and transcript fingerprints avoid re-parsing unchanged chat JSONL files (`Skipped` count). Discovered plan files are archived to `${XDG_DATA_HOME:-~/.local/share}/chv/manual/`.

**Index config** — `${XDG_CONFIG_HOME:-~/.config}/chv/index.json`:

```json
{
  "directories": [
    "~/src/my-project",
    "~/src/other-repo"
  ],
  "patterns": ["PLAN.md", "PROGRESS.md"],
  "embed_model": "mxbai-embed-large",
  "chat_model": "qwen3.6:35b-a3b",
  "ollama_url": "http://localhost:11434"
}
```

`directories` lists extra project roots to scan (always in addition to `~/.claude`). `patterns` are basename matches, case-insensitive. `embed_model`, `chat_model`, and `ollama_url` set the defaults used by `chv embed`/`ask`/`patterns` (all optional — shown values are also the built-in defaults); CLI flags override them. Missing config file uses empty directories and default patterns.

**Cron** — install a default every-4-hours index job (Go crontab helpers, invoked via Make):

```sh
make install-cron      # builds chv, adds 0 */4 * * * job
make uninstall-cron
```

Override the binary or schedule:

```sh
make install-cron CHV_BIN=/path/to/chv
go run ./cmd/cronctl install --chv ./chv --schedule "0 */3 * * *"
```

Add `--embed` to also run `chv embed` after each indexing pass (requires Ollama running):

```sh
go run ./cmd/cronctl install --chv ./chv --embed
```

Logs append to `${XDG_DATA_HOME:-~/.local/share}/chv/index.log`. The crontab line is tagged `# chv-managed` so re-install replaces any prior chv entry.

**Manual single-file index** — index one markdown/text file without a full scan:

```sh
chv index ./PLAN.md              # index only this file
chv i ./PROGRESS.md "Sprint 3"   # explicit title (alias: i)
```

Title resolution for manual index (in order): positional title if given, first `# Heading` in the file, otherwise a random two-word code-name (e.g. `happy-otter`). Manual files appear as `[plan]` in search and the TUI.

The source file is copied to `${XDG_DATA_HOME:-~/.local/share}/chv/manual/` so content survives if the original is deleted. Re-indexing the same path overwrites the archive copy.

Flags must come before positional args. These flags apply to both full scans and single-file indexing:

```
--db PATH          override DB file location
--force            re-index even if content unchanged
--debug            print indexing diagnostics to stderr
```

These flags only affect full scans:

```
--config PATH      index config JSON (default: XDG chv/index.json)
--shrink-cap N     rune cap for tool payloads (default 2000)
--no-shrink        disable payload truncation (indexes tool blocks verbatim)
--quick            index user text, tool results, and plans only
--deep             index all conversation content (default; explicit)
```

Index depth:

- **`--quick`** — user prompts, tool results, plans. Skips assistant text, thinking, tool-use args.
- **`--deep`** (default) — all block kinds. Images still dropped; tool payloads still respect `--shrink-cap`.

Rerunning `chv index` is safe and idempotent. Unchanged transcript and plan files are skipped on later runs; changed sessions are fully replaced, so counts stay stable across reruns. Use `--force` to rebuild even when fingerprints match, and `--debug` to see discovery, skip, indexing, and error diagnostics.

During indexing, tool-use and tool-result payloads are truncated to `--shrink-cap` runes (when shrink is enabled). Images are always dropped. Text and thinking blocks are kept verbatim (deep mode only for thinking).

### `chv view <path.jsonl>` / `chv v <path.jsonl>`

Opens a single Claude Code, Cursor, or Codex transcript JSONL directly in the thread TUI. This does not require the index DB and does not write anything to the index:

```sh
chv view "/Users/me/.claude/projects/-Users-me-src-app/session-id.jsonl"
```

By default chv auto-detects the transcript format from the first valid JSONL record. Use `--format claude`, `--format cursor`, or `--format codex` to force a parser.

```
--format FORMAT   auto, claude, cursor, or codex (default auto)
--shrink-cap N    rune cap for tool payloads (default 2000)
--no-shrink       disable payload truncation (tool blocks verbatim)
```

In view-only mode, `esc` clears in-thread search first, then quits.

### `chv search <query>`

Non-interactive ranked search. Prints session ID, title, and a snippet per hit:

```
a1b2c3d4-…  Fix login redirect [prompt-only]
  …matched snippet…
```

```
--db PATH       override DB file location
--limit N       max results (default 20)
--json          output results as JSON (array of SearchHit objects)
--fuzzy         fuzzy-match all bare terms (FTS mode only)
--semantic      semantic (embedding) search instead of full-text search
--vendor NAME   filter by vendor (--semantic only)
--project PATH  filter by project path (--semantic only)
--since DUR     only messages since duration ago, e.g. 30d (--semantic only)
```

`--fuzzy` cannot be combined with `--semantic` (usage error), and `--vendor`/`--project`/`--since` require `--semantic` — plain FTS search has no filters and passing them without `--semantic` is also a usage error.

**Query syntax** (compiled to FTS5, FTS mode only):

| Syntax | Meaning |
|--------|---------|
| `term1 term2` | AND (both in same message) |
| `term1 + term2` or `term1 AND term2` | explicit AND |
| `term1 \| term2` or `term1 OR term2` | OR |
| `(expr) term3` | grouping, e.g. `(foo + bar) baz` |
| `"exact phrase"` | phrase match |
| `-term` or `NOT term` | NOT |
| `term~` | fuzzy one term (prefix + edit-distance-1) |
| `~query` | fuzzy all terms (CLI: also `--fuzzy`) |

Fuzzy = prefix + edit-distance-1 variants, not full typo tolerance. AND still requires terms in the **same indexed message row** (not across messages in a session).

Sessions without a transcript on disk are shown with a `[prompt-only]` tag. Plan files appear with `[plan]`. Results include their source label: `[Claude Code]`, `[Claude Desktop]`, `[Cursor]`, `[OpenCode]`, or `[Codex]`.

**`--semantic`** runs `chv ask`'s retrieval step without the chat-model answer: it embeds the query and returns the nearest message per session (deduped, ranked by cosine similarity). It shares the embedding-model/Ollama flags (`--model`, `--ollama`) with `chv embed`/`ask` and reuses the same `SearchHit` JSON shape as FTS search, with `Score` populated.

**Score semantics differ by mode** — this matters when reading `--json` output or comparing runs:

| Mode | Score | Direction |
|------|-------|-----------|
| FTS (default) | BM25 | **lower is better** |
| `--semantic` | cosine similarity | **higher is better** |

### `chv embed`

Embeds indexed message blocks with a local Ollama model, storing vectors in the `embeddings` table. Eligible blocks: user and assistant `text`, and Bash `tool_use` commands (assistant text truncated to 2000 runes; blocks under 15 runes are skipped). Resumable — interrupting only loses the in-flight batch, since embeddings commit per batch; rerunning only embeds what's still missing.

```
--db PATH       override DB file location
--model NAME    embedding model (default: index.json embed_model, else mxbai-embed-large)
--ollama URL    Ollama base URL (default: index.json ollama_url, else http://localhost:11434)
--batch N       embed batch size (default 32)
--vendor NAME   filter by vendor
--project PATH  filter by project path
--force         re-embed even if already embedded
--limit N       max units to embed (0 = no limit)
```

Re-indexing a session (`chv index`) preserves embeddings for message blocks whose text is unchanged, so a routine reindex doesn't force re-embedding everything.

### `chv ask <question>`

Semantic search + retrieval-augmented answer over your own indexed history. Embeds the question, retrieves the nearest message blocks (deduped, max 3 per session), pulls ±2 neighboring blocks for context, and asks the chat model to answer citing `[n]` excerpt numbers.

```
--db PATH       override DB file location
--model NAME    embedding model
--chat NAME     chat model (default: index.json chat_model, else qwen3.6:35b-a3b)
--ollama URL    Ollama base URL
--k N           number of hits (default 12)
--vendor NAME   filter by vendor
--project PATH  filter by project path
--since DUR     only messages since duration ago, e.g. 30d
--no-llm        semantic search only — print hits, skip the chat model
--json          output JSON
```

### `chv patterns`

Mines the embedded corpus for repeated behavior and drafts candidates:

- **Skill candidates** — clusters repeated user-prompt intents (k-means over embeddings), then asks the chat model to draft a name, intent, trigger phrases, and outline per cluster.
- **Script candidates** — exact-match Bash command n-grams (2–4 consecutive commands within a session) plus embedding clusters of single commands (catches the same command with different flags), ranked by how many distinct sessions repeat them; trivial single commands (`ls`, `cat`, `pwd`, `git status`) are dropped.

```
--db PATH       override DB file location
--model NAME    embedding model
--chat NAME     chat model
--ollama URL    Ollama base URL
--kind KIND     skills|scripts|all (default all)
--k N           cluster count (0 = auto-pick via approximate silhouette)
--top N         max candidates per kind (default 15)
--vendor NAME   filter by vendor
--project PATH  filter by project path
--since DUR     only messages since duration ago, e.g. 30d
--out DIR       write one SKILL-CANDIDATE-<slug>.md / SCRIPT-CANDIDATE-<slug>.md file per candidate
--json          output JSON instead of markdown
--no-llm        rank and report clusters without chat-model labeling
```

### `chv summarize <session-id | path.jsonl>`

Condenses a session's transcript deterministically (no chat model — see "condensed transcript" below), then, unless `--no-llm`, asks the chat model for a five-section recap (`Goal`, `What was done`, `Key decisions`, `Files / areas touched`, `Outcome & open items`). The recap prints to stdout; a one-line status (model + cached/fresh + date) goes to stderr. The positional argument is either a session ID already in the index, or a path ending in `.jsonl` loaded directly (no indexing required, mirroring `chv view`).

```
--db PATH        override DB file location
--model NAME     embedding model (unused by summarize itself; shared flag)
--chat NAME      chat model (default: index.json chat_model, else qwen3.6:35b-a3b)
--ollama URL     Ollama base URL
--refresh        bypass the cached summary and regenerate
--no-llm         condense only — print the condensed transcript, skip the chat model
--json           output JSON (SummarizeResult)
--max-chars N    condensed-text budget in runes (0 = default, 24000)
--format FORMAT  transcript format for a .jsonl arg: auto, claude, cursor, or codex
```

**Caching** — summaries are cached in a `summaries` SQLite table keyed by `(session_id, chat_model)`: summarizing the same session with the same chat model again returns the cached recap instead of calling Ollama. The cache auto-invalidates when the session's content changes: each cached row stores a `source_hash` (SHA-256 of the condensed transcript), and a cache hit only counts if the hash still matches what the session condenses to today (e.g. after a re-index picked up edited/new messages). `--refresh` bypasses the cache read and always regenerates, overwriting the cached row. The cache only applies to a session ID already in the index — summarizing a raw `.jsonl` path also caches, but only when a DB already exists on disk (summarizing a file never creates a DB as a side effect).

`--no-llm` skips both the chat model call and the cache: it prints the condensed transcript (the same deterministic digest that would otherwise be sent to the chat model) and nothing else on stdout.

### `chv mcp`

Runs chv as an MCP ([Model Context Protocol](https://modelcontextprotocol.io)) stdio server, exposing search and summarize over an MCP transport for clients like Claude Code.

```
--db PATH     override DB file location
--model NAME  embedding model (for semantic_search)
--chat NAME   chat model (for summarize_session)
--ollama URL  Ollama base URL
--no-llm      don't construct a chat-capable summarizer; summarize_session only works with condensed_only:true
--debug       log server activity to stderr
```

Tools exposed:

| Tool | Purpose |
|------|---------|
| `search_history` | Full-text (FTS5) search over indexed sessions. |
| `semantic_search` | Embedding-based nearest-neighbor search (requires `chv embed` to have run). |
| `list_sessions` | Browse recent sessions, optionally filtered by vendor/project/kind. |
| `get_session` | Fetch a session's messages, paginated and filterable by kind. |
| `summarize_session` | Condense a session's transcript, optionally with a cached LLM-generated recap. |

`semantic_search` and `summarize_session` (unless called with `condensed_only: true`) need a reachable Ollama server — same requirement as `chv search --semantic`/`chv ask`/`chv summarize`.

**Register with Claude Code:**

```sh
claude mcp add chv -- chv mcp
```

Or via a project `.mcp.json`:

```json
{
  "mcpServers": {
    "chv": {
      "command": "chv",
      "args": ["mcp"]
    }
  }
}
```

**stdout is the protocol channel** — in `chv mcp` mode, chv writes nothing else to stdout; all logs and diagnostics go to stderr (enable with `--debug`). A stray write to stdout anywhere in the tool-call path would corrupt the protocol stream and break the client.

### `chv` (no subcommand)

Launches the interactive TUI. Exits with a helpful message if the DB hasn't been created yet.

On launch, the TUI shows the **100 most recent** sessions (all vendors unless filters are active). That list is **not** the full index — press `/` to search **all indexed** chats and plans via FTS5 (up to **500** unique sessions per query). TUI search uses the same query syntax as `chv search`; use `term~` or a leading `~query` for fuzzy matching. Search queries are saved to `${XDG_DATA_HOME:-~/.local/share}/chv/search_history` (last 100, deduplicated).

Press `f` to filter by **path**, **type** (chat/plan), or **vendor** (Claude Code/Claude Desktop/Cursor/OpenCode/Codex) when the current result list is non-empty. Default is all vendors. On the home view, path/type/vendor filters are applied server-side; on search results they filter client-side. Press `g` to group the displayed results by path, vendor, vendor+path, or off. Press `c` on the results view to clear a search and return to recent threads; press `c` inside the filter modal to clear only the active category without closing the modal.

Set `CHV_NO_COLOR_QUERY=1` to skip terminal palette detection and use ANSI fallback colors.

`ctrl+c` quits immediately. `q` and `esc` ask for confirmation (`Quit chv? [y/N]`).

Press `?` anywhere for an in-app shortcut reference.

## Claude Code skill: chv-summarize

`skills/chv-summarize/` is a Claude Code skill that has the calling agent write a session recap itself, using `chv summarize --no-llm` only for the condensed transcript (no local chat model involved). See `skills/chv-summarize/SKILL.md` for the trigger phrases and install instructions (`ln -s`).

## TUI keybindings

### Results view

| Key | Action |
|-----|--------|
| `/` | Focus search input |
| `enter` (in input) | Run search |
| `↑` / `↓` (in input) | Previous / next search from history |
| `esc` (in input) | Cancel search input |
| `j` / `↓` | Next result |
| `k` / `↑` | Previous result |
| `enter` (on result) | Open session detail |
| `f` | Open filters (path / type / vendor) |
| `t` | Cycle type filter (all → chat → plan) |
| `s` | Cycle sort (relevance → last msg → created → project → thread; also works on recent threads) |
| `g` | Cycle group (off → path → vendor → vendor+path) |
| `c` | Clear search and return to recent threads |
| `S` | Copy session ID to clipboard |
| `P` | Copy project path to clipboard |
| `shift+F` / `F` | Copy transcript/plan file path to clipboard |
| `space` | Toggle selection of the item under the cursor |
| `ctrl+a` | Select all currently-listed hits, or deselect all if all are selected |
| `shift+Y` / `Y` | Copy paths for the selection (or just the cursor item if nothing is selected) |
| `?` | Show keyboard shortcuts |
| `q` | Quit (with confirmation) |
| `esc` | Clear the selection if non-empty; otherwise quit (with confirmation) |
| `ctrl+c` | Quit immediately |

`shift+Y` copies one line per session as `FilePath<TAB>ProjectPath`, newline-joined; sessions with no file path are skipped.

### Filter view

Opened with `f` from a non-empty results list. Switch categories with `←`/`→`, `h`/`l`, `tab`, or `shift+tab`.

| Key | Action |
|-----|--------|
| `←` / `→` / `h` / `l` / `tab` / `shift+tab` | Switch filter category (path / type / vendor) |
| `j` / `↓` / `k` / `↑` | Move selection |
| `u` / `d` | Page up / down |
| `enter` | Apply filter and return to results |
| `c` | Clear active category (stay in filter view) |
| `esc` | Close filter view without applying |
| `q` | Quit (with confirmation) |

### Detail view

| Key | Action |
|-----|--------|
| `/` | In-thread search |
| `n` / `N` | Next / previous in-thread match |
| `ctrl+o` | Expand / collapse tool, thinking, and long message blocks |
| `m` | Cycle message type filter (all → you → task → claude → thinking → tool → result → sidechain) |
| `T` | Cycle message time format (local → UTC → date → off) |
| `j` / `↓` | Scroll down one line |
| `k` / `↑` | Scroll up one line |
| `d` / `u` | Half-page down / up |
| `g` / `G` | Jump to top / bottom |
| `S` | Copy session ID to clipboard |
| `P` | Copy project path to clipboard |
| `shift+F` / `F` | Copy transcript/plan file path to clipboard |
| `?` | Show keyboard shortcuts |
| `esc` | Leave active in-thread search input, clear an existing search, or back to results |
| `q` | Quit (with confirmation) |
| `ctrl+c` | Quit immediately |

Clipboard copy uses `pbcopy` on macOS and `xclip` on Linux.

Sidechain (subagent) messages are tagged `[sidechain]` in the detail view. Delegated subagent prompts show as **Task** (not **You**); tool results in user envelopes show as **result**.

## Data sources

| Path | Contents |
|------|----------|
| `~/.claude/projects/<project>/<session-id>.jsonl` | Claude Code session transcripts |
| `~/.claude/projects/<project>/<session-id>/subagents/agent-*.jsonl` | Claude subagent sidechain transcripts |
| `~/.claude/history.jsonl` | Every Claude prompt you've typed (including pasted content) |
| `~/.claude/**/PLAN.md` and `~/.claude/**/PROGRESS.md` | Claude plan markdown discovered during full index |
| Configured `index.json` directories matching `PLAN.md` / `PROGRESS.md` | Additional Claude-style plan markdown |
| `~/.cursor/projects/<project>/agent-transcripts/<chat-id>/<chat-id>.jsonl` | Cursor agent chat transcripts |
| `~/.cursor/projects/<project>/agent-transcripts/<chat-id>/subagents/*.jsonl` | Cursor subagent sidechains |
| `~/.cursor/plans/*.plan.md` | Cursor plan markdown files |
| `~/.local/share/opencode/opencode.db` | OpenCode sessions (TUI + Desktop share one DB; opened read-only) |
| `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl` | Codex rollout transcripts |

Claude plan discovery scans `~/.claude` plus any `index.json` directories for configured basename patterns. It does not currently read Claude `plansDirectory` settings; add those directories to `index.json` if they live outside `~/.claude`.

Session titles: Claude transcripts use `custom-title` / `ai-title`; Cursor chats use the first user message (truncated); OpenCode sessions use the stored title (falling back to the first user message); Codex threads use the first user message; plans use file headings or frontmatter.

## Result tags

| Tag | Meaning |
|-----|---------|
| `[prompt-only]` | Claude history entry with no transcript file on disk |
| `[plan]` | Claude plan markdown |
| `[cursor]` | Cursor chat transcript |
| `[cursor plan]` | Cursor plan markdown |
| `[opencode]` | OpenCode session |
| `[codex]` | Codex rollout thread |

Combined tags appear in CLI output (e.g. `[cursor plan]`). The TUI shows similar badges on result titles.

## Orphaned sessions

`~/.claude/history.jsonl` records every prompt you've typed across all sessions. When a session's transcript file has been deleted (Claude Code prunes old ones), chv can still search its prompts — these appear with a `[prompt-only]` badge in the results list and a ⚠ banner in the detail view.

## File locations

| File | Default path |
|------|--------------|
| Search index DB | `~/.local/share/chv/chv.db` |
| TUI search history | `~/.local/share/chv/search_history` |
| Plan file archive | `~/.local/share/chv/manual/` |
| Index cron log | `~/.local/share/chv/index.log` |
| Index config | `~/.config/chv/index.json` |

Data files respect `$XDG_DATA_HOME`; the index config respects `$XDG_CONFIG_HOME`. Use `--db PATH` on `index` and `search` to override the DB location.

## Development

```sh
make test      # go test ./...
make check     # vet + test
make dev-setup # install air
make dev       # hot-reload via air
```
