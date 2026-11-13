# History

Newest first. Commit hashes refer to `git log`.

## 2026-11-13 - sixth session
- New dev PC set up: Go 1.27.4 (winget), pnpm 12 (npm -g), Inno Setup 6.7.3 (per-user), MSYS2 make/diffutils.
  pnpm 12 fails installs on unapproved build scripts: esbuild approved in `extension/pnpm-workspace.yaml`.
- Video button (owner: "a button on the playing video, list the qualities, use our dialog"): `content.ts` in
  every http(s) frame finds the hovered `<video>` (also in open/closed shadow roots, via elementsFromPoint),
  lists archive.org `play-av[playlist]` renditions, `<source>`, currentSrc, other videos and og:video files;
  sizes from a background HEAD / 1-byte range fetch; pick -> `handOff` (dialog). Owner confirmed archive.org
  works in Chrome. Other sites said "MSE": those stream HLS/DASH, which is the next step (ROADMAP "Media").
- Owner: ODM ships as an installer, and users must not be told to install ffmpeg first. Prebuilt ffmpeg is
  100-200 MB, so `tools/ffmpeg/build.sh` builds 9.0.2 from the signed tarball with only containers, parsers,
  bsfs and the decoders stream probing needs (without them TS -> MP4 failed with "dimensions not set"): 4 MB,
  LGPL, system DLLs only. Verified on TS, fMP4 and AES-128 HLS plus separate audio+video. `internal/ffmpeg`
  finds it ($ODM_FFMPEG, next to odm.exe, PATH); `info.ffmpeg` reports it.
- `installer/odm.iss` + `scripts/build-installer.ps1`: per-user Inno Setup (7.5 MB) with odm.exe and ffmpeg.
  `odm quit` stops tray + window through a named event (no new API surface) so upgrades can replace files.
  Tray autostart now writes `tray --background`. The installer was built but not run (see TODO 0c).
- Owner on Show TV: "it downloaded the mp4, but the other qualities were missing and the name was a hash".
  The player removes its `data-hope-video` config once it starts (only JSON-LD's 1080p stayed), so the page's
  own HTML is fetched and scanned too; configs are found generically (any JSON attribute/script with media
  URLs, grouped by title), and files are named "<title> (<quality>).<ext>".
- Owner: tranimeizle "not supported". Inspected with a debug Chrome over CDP (owner's permission): JW Player,
  HLS playlist from a session-bound `/w/<token>` (octet-stream), segments are plain MPEG-TS named `.jpeg`.
  HLS support: `hook.ts` in the page world catches playlist bodies by content; the menu lists every variant
  with an estimated size; the playlist text travels with the request (`proto.Request.Media`), since the URL may
  not work outside the session; `internal/hls` + `engine/hls.go` download pieces in parallel (resumable,
  cookies only to the page's host), ffmpeg joins them (AES-128, fMP4) into MP4.
- An ad SDK iframe over the player swallowed every mouse event (Chrome reports nothing to the page about a
  pointer in a cross-process iframe), so the button never showed while ads ran without uBlock: frames now pass
  the pointer position up to their parent. Ad `<video>`s (IMA) are skipped when picking the hovered video.
- YouTube (owner: "bundle yt-dlp like ffmpeg, a yt-dlp button and dialog in the UI, playlists"). yt-dlp
  2026.10.26 (Unlicense) is shipped next to odm.exe; since late 2025 yt-dlp needs a JavaScript runtime for
  YouTube (works without one for now, with a deprecation warning), so QuickJS-NG 0.17.0 (MIT, 2 MB; Deno/Node
  are 80-100 MB) ships too. Both pinned by SHA-256 in `scripts/fetch-tools.ps1`; installer 7.5 -> 30 MB.
  `internal/ytdlp` probes (qualities with sizes, flat playlists) and downloads with its own progress lines;
  a Windows job object makes pause stop yt-dlp's PyInstaller child and ffmpeg. MP4 prefers H.264 at the same
  resolution (YouTube's default pick was AV1, which older Windows cannot play). Web UI: "yt-dlp" toolbar
  button / Ctrl+Shift+N dialog (quality, MP4/MKV, folder, playlist with select-all and a subfolder, "Update
  yt-dlp"). Extension: "yt-dlp ile" entries (best/1080/720/480/audio) on YouTube and similar sites, or when a
  page offers nothing else. Owner ran QuickJS: works; our sort picks H.264 https DASH (not the larger HLS).
- Settings (owner: "tabs: general, file types, save, downloads, connection; a progress dialog with Windows
  progress, and settings to show it / a done dialog"). Settings live in prefs.json (`config.Settings`), local
  daemon only (`/v1/settings`), applied without restart: download folder and category folders (a category
  without its own folder follows the download folder), downloads at once, connections + per-site exceptions,
  per-download and a shared total speed limit (`engine/limit.go`, also yt-dlp `--limit-rate`). The extensions
  fetch the capture rules (which browsers, types the browser keeps, types/sites without the dialog, sites never
  captured) from ODM. Integrations moved from the menu into Settings > General. Progress windows are separate
  `odm.exe progress <id>` processes started by the tray (`/progress.html`, taskbar progress via ITaskbarList3).
- Download log (owner: "when I say it did not download, you should see the URL and why"): `internal/applog`,
  Settings > Log (view, copy, show in folder). Owner's mail.ru report, found from their ODM's list: the player's
  `<video src="">` makes `video.src` the page address, so the menu offered the page as a "file" and ODM saved
  HTML. Fixed in the extension (the page is never a source; mail.ru goes to yt-dlp, which supports it) and in
  ODM (a text/html answer for a video/audio name is refused with a clear error). A repo-root dev build also
  finds `dist\yt-dlp\yt-dlp.exe`.
- Toward 0.5.0: "Clear List" (toolbar + File menu: completed / failed / paused, files optional); double-click or
  Enter opens the download's window (progress, or "complete" when done; POST /v1/downloads/{id}/window in the
  desktop app, progress.html popup in a browser). Docker image (owner: "prepare everything, I will install Docker
  and we publish 0.5.0 on Docker Hub and GitHub releases"): Dockerfile, docker-compose.yml, scripts/docker-build.ps1;
  ffmpeg components shared between the Windows and Linux builds. Settings now also work on a token server
  (folders stay inside --dir). Not built yet: Docker was not installed on this PC.
- Version 0.4.0 (owner brought the AMO keys back in `.env`): Firefox extension signed (unlisted) and embedded,
  installer `dist\odm-setup-0.4.0.exe` (30.4 MB), tag `v0.4.0`. Contents since 0.3.0: video button + HLS,
  bundled ffmpeg / yt-dlp / QuickJS, installer, Settings window, progress windows, download log, queues.
- Queues (owner: "Download, Add to queue, Later; Later is a standby queue with Start and a schedule; a wait
  between downloads, default 60 s"). Main queue: one after another as soon as added. Later queue: waits for
  Start (toolbar, queue bar) or the schedule (date+time, optionally daily), one after another with the wait,
  stops when empty; Stop puts the running one back at the head. Extension dialog got "Add to queue"; "Later"
  now means the later queue (older ODMs keep it paused). Web UI: sidebar Queues, queue bar, "When" in the Add
  and yt-dlp dialogs, context menu (to queue / later / out, move up/down), Settings > Queue. Later-queue
  downloads open no progress/complete windows (unattended). Verified on an isolated daemon (2 + 2 files,
  never two at once, 3 s wait, queue stopped itself, log lines).
- Test incident: an end-to-end script clicked the first "primary" button in the Settings dialog, which was
  "Install in Firefox", and the test daemon opened the owner's Firefox (closed again by its PID) and saved a
  test file to the real Downloads (deleted). Fixed the cause: dialog Enter/primary only means the footer
  button (real users would have hit it with Enter too); a second local daemon (`--local --listen`) has no
  integrations; tests pass `--dir`.

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
