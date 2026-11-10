# History

Newest first. Commit hashes refer to `git log`.

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
