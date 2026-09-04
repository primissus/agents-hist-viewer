# Workflow

## Before touching anything
1. Read `.context/decisions.md` — don't redo settled choices
2. Check `.context/known-issues.md` for traps in the area you're changing
3. Keep deps pointing inward: adapters → ports in `internal/domain`

## To make a change
1. Locate the layer: domain logic (`internal/domain`), use case (`internal/app`), I/O (`internal/adapter/*`), wiring (`cmd/chv`)
2. Write/extend tests: fakes from domain ports + temp SQLite DBs; `testdata/` JSONL fixtures for parser changes
3. Implement with minimal comments, matching existing style
4. Run `make check` (vet + test)

## Before calling something done
- [ ] `make check` passes
- [ ] `go build -o chv ./cmd/chv` compiles
- [ ] No `chv` binary or user DB files staged
- [ ] TUI key changes: update `internal/adapter/tui/keys.go`, `help_view.go`, and README together
- [ ] XDG path changes: update `internal/config/config.go` + README
- [ ] New adapter/package: note it in AGENTS.md package table (and `.context/architecture.md` if structure changed)

## Deploy
Local CLI, no CI/deploy pipeline. `make install` → `go install ./cmd/chv`; optional `make install-cron` (cron every 4h runs `chv index`), removed via `make uninstall-cron`. Users upgrade the DB schema by simply running `chv index` (`Repo.Init` runs migrations).
