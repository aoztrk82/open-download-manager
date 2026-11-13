# Downloads the third-party programs the installer ships next to odm.exe, at
# pinned versions with pinned SHA-256 (a changed file fails the build):
#   yt-dlp.exe  (Unlicense)  YouTube and other video sites
#   qjs.exe     (QuickJS-NG, MIT)  the JavaScript runtime yt-dlp needs for YouTube
# Output: dist\yt-dlp\ (+ license texts). ffmpeg is built from source instead:
# tools/ffmpeg/build.sh.
#
# Bumping: pick a release at least two weeks old, take the SHA-256 from the
# release page (GitHub shows a digest per asset; yt-dlp also publishes
# SHA2-256SUMS), update the pins below and HISTORY.md.
$ErrorActionPreference = "Stop"
$repo = Split-Path $PSScriptRoot
$out = Join-Path $repo "dist\yt-dlp"
New-Item -ItemType Directory -Force $out | Out-Null

$pins = @(
  @{ Name = "yt-dlp.exe"; Sha = "f5389d3698ecde4ef06f4e6b1ce0294fe9ac1396228c8990b8d3416d6d177a7d"
     Url = "https://github.com/yt-dlp/yt-dlp/releases/download/2026.10.26/yt-dlp.exe" },
  @{ Name = "qjs.exe"; Sha = "2aeabf0092c3262d6b2609824418f7dd7ed1f1df939f73b2b15645230cac0d77"
     Url = "https://github.com/quickjs-ng/quickjs/releases/download/v0.17.0/qjs-windows-x86_64.exe" }
)
$licenses = @(
  @{ Name = "yt-dlp-LICENSE.txt"; Url = "https://raw.githubusercontent.com/yt-dlp/yt-dlp/2026.10.26/LICENSE" },
  @{ Name = "quickjs-ng-LICENSE.txt"; Url = "https://raw.githubusercontent.com/quickjs-ng/quickjs/v0.17.0/LICENSE" }
)

foreach ($p in $pins) {
  $dest = Join-Path $out $p.Name
  $ok = (Test-Path $dest) -and ((Get-FileHash $dest -Algorithm SHA256).Hash.ToLower() -eq $p.Sha)
  if (-not $ok) {
    Write-Host "downloading $($p.Name)"
    $tmp = "$dest.download"
    curl.exe -sSfL -o $tmp $p.Url
    if ($LASTEXITCODE -ne 0) { throw "download failed: $($p.Url)" }
    $got = (Get-FileHash $tmp -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $p.Sha) { Remove-Item $tmp; throw "$($p.Name): SHA-256 $got does not match the pinned $($p.Sha)" }
    Move-Item -Force $tmp $dest
  }
  Write-Host "ok $($p.Name)"
}
foreach ($l in $licenses) {
  $dest = Join-Path $out $l.Name
  if (-not (Test-Path $dest)) {
    curl.exe -sSfL -o $dest $l.Url
    if ($LASTEXITCODE -ne 0) { throw "download failed: $($l.Url)" }
  }
  Write-Host "ok $($l.Name)"
}
