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
internal/queue     scheduler, persistence, events, Remove(deleteFile). queues.go: downloads with Queue "" start
                   up to MaxConcurrent; "main" runs one at a time in QPos order; "later" waits for StartLater
                   (button, or Settings > Queue schedule via LaterDue) and runs one at a time with LaterDelay
                   (default 60 s) between, stopping when empty. Queue/QPos are written only by store.SetQueue
                   (a running download's own saves must not undo a move). API /v1/queues, /queues/later/start|stop,
                   /downloads/{id}/queue, /downloads/{id}/move
internal/api       REST + WebSocket server, Go client, auth/origin guards (api_test.go)
internal/web       go:embed Web UI (static/index.html, single file, VS Code / qBittorrent look)
internal/extbundle go:embed of the extensions: files/chrome (copied by `pnpm build`, gitignored) and
                   files/odm-firefox.xpi (AMO-signed unlisted, committed: needs the owner's keys to rebuild)
internal/ffmpeg    finds ffmpeg ($ODM_FFMPEG, next to odm.exe, PATH) and remuxes with -c copy only
internal/hls       HLS playlist parsing: master variants; media items (segments, AES keys, fMP4 init,
                   byte ranges) + a local playlist for ffmpeg. engine/hls.go downloads them in parallel into
                   "<name>.parts" (resumable, State.HLS), then ffmpeg -> MP4 (or plain TS concat without ffmpeg)
internal/ytdlp     runs yt-dlp (next to odm.exe; QuickJS qjs.exe beside it is passed as --js-runtimes, our
                   ffmpeg as --ffmpeg-location, --ignore-config): Probe (-J --flat-playlist: qualities or
                   playlist entries), Download (own progress template), Update (-U). Windows: job object so
                   pause kills the PyInstaller child and ffmpeg too. Media kind "ytdlp" in proto.Request;
                   API /v1/media, /v1/media/probe, /v1/media/update; Web UI toolbar "yt-dlp" dialog
extension/         shared TS built twice (dist/chrome, dist/firefox); MV3; src/content.ts = video button;
                   src/hook.ts runs in the page world (MAIN) and reports fetch/XHR bodies starting "#EXTM3U"
internal/config    paths; prefs.json = language + Settings dialog (settings.go: capture per browser, file-type and
                   site rules, download folder + category folders, downloads at once, progress/complete windows,
                   connections + per-site exceptions, per-download and total speed limits). Local daemon only:
                   GET/PUT /v1/settings, applied live (cmd/odm/settings.go -> queue.SetConfig, shared limiter)
internal/applog    download log (slog text lines) in %APPDATA%\odm\logs\odm.log, rotated 3 x 5 MB, Settings > Log
                   (default on). Writes: add (from extension/webui/cli, url, kind, name, dir, cookie COUNT only),
                   start/completed/failed/paused/removed per id, server probe (status, Content-Type, size,
                   ranges), retries, HLS/ffmpeg/yt-dlp errors, and extension events via POST /v1/log (capture
                   decision, video menu contents, picks). GET /v1/log?lines=N, POST /v1/log/reveal
cmd/odm progress   progress_windows.go: tray watches queue events and starts `odm.exe progress <id>` (small
                   WebView2 window on /progress.html, Pause/Cancel/Hide, turns into "download complete" with
                   Open / Show in Folder); taskbar_windows.go = ITaskbarList3 progress (also the main window)
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
powershell -File scripts\build-installer.ps1  # -> dist\odm-setup-<ver>.exe (Inno Setup, per-user, ~30 MB)
                                              # version = api.Version; builds ffmpeg once if dist\ffmpeg is missing;
                                              # scripts\fetch-tools.ps1: yt-dlp + QuickJS-NG at pinned SHA-256
$env:ODM_YTDLP="$PWD\dist\yt-dlp\yt-dlp.exe"; $env:ODM_YTDLP_NET=1; go test ./internal/ytdlp  # real YouTube run
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

## When the owner says "it did not download"
Read the log first: `%APPDATA%\odm\logs\odm.log` (and `odm.1.log`). Find the `add` line for the URL, then the
lines with the same `id=`: `probe` shows what the server answered (a `type="text/html"` for a video = a page
was taken for the file), `failed` the error. The extension's `video menu` / `capture` lines say what it offered
and decided. A read-only `GET http://127.0.0.1:6800/v1/downloads` also shows what the extension sent.

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
- Browser checks without touching the owner's Chrome: a separate debug Chrome
  (`--remote-debugging-port=9333 --enable-unsafe-extension-debugging --user-data-dir=%TEMP%\odm-cdp-profile`),
  extension loaded with CDP `Extensions.loadUnpacked` (`--load-extension` is gone since Chrome 137), DOM with
  `pierce: true` reaches the overlay's closed shadow root. Stop only that Chrome (its PID). The owner allowed this
  on 2026-11-13 ("this PC is barely used"). The owner's own Chrome has uBlock: ads there hide problems.
- Never point tests at the owner's running ODM (127.0.0.1:6800): use `serve --listen 127.0.0.1:6899 --token`
  with an isolated APPDATA, or `serve --local --listen 127.0.0.1:6899 --dir <temp>` to test local-only features
  (Settings, open file). That second local daemon has no browser integrations on purpose: a test once clicked
  "Install in Firefox" and opened the owner's Firefox. Always pass `--dir` so nothing lands in the real Downloads.
- Headless Chrome (`--headless=new`) is enough for Web UI pages; it opens nothing on screen.
- Extensions read their capture rules from ODM (GET /v1/settings on 127.0.0.1:6800, cached in storage as
  `odmRules`); an older ODM answers 404 and the extension then behaves as before (always ask).

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
