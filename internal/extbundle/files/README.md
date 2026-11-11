# Bundled browser extensions

Filled by the extension build, embedded into odm.exe by `internal/extbundle`.

- `chrome/` - unpacked Chrome/Edge build, written by `cd extension && pnpm build` (not committed).
- `odm-firefox.xpi` - Mozilla-signed (unlisted) Firefox build, written by `cd extension && pnpm sign`.
  Committed, because it cannot be rebuilt without the owner's AMO API keys.

Build the extension before `go build` or odm.exe reports the extensions as "not bundled".
