---
name: chv-summarize
description: Summarize or recap a past Claude Code / Cursor / OpenCode / Codex session indexed by chv. Trigger on phrases like "summarize that session", "recap what I did in <session-id>", "what happened in the conversation about X", or when the user pastes a chv session id.
---

# chv-summarize

Produce a five-section recap of a past coding-agent session using `chv`'s
condensed transcript — written by you (the calling agent), not by a local
LLM.

## Steps

1. **Resolve the session.**
   - If the user gave a session id directly, use it as-is.
   - Otherwise, run `chv search "<terms>" --limit 5` to find candidates. If
     FTS search returns nothing useful, fall back to
     `chv search "<terms>" --semantic --limit 5`.
   - If more than one result plausibly matches, ask the user to disambiguate
     before continuing.

2. **Get the condensed transcript.** Run:

   ```
   chv summarize <id> --no-llm --max-chars 80000
   ```

   Read its **stdout** — that's the condensed transcript, ready to summarize.
   If stdout instead shows something like `DB not found`, tell the user to
   run `chv index` first and stop.

   **Never** run `chv summarize` without `--no-llm` from within this skill.
   Omitting `--no-llm` would call the user's local Ollama chat model to do
   the summarizing — the whole point of this skill is that *you* (the
   calling agent) write the summary instead, using the condensed transcript
   only as input.

3. **Write the summary yourself**, based on the condensed transcript from
   step 2, using exactly these five `##` sections, in this order:

   ```
   ## Goal
   ## What was done
   ## Key decisions
   ## Files / areas touched
   ## Outcome & open items
   ```

   Use bullets, be concrete (name files, commands, decisions), never invent
   facts not present in the transcript, and write "None noted." for a
   section that's genuinely empty. Reply inline, or write to a file if the
   user asked for one.

## Install

```
ln -s "$PWD/skills/chv-summarize" ~/.claude/skills/chv-summarize
```

Requires `chv` on `PATH` (`make install` in the `chv` repo, or `go install
./cmd/chv`).
