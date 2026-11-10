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

Next steps, in order:
1. Make `takeFromBrowser` log each step's result/error (`console.log`) and surface it via `report()`.
   Ask the owner to open `about:debugging` -> ODM -> Inspect and paste the console.
2. Confirm only one copy of the extension is loaded (remove all ODM entries in `about:debugging`, load once).
3. If `downloads.cancel` does not stop it in Firefox (the item may already be past the point of cancel because the
   link answers with `Content-Disposition: attachment`), switch Firefox to **blocking `webRequest`**:
   `webRequest.onHeadersReceived` with `["blocking","responseHeaders"]` (needs `webRequest`, `webRequestBlocking`
   permissions in `manifest.firefox.json`), detect download responses (attachment disposition or non-renderable
   content type), return `{cancel: true}` and hand the URL to ODM. Chrome MV3 cannot block, so Chrome keeps the
   `downloads.onCreated` path.
4. Add a deduplication guard on the ODM side too (same URL added twice within a few seconds -> ignore).

## 2. Verify (written but never run against the real thing)
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

## 3. Small known issues
- Context menu shows a stray separator when "Show in Folder" is hidden (remote/browser use).
- "Save to" in the details panel always shows the default directory, not the download's own.
- Brief console flash on double-click (console-subsystem exe); needs a separate GUI-subsystem build to remove.
- `internal/queue` has no cancel-during-download test for `Remove(deleteFile)` on active downloads.
- Closing the chooser cancels the download (deliberate; could instead fall back to the browser).
