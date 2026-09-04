# chv — Claude Code, Claude Desktop & Cursor History Viewer

Globally search across Claude Code, Claude Desktop, and Cursor session transcripts, typed prompts, and plan documents. Find what you said, which session it was in, and browse the full conversation.

## Build

```sh
go build -o chv ./cmd/chv
# or
make build
make install   # installs to $GOPATH/bin
```

Requires Go 1.26+. No CGO — pure Go SQLite (`modernc.org/sqlite`).

## First run

```sh
chv index          # scan Claude Code, Claude Desktop, and Cursor sources
chv search "term"  # search immediately from the CLI
chv view ./chat.jsonl # open one transcript without indexing
chv                # launch interactive TUI
```

The index DB is stored at `${XDG_DATA_HOME:-~/.local/share}/chv/chv.db`.

## Commands

### `chv index` / `chv i`

Scans **Claude Code** transcripts, **Claude Desktop** agent-session metadata, typed prompts, and plan markdown, plus **Cursor** agent transcripts and Cursor plan files, into the FTS5 search index. Progress is printed to stderr (`Indexing N/M…`); a summary goes to stdout:

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

Session IDs are namespaced to avoid collisions: Claude Code chats use the transcript UUID; Claude Desktop agent sessions reuse the linked Claude Code transcript UUID so they replace the plain Claude Code metadata instead of duplicating the same conversation; Claude plans use `plan:<hash>`; Cursor chats use `cursor:<chat-id>`; Cursor plans use `cursor-plan:<hash>`.

**Upgrading** — schema changes (e.g. adding `vendor`) are applied automatically when you run `chv index`. If you upgraded chv and the TUI errors on an old DB, run `chv index` once before searching.

Plan files (`PLAN.md`, `PROGRESS.md` by default) are discovered with [`fd`](https://github.com/sharkdp/fd) under `~/.claude` and any directories listed in the index config. The scanner matches basenames case-insensitively, skips `*.original.md`, and falls back to a Go directory walk if `fd` is not installed. Cursor plans are scanned from `~/.cursor/plans/*.plan.md` (title from YAML frontmatter `name`, then `# Heading`, then filename). Content-hash skip avoids re-indexing unchanged plan files, and transcript fingerprints avoid re-parsing unchanged chat JSONL files (`Skipped` count). Discovered plan files are archived to `${XDG_DATA_HOME:-~/.local/share}/chv/manual/`.

**Index config** — `${XDG_CONFIG_HOME:-~/.config}/chv/index.json`:

```json
{
  "directories": [
    "~/src/my-project",
    "~/src/other-repo"
  ],
  "patterns": ["PLAN.md", "PROGRESS.md"]
}
```

`directories` lists extra project roots to scan (always in addition to `~/.claude`). `patterns` are basename matches, case-insensitive. Missing config file uses empty directories and default patterns.

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

Opens a single Claude Code or Cursor transcript JSONL directly in the thread TUI. This does not require the index DB and does not write anything to the index:

```sh
chv view "/Users/me/.claude/projects/-Users-me-src-app/session-id.jsonl"
```

By default chv auto-detects the transcript format from the first valid JSONL record. Use `--format claude` or `--format cursor` to force a parser.

```
--format FORMAT   auto, claude, or cursor (default auto)
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
--db PATH     override DB file location
--limit N     max results (default 20)
--json        output results as JSON (array of SearchHit objects)
--fuzzy       fuzzy-match all bare terms
```

**Query syntax** (compiled to FTS5):

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

Sessions without a transcript on disk are shown with a `[prompt-only]` tag. Plan files appear with `[plan]`. Results include their source label: `[Claude Code]`, `[Claude Desktop]`, or `[Cursor]`.

### `chv` (no subcommand)

Launches the interactive TUI. Exits with a helpful message if the DB hasn't been created yet.

On launch, the TUI shows the **100 most recent** sessions (all vendors unless filters are active). That list is **not** the full index — press `/` to search **all indexed** chats and plans via FTS5 (up to **500** unique sessions per query). TUI search uses the same query syntax as `chv search`; use `term~` or a leading `~query` for fuzzy matching. Search queries are saved to `${XDG_DATA_HOME:-~/.local/share}/chv/search_history` (last 100, deduplicated).

Press `f` to filter by **path**, **type** (chat/plan), or **vendor** (Claude Code/Claude Desktop/Cursor) when the current result list is non-empty. Default is all vendors. On the home view, path/type/vendor filters are applied server-side; on search results they filter client-side. Press `g` to group the displayed results by path, vendor, vendor+path, or off. Press `c` on the results view to clear a search and return to recent threads; press `c` inside the filter modal to clear only the active category without closing the modal.

Set `CHV_NO_COLOR_QUERY=1` to skip terminal palette detection and use ANSI fallback colors.

`ctrl+c` quits immediately. `q` and `esc` ask for confirmation (`Quit chv? [y/N]`).

Press `?` anywhere for an in-app shortcut reference.

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
| `?` | Show keyboard shortcuts |
| `q` / `esc` | Quit (with confirmation) |
| `ctrl+c` | Quit immediately |

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

Claude plan discovery scans `~/.claude` plus any `index.json` directories for configured basename patterns. It does not currently read Claude `plansDirectory` settings; add those directories to `index.json` if they live outside `~/.claude`.

Session titles: Claude transcripts use `custom-title` / `ai-title`; Cursor chats use the first user message (truncated); plans use file headings or frontmatter.

## Result tags

| Tag | Meaning |
|-----|---------|
| `[prompt-only]` | Claude history entry with no transcript file on disk |
| `[plan]` | Claude plan markdown |
| `[cursor]` | Cursor chat transcript |
| `[cursor plan]` | Cursor plan markdown |

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
