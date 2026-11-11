# Roadmap

Product idea: a download manager with a **server** and a **local** side that the browser can choose between per
download. One Go core shared by CLI, server and tray app; one TypeScript codebase for both browsers.

## Now (stabilize v0.1)
- Fix browser capture reliability in Firefox (see TODO #1), then confirm on Chrome.
- Run through the verification checklist in TODO #2 and fix what breaks.

## Next
- **Ask where to download in more places:** Web UI "Add" dialog and CLI (`odm add --to server|local`) should offer the
  same local/server choice as the extension; remember last choice per site (optional).
- **Remote servers as first-class:** named servers (several), per-server default folder, show the server's
  downloads from the local UI, "move this download to server".
- **Shared API contract:** write `api/openapi.yaml` and generate Go types + TS client from it (the original plan;
  today the TS client in `extension/src/api.ts` and the Go types are kept in sync by hand).
- **FTP/FTPS hardening:** real-server tests, passive-mode edge cases, resume verification.
- **Engine:** checksum verification, per-download speed limit and connections, global schedule/bandwidth limit,
  proxy support, retry policy surfaced in the UI, redirect/filename handling edge cases.
- **Server hardening:** TLS (or documented reverse proxy), multiple users/tokens, rate limiting, audit log.
- **WebSocket events in the UI** instead of 1 s polling (the API already streams `/v1/events`).

## Later
- Packaging: goreleaser (Windows/Linux), signed installer, auto-update, GitHub Actions CI (go test, extension build,
  web-ext lint).
- Extensions are deliberately not published to stores (they install from odm.exe, see CLAUDE.md). Firefox uses
  AMO *unlisted* signing. Revisit the Chrome Web Store only if "Load unpacked" proves too awkward.
- GUI-subsystem build so double-click never flashes a console; proper app icon and tray icon assets.
- Linux/macOS tray (the daemon and CLI already build there; tray, window and native-host registration are Windows only).
- Torrent/magnet and media-site support are explicitly **not** planned.
