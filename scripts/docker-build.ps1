# Builds the ODM server image and, with -Push, publishes it to Docker Hub.
#   powershell -File scripts\docker-build.ps1                 # local image odm:<version> for testing
#   powershell -File scripts\docker-build.ps1 -Test           # build, then run it and check it answers
#   powershell -File scripts\docker-build.ps1 -Push           # amd64 + arm64 to <Image>:<version> and :latest
# Needs Docker Desktop (buildx included); -Push needs `docker login` first.
# The version comes from internal/api/api.go, like the installer's.
param(
  [string]$Image = "aoztrk82/odm",
  [switch]$Push,
  [switch]$Test,
  [string]$Platforms = "linux/amd64,linux/arm64"
)
$ErrorActionPreference = "Stop"
$repo = Split-Path $PSScriptRoot
Set-Location $repo
function Check($what) { if ($LASTEXITCODE -ne 0) { throw "$what failed (exit $LASTEXITCODE)" } }

$version = (Select-String -Path internal\api\api.go -Pattern '^const Version = "([^"]+)"').Matches[0].Groups[1].Value
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw "docker not found: install Docker Desktop" }
Write-Host "== $Image`:$version" -ForegroundColor Cyan

if ($Push) {
  # arm64 is built under emulation on an x86 PC (ffmpeg takes a while).
  # PowerShell 5.1 turns a native command's stderr into a terminating error under "Stop";
  # a missing builder is expected the first time.
  $ErrorActionPreference = "Continue"
  docker buildx inspect odm-builder *> $null
  $ErrorActionPreference = "Stop"
  if ($LASTEXITCODE -ne 0) { docker buildx create --name odm-builder --driver docker-container | Out-Null; Check "buildx create" }
  docker buildx build --builder odm-builder --platform $Platforms --build-arg "VERSION=$version" `
    -t "$Image`:$version" -t "$Image`:latest" --push .
  Check "docker buildx build --push"
  Write-Host "pushed $Image`:$version and :latest ($Platforms)" -ForegroundColor Green
  return
}

docker build --build-arg "VERSION=$version" -t "odm:$version" -t "odm:latest" .
Check "docker build"
docker image ls "odm:$version"

if ($Test) {
  # Throwaway container on port 6899 with temporary volumes; removed afterwards.
  $token = -join ((1..32) | ForEach-Object { "{0:x2}" -f (Get-Random -Maximum 256) })
  $name = "odm-test-$PID"
  docker run -d --name $name -p 127.0.0.1:6899:6800 -e "ODM_TOKEN=$token" "odm:$version" | Out-Null
  Check "docker run"
  try {
    Start-Sleep -Seconds 3
    $h = @{ Authorization = "Bearer $token" }
    $info = Invoke-RestMethod -Uri http://127.0.0.1:6899/v1/info -Headers $h
    "info: version=$($info.version) ffmpeg=$($info.ffmpeg) ytdlp=$($info.ytdlp) settings=$($info.canSettings) anyFolder=$($info.anyFolder)"
    $media = Invoke-RestMethod -Uri http://127.0.0.1:6899/v1/media -Headers $h
    "yt-dlp: $($media.version)"
    docker exec $name /opt/odm/ffmpeg -hide_banner -version | Select-Object -First 1
    docker exec $name /opt/odm/qjs -e "print('qjs ok')"
    try { Invoke-RestMethod -Uri http://127.0.0.1:6899/v1/info | Out-Null; "WITHOUT TOKEN: answered (bad)" } catch { "without token: refused (good)" }
  } finally {
    docker logs --tail 20 $name
    docker rm -f $name | Out-Null
  }
}
