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

odm serve --local                          # 127.0.0.1:6800, no token (loopback and extensions only)
odm serve --listen :6800 --token SECRET    # remote server (+ Web UI at /)
odm --server http://host:6800 --token SECRET add <url>
```

## Windows

Install with `odm-setup-<version>.exe` from the GitHub releases (per user, no admin rights). It brings ODM, a small
ffmpeg, yt-dlp and QuickJS; the browser extensions are installed from ODM (Settings > General).

## Docker (server)

The image runs ODM as a server with its Web UI, yt-dlp (+ QuickJS) and a small ffmpeg, for amd64 and arm64.
A token is required:

```
docker run -d --name odm -p 6800:6800 \
  -e ODM_TOKEN=$(openssl rand -hex 32) -e TZ=Europe/Istanbul \
  -v ./downloads:/downloads -v ./config:/config \
  aoztrk82/odm:latest
```

or `docker compose up -d` with [docker-compose.yml](docker-compose.yml) and `ODM_TOKEN=...` in a `.env` file.
Open `http://<host>:6800`, enter the token. Files land in `/downloads`; the database, settings and the download log
(`/config/odm/logs/odm.log`) in `/config`. The container runs as uid 1000. Building it yourself:
`docker build -t odm .` or `scripts\docker-build.ps1 [-Test] [-Push]`.

In the browser extension's settings add the server URL and the token; each captured download then asks: this
computer or the server.

## Browser extensions

```
cd extension && pnpm install && pnpm build
```

Load `extension/dist/chrome` (chrome://extensions, Developer mode, Load unpacked) or `extension/dist/firefox`
(about:debugging, Load Temporary Add-on, pick `manifest.json`). Run `odm.exe` once so it registers the browser integration; after that the extension can start ODM by itself. No setup is needed for local downloads. Add a server URL + token in the extension settings and each captured download asks: this computer or the server.

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
