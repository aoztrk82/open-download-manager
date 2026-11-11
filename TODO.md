# TODO (updated 2026-11-11, end of day)

## 1. OPEN BUG - start here tomorrow: Firefox still downloads by itself, and ODM downloads too
Owner's report after the last commit (`b4c3464`): with the Firefox extension loaded, clicking the Gutenberg epub3
button (https://www.gutenberg.org/ebooks/79962) makes **both** Firefox and ODM download the file. Two earlier fix
attempts (cancel after add; then cancel first + removeFile + erase) did not solve it.

What we know:
- Server side is fine: preflight/CORS from a `moz-extension://` origin works, `POST /v1/downloads` works.
- `web-ext lint` passes; the extension compiles; ODM *does* receive the download, so `onCreated` fires and the add works.
- So the failure is in `takeFromBrowser()` (`extension/src/background.ts`): cancel/removeFile/erase are all wrapped
  in `try/catch {}` and **errors are swallowed silently**. We never saw why cancel did not stop Firefox.
- Not verified: whether the owner reloaded the extension after the last build (an old temporary add-on copy
  loaded next to the new one would explain two ODM downloads); whether Firefox's own download panel is the
  "Firefox download" they see; what the `[ODM]` console lines say.

Diagnosis (2026-11-11, third session): the Gutenberg file is 302 -> 200 `application/epub+zip`, ~106 KB, **no
`Content-Disposition`**. Firefox has saved it before the async `onCreated` handler can call `cancel()`.

Done (compile/lint-checked only, NOT yet tested in Firefox):
- Firefox now uses **blocking `webRequest.onHeadersReceived`** (main_frame/sub_frame, GET, status 200): a response
  with `attachment` disposition or a content type Firefox does not render is cancelled and handed to ODM, so
  Firefox never creates a download. `manifest.firefox.json` got `webRequest` + `webRequestBlocking`. Chrome has no
  such permission, `ext.webRequest` is undefined there, and it keeps the `onCreated` path.
- `onCreated` stays as fallback (e.g. "Save link as", renderable types) and skips URLs webRequest just took.
- `takeFromBrowser` logs every step (`[ODM] ...` in the console) and reports when the browser kept its copy.

Owner checklist:
1. Remove every ODM entry in `about:debugging`, load `extension/dist/firefox` once.
2. about:addons -> ODM -> Permissions: "Access your data for all websites" must be on (MV3 host permissions can
   be ungranted; then webRequest sees nothing).
3. Click the epub3 button: only ODM should download. Does the tab stay on the page, or show a
   "blocked by an extension" error page? (error page = follow-up needed)
4. Leave Firefox idle ~1 minute (background page gets suspended), then click epub3 again: the cold-wake path
   waits for settings via a promise in the blocking listener.
5. If it still fails: about:debugging -> ODM -> Inspect, paste the `[ODM]` lines.

Still open: deduplication guard on the ODM side (same URL added twice within a few seconds -> ignore).

## 2. Extension install from ODM (written 2026-11-11, needs the owner)
- [ ] Owner: create AMO API keys, `$env:WEB_EXT_API_KEY=...; $env:WEB_EXT_API_SECRET=...; cd extension; pnpm sign`,
      then rebuild odm.exe and commit `internal/extbundle/files/odm-firefox.xpi`. Only after TODO #1 works with a
      temporary load (a signed version number cannot be reused).
- [ ] Integrations > Install in Firefox: shows Firefox's "Add ODM?" prompt (firefox.exe <xpi path>, untested).
- [ ] Chrome/Edge: "Open ... Extensions Page" really opens chrome://extensions / edge://extensions from the
      command line (unverified); Load unpacked from the folder; extension id equals `chromeExtensionID`.
- [ ] Tray restart after an extension change rewrites the folder while Chrome has it loaded; Chrome picks it up.
- [ ] Popup version warning and the "last seen" column after installing.

## 3. Verify (written but never run against the real thing)
- [ ] Tray "Quit ODM" closes an open window within ~3 s (window pings the daemon, `runWindow` in `gui_windows.go`).
- [ ] Frameless window: drag, edge resize, minimize/maximize/close buttons, double-click title to maximize.
- [ ] Tray menu: "Start with Windows" (registry Run key), "Open downloads folder", icon rendering.
- [ ] Context menu "Show in Folder" opens Explorer with the file selected (`reveal_windows.go`).
- [ ] Remove dialog with "also delete file" in the real UI (backend is unit-tested).
- [ ] Chrome: load `extension/dist/chrome`, compare the shown extension id with `chromeExtensionID`
      (`linelmlgjkggjofcfgljpdkfgdjgabkl`); test native-host start with the tray closed.
- [ ] Native host survives browser job-object kill (tray must stay alive after the host exits, in both browsers).
- [ ] Chooser window (`choose.html`) appears when a server URL is configured; closing it cancels the download.
- [ ] FTP/FTPS against a real server (fake-server test not written yet).

## 4. Download engine ideas (from the connections view)
- Dynamic splitting: when a connection finishes its range, split the largest remaining one so all connections
  stay busy to the end (today finished connections sit idle).
- Connections per download is fixed at 4 (`queue.Config.Connections`); make it a setting / per-download option.
- Not visually checked yet: resize handles and the Connections tab in the real window (only `node --check`).

## 5. Small known issues
- Context menu shows a stray separator when "Show in Folder" is hidden (remote/browser use).
- Brief console flash on double-click (console-subsystem exe); needs a separate GUI-subsystem build to remove.
- `internal/queue` has no cancel-during-download test for `Remove(deleteFile)` on active downloads.
- Closing the chooser cancels the download (deliberate; could instead fall back to the browser).
