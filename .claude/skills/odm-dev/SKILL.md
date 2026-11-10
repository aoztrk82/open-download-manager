---
name: odm-dev
description: Build, test and safely verify the ODM (open-download-manager) project. Use when changing Go code under cmd/ or internal/, the Web UI, or the browser extension, and before declaring any ODM change done.
---

# ODM development loop

Context lives in `CLAUDE.md`; open work in `TODO.md`. This skill is the checklist for changing code.

## 1. Build and static checks (always, no windows involved)
```
gofmt -l cmd internal            # must print nothing
go vet ./...
go test ./...                    # api guards, engine, queue, native host
GOOS=linux go vet ./cmd/...      # non-windows must still compile
go build -o odm.exe ./cmd/odm
```
Web UI: extract the `<script>` from `internal/web/static/index.html` to a temp file and run `node --check`; also
check every `$("id")` used exists as `id="..."` in the markup.
Extension: `cd extension && pnpm typecheck && pnpm build && pnpm dlx web-ext lint -s dist/firefox`.

## 2. Runtime checks that are safe without a GUI
- Isolated state: `$env:APPDATA = "$env:TEMP\odmtestN"`; remove it afterwards.
- Headless daemon: `odm.exe serve --local --dir <tempdir>`; drive it with `curl.exe`.
- Test downloads: a local `python -m http.server 8099 --directory <tempdir>`; stop it by matching its command line.
- Native host: `scratchpad/hosttest.ps1`-style script that starts `odm.exe chrome-extension://<id>/`, writes a
  length-prefixed `{"cmd":"ping"}` and reads the reply (see `cmd/odm/native_test.go` for the framing).
- Security-sensitive API change? Extend `internal/api/api_test.go` first.

## 3. Things that need the owner
Anything that opens the tray, a window, or a real browser: ask first, then give a short numbered checklist.
Never capture the full screen. Never stop processes you did not start.

## 4. Finishing
Report exactly what was tested and what was only compiled. Update `TODO.md` (open items) and `HISTORY.md`
(what changed and why) in the same commit when behavior changes.
