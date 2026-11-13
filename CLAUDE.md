# ODM (open-download-manager) - guide for Claude

Read this first, then `TODO.md` (what is open right now) and `ROADMAP.md` (where it is going).
`HISTORY.md` records what was done and why.

## What this is
A three-part download manager: **server**, **Windows CLI/tray app** and **Firefox + Chrome extensions**.
One Go binary (`odm.exe`) is both the daemon/server and the CLI; the extensions are TypeScript.
Owner's language is Turkish; talk to them in Turkish, keep code and comments in English.

Headline feature the owner cares about: when a server is configured in the extension, every captured
download **asks "this computer or the server?"**; with no server it downloads locally.

## Architecture (all Go under `internal/`, shared by CLI, server and tray)
```
cmd/odm            main, cobra commands, tray + WebView2 window (windows), native-messaging host
internal/engine    segmented, resumable download engine (State is persisted for resume); pool.go = dynamic
                   connections: start with 2, add one while speed rises >10%, ceiling = CPU threads (2..32),
                   finished connections split the largest running range, refused extra connections lower the
                   per-host limit; verify() checks the ranges tile the file before .part is renamed
internal/proto     Source interface; http.go (Range probe) and ftp.go (not yet run against a real server)
internal/store     SQLite (modernc, no cgo): downloads table, state as JSON
internal/queue     scheduler, persistence, events, Remove(deleteFile)
internal/api       REST + WebSocket server, Go client, auth/origin guards (api_test.go)
internal/web       go:embed Web UI (static/index.html, single file, VS Code / qBittorrent look)
internal/extbundle go:embed of the extensions: files/chrome (copied by `pnpm build`, gitignored) and
                   files/odm-firefox.xpi (AMO-signed unlisted, committed: needs the owner's keys to rebuild)
internal/ffmpeg    finds ffmpeg ($ODM_FFMPEG, next to odm.exe, PATH) and remuxes with -c copy only
extension/         shared TS built twice (dist/chrome, dist/firefox); MV3; src/content.ts = video button
installer/odm.iss  Inno Setup script: odm.exe + ffmpeg.exe, per-user, stops ODM with `odm quit` first
```
- **Distribution is an installer, not a portable exe.** ffmpeg ships inside it (users never install it
  themselves). `odm quit` (hidden, `cmd/odm/quit_windows.go`) stops tray + window via a named event so the
  installer can replace files; it is not an API endpoint on purpose.
- **Local mode** (`odm serve --local`, the tray app): listens on 127.0.0.1:6800, **no token**. Protection is
  `auth()` in `internal/api/api.go`: Host must be loopback, Origin must be absent / same-origin /
  `chrome-extension|moz-extension|safari-web-extension`, `POST /v1/downloads` must be `application/json`.
  Do not weaken this; `internal/api/api_test.go` is the proof. CORS headers are only emitted for trusted origins.
- **Server mode** (`--listen`): token mandatory (`--token` / `ODM_TOKEN`), `Reveal` (show in folder) disabled.
- Double-clicking `odm.exe` (no args) -> `tray` (detached) -> daemon + notification-area icon + `window` process.
  The window is a separate process (`odm.exe window`, WebView2, frameless, custom title bar in the Web UI) and
  closes itself when the daemon stops answering. Closing the window does not stop the daemon; "Quit ODM" does.
- Extension -> ODM launch: web extensions cannot start programs, so `odm.exe` registers itself as native-messaging
  host `odm_host` (HKCU keys for Chrome, Edge, Firefox; manifests in `%APPDATA%\odm\native`) on every tray start.
  Browsers start it as `odm.exe <origin>` / `odm.exe <manifest.json> <id>`; `isNativeHostLaunch()` detects that
  before cobra runs. stdout carries only length-prefixed JSON frames.
- Extensions are installed **from odm.exe** (menu Integrations > Browser Extensions, `/v1/integrations`, local
  daemon only), never from a store, so the extension version follows the exe. Firefox: the embedded signed xpi is
  opened with firefox.exe (no `update_url`, so Firefox never auto-updates it). Chrome/Edge: "Load unpacked" from
  `%APPDATA%\odm\extension\chrome`, which the tray refreshes in place on every start. Extensions report their
  version as `GET /v1/info?ext=<browser>/<version>` (query param on purpose: a new header would change CORS).
  The version lives only in `extension/package.json`; `build.mjs` writes it into both manifests.
- Chrome extension id is pinned by the `key` in `extension/manifest.chrome.json` and must equal
  `chromeExtensionID` in `cmd/odm/native_windows.go`. Firefox id: `odm@open-download-manager.local`.

## Commands
```
go build -o odm.exe ./cmd/odm               # build the extension first, or it is "not bundled"
go vet ./... && go test ./...                 # api, engine, queue, native host
GOOS=linux go vet ./cmd/...                   # keep non-windows building
cd extension && pnpm install && pnpm build    # -> extension/dist/{chrome,firefox} + internal/extbundle/files/chrome
cd extension && pnpm sign                     # AMO keys from env or root .env (MOZILLA_JWT_ISSUER/KEY, gitignored);
                                              # uploads to Mozilla, version is burned: bump package.json first
cd extension && pnpm typecheck && pnpm test   # test = pure helpers in src/util.ts
node scripts/i18n-check.mjs                   # en/tr key parity for Web UI and extension
go run tools/genicons/main.go                 # redraw every icon (extension, Web UI, tray, window)
powershell -File scripts\build-installer.ps1  # -> dist\odm-setup-<ver>.exe (Inno Setup, per-user, ~7.5 MB)
                                              # version = api.Version; builds ffmpeg once if dist\ffmpeg is missing
C:\msys64\usr\bin\bash.exe -lc "cd <repo> && MSYSTEM=MINGW64 bash tools/ffmpeg/build.sh"
                                              # small LGPL remux-only ffmpeg (~4 MB) -> dist\ffmpeg
$env:ODM_FFMPEG="$PWD\dist\ffmpeg\ffmpeg.exe"; go test ./internal/ffmpeg  # otherwise the real-ffmpeg test skips
cd extension && pnpm dlx web-ext lint -s dist/firefox
```
Web UI has no build step; check syntax by extracting the `<script>` and running `node --check`.

## UI conventions
- **Two languages, English and Turkish.** Web UI strings live in `internal/web/static/i18n.js` (`t("key")`,
  `data-i18n*` attributes; View menu picks the language). Extension strings live in `extension/src/i18n.ts`
  (setting "language", default = browser language); manifest name/description in `extension/_locales`.
  The program (window + Web UI on the local daemon + tray) has **one** language: `prefs.json` in the config dir
  (`internal/config/prefs.go`), default = Turkish if the Windows display language *or* regional format is
  Turkish (`systemLang`), changed via View menu -> `PUT /v1/prefs`, tray retitles live. The window passes it as
  `#lang=xx`. Never hard-code UI text: add the key to every language and run `node scripts/i18n-check.mjs`.
- Extension pages build DOM with `h()` / `setText()` from `i18n.ts`, not `innerHTML` (AMO lint warns on it).
- Download dialog (`extension/static/download.html`): shown before every captured download unless the
  "askBeforeDownload" setting is off; name, size (browser hints, else `POST /v1/probe`), this computer or the
  server, folder (`dir`, recent per destination, "Browse…" = `POST /v1/pickfolder`, local daemon only), Download /
  Later (`paused`) / browser. Server mode only accepts folders inside its download dir (`checkDir` in
  `internal/api/dialog.go`); the local daemon accepts any absolute folder (`Server.AnyDir`).

## Rules for working in this repo (learned the hard way)
- **Never open a window, launch the GUI or take screenshots unless the owner says so.** They may be gaming or working.
  If a capture is ever needed, capture only the ODM window rectangle, never the full screen (an earlier full-screen
  capture exposed their private chats; the file was deleted).
- **Never `taskkill`/`Stop-Process` by image name for things you did not start** (e.g. all `python.exe`). Match on the
  command line of the process you launched (`http.server 8099`) or use the PID you got.
- Test the daemon with an isolated `%APPDATA%` (`$env:APPDATA = "$env:TEMP\odmtestN"`) so the real DB/registry
  registration is not polluted, and delete it afterwards. Exception: native-host registration tests write HKCU keys
  that point at the repo `odm.exe`; that is the intended final state.
- Bash heredocs with quotes broke several times in this environment; write files with the Write tool and run
  helper scripts from files.
- Say plainly what was and was not tested. Many GUI/browser paths are only compile-checked.

## Gotchas already solved (do not re-learn)
- Cobra "mousetrap" refuses to run when launched from Explorer: `cobra.MousetrapHelpText = ""` in `main()`.
- `HideWindow` in `SysProcAttr` makes the GUI window invisible; `spawnSelf("window")` must not set it.
- WebView2 window must be DPI-aware (`SetProcessDpiAwarenessContext(-4)`) or it is blurry and off-center.
- Frameless window: system maximize overshoots the monitor, so maximize is done manually to the work area.
- `x/sys/windows` has no monitor API; `MonitorFromWindow`/`GetMonitorInfoW` are called through `user32` directly.
- Firefox: use the `browser` namespace (promises); `chrome.*` there is callback-based. `ext` in `extension/src/api.ts`
  picks the right one.
- Go's argv quoting breaks `explorer.exe /select,"path with spaces"`; `reveal_windows.go` uses `SysProcAttr.CmdLine`.
- `go mod tidy` before the code imports a package removes it; run it after adding code, not before.

## Conventions
Match the surrounding code; comments explain why, not what. Small, tested steps; keep CLI, server, tray and
extensions on the same API (`/api` is the single contract; there is no OpenAPI file yet, see ROADMAP).
Commit messages end with the attribution line the session asks for.
