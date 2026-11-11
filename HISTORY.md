# History

Newest first. Commit hashes refer to `git log`.

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
