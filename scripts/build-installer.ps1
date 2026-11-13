# Builds dist\odm-setup-<version>.exe: extension -> odm.exe -> small ffmpeg -> Inno Setup.
# Run from anywhere: powershell -ExecutionPolicy Bypass -File scripts\build-installer.ps1
# Needs: Go, Node + pnpm, MSYS2 (C:\msys64, mingw-w64-x86_64-gcc make diffutils) for
# ffmpeg (only when dist\ffmpeg is missing or -RebuildFFmpeg), Inno Setup 6.
param([switch]$RebuildFFmpeg)
$ErrorActionPreference = "Stop"
$repo = Split-Path $PSScriptRoot
Set-Location $repo

function Step($name) { Write-Host "== $name" -ForegroundColor Cyan }
function Check($what) { if ($LASTEXITCODE -ne 0) { throw "$what failed (exit $LASTEXITCODE)" } }

# The exe version is the single source for the installer version.
$version = (Select-String -Path internal\api\api.go -Pattern '^const Version = "([^"]+)"').Matches[0].Groups[1].Value
if (-not $version) { throw "Version not found in internal/api/api.go" }

Step "extension"
Push-Location extension
pnpm install --frozen-lockfile; Check "pnpm install"
pnpm build; Check "pnpm build"
Pop-Location

Step "odm.exe $version"
New-Item -ItemType Directory -Force dist | Out-Null
# -s -w: no symbol tables (smaller download); stack traces still have file:line.
go build -trimpath -ldflags "-s -w" -o dist\odm.exe ./cmd/odm; Check "go build"

if ($RebuildFFmpeg -or -not (Test-Path dist\ffmpeg\ffmpeg.exe)) {
  Step "ffmpeg (MSYS2)"
  $msys = "C:\msys64\usr\bin\bash.exe"
  if (-not (Test-Path $msys)) { throw "MSYS2 not found at C:\msys64 (needed to build ffmpeg)" }
  $unixRepo = "/" + $repo.Substring(0, 1).ToLower() + ($repo.Substring(2) -replace "\\", "/")
  & $msys -lc "cd '$unixRepo' && MSYSTEM=MINGW64 bash tools/ffmpeg/build.sh"; Check "ffmpeg build"
}

Step "yt-dlp + QuickJS (pinned downloads)"
& (Join-Path $PSScriptRoot "fetch-tools.ps1")

Step "installer"
$iscc = @("$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe", "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe") |
  Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $iscc) { throw "Inno Setup 6 not found (winget install JRSoftware.InnoSetup --scope user)" }
# Antivirus scanning the fresh setup exe can make ISCC's resource update fail
# ("EndUpdateResource failed (110)"); a retry a moment later passes.
for ($try = 1; ; $try++) {
  & $iscc /Q "/DAppVersion=$version" installer\odm.iss
  if ($LASTEXITCODE -eq 0) { break }
  if ($try -ge 3) { Check "ISCC" }
  Write-Host "ISCC failed (exit $LASTEXITCODE), retrying..." -ForegroundColor Yellow
  Start-Sleep -Seconds 3
}

$out = Get-Item "dist\odm-setup-$version.exe"
Write-Host ("built {0} ({1:N1} MB)" -f $out.FullName, ($out.Length / 1MB)) -ForegroundColor Green
