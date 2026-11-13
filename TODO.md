# TODO (updated 2026-11-12, end of day)

## 0. Start here next session
- Owner rebuilds `odm.exe` (quit ODM first) to get the fifth-session work: shared program language, dynamic
  connections. Then walk through 2b and the language / connections items below and fix what they report.
- Nothing from the fourth/fifth sessions has been looked at on screen except the extension (owner: "very nice").
- Ideas waiting for a decision: global connection cap across downloads, connection ceiling as a UI setting,
  ODM-side dedupe of the same URL added twice within seconds.

## 0b. Video button (sixth session, 2026-11-13, never seen on screen)
`extension/src/content.ts`: hovering a video >= 200x110 shows "Download with ODM ▾" top-right; the menu lists
renditions (archive.org `play-av[playlist]`, `<source>`, currentSrc, other `<video>`s, og:video media files),
sizes via background HEAD/range fetch, pick -> `handOff` (dialog). Unit tests cover the pure helpers in util.ts;
`/v1/probe` on the archive.org mp4 checked against an isolated daemon. Not yet tried in a browser:
- [ ] https://archive.org/details/Cops1922 in Chrome and Firefox: button appears over the player (shadow DOM),
      menu shows 480p MP4 / 300p OGV with sizes, "playing" tag, pick opens the dialog, ODM downloads it.
- [ ] Button hides after ~3 s idle / on leaving the video; clicks on it do not pause the player; fullscreen.
- [ ] Firefox needs a re-signed xpi (bump package.json, `pnpm sign`) before the embedded one has the feature.
- Later: HLS/DASH (m3u8/mpd) are listed but disabled; MSE (blob:) videos show a note. Needs engine support.

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
