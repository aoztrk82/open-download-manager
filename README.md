# open-download-manager

Versatile download manager: one Go binary (`odm`) that is both the daemon/server and the CLI, plus Chrome and Firefox extensions that hand browser downloads to it.

- Protocols: HTTP/HTTPS (segmented, resumable) and FTP/FTPS
- Persistent queue (SQLite), pause/resume, restart recovery
- Web UI served by the daemon
- Local mode and remote server mode use the same HTTP/WebSocket API

## Usage

```
go build -o odm.exe ./cmd/odm

odm add https://example.com/file.iso      # auto-starts the local daemon
odm list
odm watch
odm pause|resume|rm <id>

odm serve --local                          # 127.0.0.1:6800, token in %APPDATA%\odm\token
odm serve --listen :6800 --token SECRET    # remote server (+ Web UI at /)
odm --server http://host:6800 --token SECRET add <url>
```

## Browser extensions

```
cd extension && pnpm install && pnpm build
```

Load `extension/dist/chrome` (chrome://extensions, Developer mode, Load unpacked) or `extension/dist/firefox`
(about:debugging, Load Temporary Add-on, pick `manifest.json`). In the extension settings enter the ODM URL and token.

## Layout

```
cmd/odm            CLI + serve command
internal/engine    segmented/resumable download engine
internal/proto     HTTP and FTP sources behind one interface
internal/store     SQLite persistence
internal/queue     scheduler, state saving, events
internal/api       REST + WebSocket server and Go client
internal/web       embedded Web UI
extension/         shared TypeScript, built for Chrome and Firefox
```
