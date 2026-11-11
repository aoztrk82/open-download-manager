# History

Newest first. Commit hashes refer to `git log`.

## 2026-11-12 - fifth session
- Dynamic connections (owner: "why always 4?"). The engine starts with one segment and 2 connections, adds a
  connection every 2 s while total speed still rises by >10%, up to CPU threads (owner's choice; clamped 2..32,
  per download). A connection that finishes splits the largest remaining range (work stealing), so none idle at
  the end. Extra connections the server refuses hand their range back and lower a per-host limit. Safety: writes
  are clamped to the segment's End under the lock, split points stay past in-flight writes, HTTP Open checks the
  Content-Range start and total (stale mirrors), and `verify()` must pass before the file is renamed.
  Tests: growth, stealing stress, connection-limited server, servers ignoring Range / wrong size, pause+resume
  through JSON after splits, resuming old fixed 4-segment state; engine suite 20x under -race.
  Real network (scratch binary on :6801, isolated APPDATA): Linux Mint 22.3 Cinnamon ISO, 3,091,660,800 bytes in
  36 s (~85 MiB/s), grew 2 -> 5 connections and stopped at the plateau (~100 MiB/s line), 16 segments after
  stealing, SHA-256 equal to Mint's published `sha256sum.txt`.
- Owner saw the program in English: Windows display language is en-US, regional format tr-TR, and the window
  took WebView2's language. Now the program has one shared language (`prefs.json`, `PUT /v1/prefs`,
  `info.lang`), defaulting to Turkish when the display language or the regional format is Turkish; choosing a
  language in the window also switches the tray menu live.

## 2026-11-11 - fourth session (owner away; nothing was opened on screen)
- Icons: one generator (`tools/genicons`) draws the logo (blue rounded square, down arrow over a 4-part bar =
  parallel connections) for the extension (16-128), Web UI favicon/brand, tray `.ico` and the window/taskbar icon
  (`cmd/odm/icon_windows.go`, `CreateIconFromResourceEx` + `WM_SETICON`). Sidebar dots became icons.
  The exe file icon (Explorer) is not done: generating a `.syso` needs an external tool that was not allowed to run.
- English + Turkish everywhere: Web UI (`i18n.js`, View menu), extension (`src/i18n.ts`, `_locales`), tray menu.
  `scripts/i18n-check.mjs` checks key parity and that every used key exists.
- Download dialog before every captured download (owner: "ask before it starts: where, server or local"):
  file name, size/resumable, destination, folder with recent list and native "Browse…", Download / Later / let
  the browser. New API: `POST /v1/probe`, `POST /v1/pickfolder` (local only, IFileOpenDialog owned by an invisible
  topmost window), `dir` validation, `paused` on add, `info.canPickFolder/anyFolder`. Web UI Add dialog got the
  folder field, Browse and "add paused" too.
- Extension UI rebuilt on a shared stylesheet (`static/ui.css`, light/dark): popup with status badge, capture
  switch, progress bars; settings page with language, ask-before-download, default destination.
- Fixed: the version ping added in the third session ran inside `storage.onChanged` instead of at startup.
- Version 0.3.0.

## 2026-11-11 - third session
- Version 0.2.0 (ODM and extensions). First AMO-signed Firefox build committed as
  `internal/extbundle/files/odm-firefox.xpi`; from now on bump `extension/package.json` before every `pnpm sign`.
- Firefox double download: root cause is timing, not an API error. Small files (Gutenberg epub, ~106 KB, no
  `Content-Disposition`) finish before `downloads.onCreated` -> `cancel()` runs. Firefox now cancels download
  responses in blocking `webRequest.onHeadersReceived` and hands them to ODM; `onCreated` is the fallback and
  the only path in Chrome. `takeFromBrowser` no longer swallows errors silently. Not yet tested in Firefox.
- Extensions install from the app (owner's request: a store auto-updating the extension past the exe would break
  things). New `internal/extbundle` embeds both builds; menu Integrations > Browser Extensions shows per browser
  the bundled version, the version last seen (`/v1/info?ext=`) and install buttons. Decisions with the owner:
  Firefox via AMO unlisted signing (`pnpm sign`, signed xpi committed), Chrome/Edge via "Load unpacked" from a
  folder the tray keeps in sync. Popup warns when extension and ODM versions differ. `strict_min_version` -> 140.
  Owner reports the extension changes work (which steps were run is not yet confirmed).
- Web UI: sidebar, details panel and table columns are resizable (sizes in localStorage; View > Reset Layout,
  double-click a splitter/column edge to reset). Details panel got a **Connections** tab: one box per segment
  (state, range, received, speed, retries/error) plus a file map, fed by new `GET /v1/downloads/{id}/connections`.
  The engine tracks per-connection state (`State.Conns`, not persisted); the queue keeps the latest snapshot of
  running downloads in memory. "Save to" now shows the download's own path. Verified headless against the Pardus
  25.2 ISO (2.8 GB): 4 connections active in parallel, "stopped" after pause.
- Fixed a long-standing race in `queue.run`: the engine wrote through `&d.State` while the progress callback
  replaced `d.State` with snapshots, so the segment array could be swapped under running connections (a lost
  `Done` increment would shift later writes = silent corruption). The engine now owns a private copy; found while
  adding the live connections view, proven by `TestConnectionsWhileRunning` under `-race`.

## 2026-11-11 - second session
- `b4c3464` Token-less local mode (host/origin guards + tests), native-messaging launcher so the extension can start
  ODM, local/server chooser window in the extension, tray `--background`, window closes when the tray quits.
- `df56b34` Frameless window with custom title bar, dialog X/Esc/Cancel, "Show in Folder", remove-with-file-delete
  (queue + API + CLI `--delete-file`), extension switched to the `browser` namespace with visible error reporting.
- `44564f9` Fix: Explorer double-click showed "This is a command line tool" (cobra mousetrap disabled).
- Docs: CLAUDE.md, TODO.md, ROADMAP.md, HISTORY.md, `.gitignore`, `.gitattributes`, `odm-dev` skill.
- **Ended with an open bug:** Firefox still downloads the file itself and ODM downloads it too. See TODO #1.

## 2026-11-10 - first session
- `0997475` Windows tray app + WebView2 window + VS Code/qBittorrent-style Web UI (menu bar, toolbar, sidebar,
  table, details panel, status bar, themes). Fixed an invisible window (HideWindow) and DPI scaling.
- `067e815` Initial implementation: Go engine (segmented download, resume, retry, rate limit), HTTP + FTP sources,
  SQLite store, queue with events, REST + WebSocket API, CLI, Web UI, Chrome/Firefox extension (TypeScript, esbuild).
- Decisions (made with the owner): Go core + TypeScript extensions; one daemon with local and server modes;
  scope = HTTP(S) multi-segment with resume, FTP/FTPS, persistent queue, Web UI.
- Verified then: `go test -race` (segmented download, no-Range fallback, resume, queue persistence); an 8 MB end-to-end
  download with matching SHA-256; 401 without token (token mode was later removed for local use).
- Process notes: a full-screen screenshot once captured the owner's private chat; it was deleted and the rule
  "never capture the whole screen / never open windows unasked" was added to CLAUDE.md. A blanket
  `taskkill python.exe` was blocked; only processes we started may be stopped.
