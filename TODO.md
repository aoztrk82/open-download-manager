# TODO (updated 2026-11-12, end of day)

## 0. Start here next session
- Owner rebuilds `odm.exe` (quit ODM first) to get the fifth-session work: shared program language, dynamic
  connections. Then walk through 2b and the language / connections items below and fix what they report.
- Nothing from the fourth/fifth sessions has been looked at on screen except the extension (owner: "very nice").
- Ideas waiting for a decision: global connection cap across downloads, connection ceiling as a UI setting,
  ODM-side dedupe of the same URL added twice within seconds.

## 0b. Video button + HLS (sixth session, 2026-11-13)
Checked in a debug Chrome (CDP, no uBlock, ads playing) up to the dialog's request, then that exact request
downloaded by an isolated daemon and checked with ffprobe:
- archive.org/details/Cops1922: 480p MP4 / 300p OGV, playing mirror merged into the listed file.
- showtv.com.tr (Ciner "hope" player, removes its config attribute after start; Google IMA iframe over the
  player): 5 MP4s from the page HTML + 5 HLS variants; 720p HLS -> 1280x720 MP4, 135.5 s.
- tranimeizle.live (JW Player, playlist at a session-bound /w/<token> served as octet-stream, segments are
  MPEG-TS disguised as .jpeg on rotating hosts): 1080p/720p/480p; 480p (never loaded by the player) fetched
  with the session, 725 pieces in ~20 s -> 854x480 MP4, 24:11.
Owner still to check in their own Chrome (with uBlock) and the real dialog -> their ODM:
- [ ] Rebuild/restart ODM, reload the extension; the three sites above; Web UI progress for an HLS download
      (size is an estimate until the end), pause/resume of an HLS download, remove with "delete file".
- [ ] Firefox needs a re-signed xpi (bump package.json, `pnpm sign`) before the embedded one has the feature.
- Not done: DASH (.mpd, listed disabled), HLS with separate audio renditions, live streams (refused).

## 0d. yt-dlp (YouTube) - sixth session, 2026-11-13
Tested here: real YouTube download through yt-dlp + our 4 MB ffmpeg (merge works; MP4 prefers H.264, checked
with ffprobe), the extension's "yt-dlp ile" entries on youtube.com up to the dialog's request and that request
downloaded by an isolated daemon, the Web UI dialog against an isolated daemon (video qualities with sizes,
20-item playlist, 2 picked into a "Blender Releases" subfolder, paused adds stay paused).
- [x] QuickJS works: owner ran `yt-dlp --js-runtimes quickjs:qjs.exe -F <youtube>` ("[jsc:quickjs] Solving JS
      challenges using quickjs", no runtime warning; only a harmless "Unknown codec iamf" for format 773).
- With QuickJS YouTube also lists H.264 HLS formats (311/312) at 2-3x the bitrate of the https DASH ones
  (1080p60: 312 ~562 MiB vs 299 ~246 MiB). Checked with qjs: our sort still picks https DASH (1080 -> 299+258,
  720 -> 298+258; best -> 628+258, VP9 HLS 2160p since there is no 4K H.264). Owner said "keep the high-bitrate
  HLS for now" believing HLS was picked; the sort was left unchanged, tell them it is actually DASH.
  The quality sizes in the dialog use the largest video at a height, so they show the HLS size (~562 MiB).
- [ ] Pause/resume of a yt-dlp download in the real program (job object kill + `--continue`); "delete file"
      removes the current .part only (the other stream's .part may stay).
- [ ] "Update yt-dlp" button in the installed program (-U needs a writable install dir; per-user install is).
- [ ] Owner's own Chrome: YouTube menu -> dialog -> their ODM.

## 0g. Queues (sixth session, end)
Tested: Go tests (order, one at a time, move up/down, later waits, wait between, stop -> back to waiting,
schedule hook, resume = start now, moves survive a running download's saves), schedule maths (once, daily,
midnight), isolated daemon end to end + Web UI screenshots. `go test -race` could not start on this PC
(ThreadSanitizer allocation error 87), so the new queue code has no race run yet.
- [ ] Owner: extension dialog with 4 buttons (layout at 540 px not seen), schedule firing for real at a set
      time with the tray running, Start/Stop from the toolbar.
- Not done: queue state after a restart (a running later queue is stopped after ODM restarts; its downloads
  wait), per-queue "downloads at once", "stop at" time, shutdown when the queue is done.

## 0f. Download log (sixth session, end)
Tested on an isolated daemon: the mail.ru page sent as a "file" fails with "the server sent a web page" and the
log shows `probe ... type="text/html"`; the same page via yt-dlp starts (81 MiB, deleted); extension POST
/v1/log lands in the log; Settings > Log shows it (screenshot). Rotation/tail unit-tested.
- [ ] Owner: rebuild + reload the extension, try the mail.ru link again (menu should offer only "yt-dlp ile").
      The extension's log lines appear only once it has fetched the rules (log: true) from the new ODM.

## 0e. Settings window + progress windows (sixth session, 2026-11-13)
Owner's request: a tabbed Settings dialog (General / File Types / Save / Downloads / Connection), browser
integrations moved there from the menu, a progress window per download with Windows taskbar progress and a
"download complete" window, both switchable in Settings.
Tested here (isolated `serve --local --listen 127.0.0.1:6899 --dir <temp>`, headless Chrome, screenshots looked
right): every tab renders in Turkish; saving writes prefs.json and applies live; a relative folder is refused in
the dialog; category folders follow the download folder; a .zip went to `Compressed`; a 512 KiB/s total limit
held (4 MiB in 7.2 s); progress.html showed speed/time left/resumable and turned into the complete view.
Go tests: settings normalize/merge, API (415/403/400, partial PUT), shared limiter, extension rule helpers.
Not tested (needs the owner, opens windows):
- [ ] Real progress window (`odm.exe progress <id>` from the tray): opens on a new download, title "45% name",
      **taskbar button progress** (green/yellow paused/red error; COM ITaskbarList3, never run), Pause/Cancel/
      Hide, "complete" window with Open / Show in Folder, "don't show again". Main window taskbar progress too.
- [ ] Settings > General in the tray daemon: "Start with Windows" (tray checkbox follows), language switch,
      browser extension rows (moved from the old Integrations menu).
- [ ] Extension with the new ODM: Firefox unchecked -> Firefox keeps its downloads; pdf in "browser types" ->
      browser saves it; a site in "start without asking" -> no dialog; category folder prefilled in the dialog.
      Chrome often has no file name at onCreated: the MIME type is used then (pdf, zip, exe, ... see util.ts).
- Known limits: rules are cached up to 30 s in the extension (and the last known rules apply while ODM is
  closed); the total speed limit does not reach yt-dlp (separate process; it gets the per-download limit);
  at most 6 progress windows per 10 minutes (long playlists); downloads resumed automatically at start-up get
  no progress window (only their complete window).

## 0c. Installer + bundled ffmpeg (sixth session, built, never installed)
`scripts\build-installer.ps1` -> `dist\odm-setup-0.3.0.exe` (7.5 MB). ffmpeg 9.0.2 built from the signed release
tarball, LGPL, remux-only + probing decoders, 4 MB, imports only system DLLs. Tested: TS / fMP4 / AES-128 HLS
and separate audio+video remux; `info.ffmpeg` true with ffmpeg next to odm.exe. Not tested (needs the owner or
Windows Sandbox, the uninstaller deletes the real HKCU native-host keys and %APPDATA%\odm\extension):
- [ ] Fresh install: no admin prompt, Start menu entry, "Start ODM now", autostart task, Turkish wizard.
- [ ] Upgrade while ODM runs: `odm quit` closes tray + window, files replaced, downloads resume.
- [ ] Uninstall: ODM stopped, files, native-host keys and Run value gone; downloads/DB in %APPDATA%\odm kept.
- [ ] Tray "Start with Windows" now writes `tray --background` (was `tray`, which opened the window at sign-in).
- Not done: code signing (SmartScreen will warn on an unsigned setup), auto-update.

## 1. Firefox double download - fixed (owner reports it works)
Cause: small files (Gutenberg epub, ~106 KB, no `Content-Disposition`) were saved before `downloads.onCreated`
could cancel them. Firefox now cancels download responses in blocking `webRequest.onHeadersReceived`; Chrome keeps
`onCreated`. Not explicitly confirmed: the cold-wake case (Firefox idle ~1 minute, then click) and whether the
tab ever shows a "blocked by an extension" page. If capture misbehaves: about:debugging -> ODM -> Inspect and
read the `[ODM]` console lines.

## 2. Extension install from ODM (written 2026-11-11, needs the owner)
- [x] Signed with AMO (unlisted) and embedded (0.2.0, then 0.3.0); keys come from the gitignored root `.env`.
- [ ] Integrations > Install in Firefox: shows Firefox's "Add ODM?" prompt (firefox.exe <xpi path>, untested).
- [ ] Chrome/Edge: "Open ... Extensions Page" really opens chrome://extensions / edge://extensions from the
      command line (unverified); Load unpacked from the folder; extension id equals `chromeExtensionID`.
- [ ] Tray restart after an extension change rewrites the folder while Chrome has it loaded; Chrome picks it up.
- [ ] Popup version warning and the "last seen" column after installing.

## 2b. Fourth session work, never seen on screen (owner was gaming)
- [ ] Download dialog in Firefox and Chrome: layout, Enter/Esc, Browse… (folder chooser must come to the front,
      `cmd/odm/pickfolder_windows.go`), Later, "let the browser", "don't ask again", server destination.
- [ ] Popup and settings page look (light and dark), language switch.
- [ ] Web UI in Turkish: menus, sidebar icons, statuses, dialogs; uppercase sidebar headings show "İ".
- [ ] Tray menu in Turkish; tray, window and taskbar icons.
- [x] Exe icon: owner generated `cmd/odm/rsrc_windows_*.syso` with go-winres (2026-11-12).
- [ ] Check the go-winres default manifest did not change window DPI/sharpness; regenerate after icon changes:
      in `cmd/odm` run `go run github.com/tc-hib/go-winres@v0.3.3 simply --icon assets/odm.ico`.
- [ ] Program language: Turkish by default on this PC (regional format tr-TR), View menu switch also renames the
      tray menu without restart.

## 3. Verify (written but never run against the real thing)
- [ ] Tray "Quit ODM" closes an open window within ~3 s (window pings the daemon, `runWindow` in `gui_windows.go`).
- [ ] Frameless window: drag, edge resize, minimize/maximize/close buttons, double-click title to maximize.
- [ ] Tray menu: "Start with Windows" (registry Run key), "Open downloads folder", icon rendering.
- [ ] Context menu "Show in Folder" opens Explorer with the file selected (`reveal_windows.go`).
- [ ] Remove dialog with "also delete file" in the real UI (backend is unit-tested).
- [ ] Chrome: load `extension/dist/chrome`, compare the shown extension id with `chromeExtensionID`
      (`linelmlgjkggjofcfgljpdkfgdjgabkl`); test native-host start with the tray closed.
- [ ] Native host survives browser job-object kill (tray must stay alive after the host exits, in both browsers).
- [ ] FTP/FTPS against a real server (fake-server test not written yet).

## 4. Download engine
- [ ] Connections tab with dynamic connections in the real window ("n / max receiving", compact finished boxes).
- [x] Dynamic connections and work stealing (fifth session).
- Growth stops at the first plateau; a later re-probe could find more speed on noisy links.
- Ceiling is per download: 3 active downloads may open 3 x CPU threads connections; a global cap may be wanted.
- Make the ceiling a UI setting (today: `serve --connections`, 0 = CPU threads).
- Not visually checked yet: resize handles and the Connections tab in the real window (only `node --check`).

## 5. Small known issues
- Context menu shows a stray separator when "Show in Folder" is hidden (remote/browser use).
- Brief console flash on double-click (console-subsystem exe); needs a separate GUI-subsystem build to remove.
- `internal/queue` has no cancel-during-download test for `Remove(deleteFile)` on active downloads.
- Closing the chooser cancels the download (deliberate; could instead fall back to the browser).
