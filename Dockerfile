# syntax=docker/dockerfile:1
# ODM server image: the download daemon with its Web UI, yt-dlp (+ QuickJS for
# YouTube) and ODM's small remux-only ffmpeg. Token mode: set ODM_TOKEN.
#
#   docker build -t odm .
#   docker run -d -p 6800:6800 -e ODM_TOKEN=$(openssl rand -hex 32) \
#     -v ./downloads:/downloads -v ./config:/config odm
#
# Multi-arch (linux/amd64, linux/arm64) via buildx: scripts/docker-build.ps1.

ARG GO_VERSION=1.26
ARG DEBIAN=bookworm

# ---- odm: cross-compiled on the build machine (pure Go, no cgo) ----
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-${DEBIAN} AS odm
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/odm ./cmd/odm

# ---- ffmpeg: the same small LGPL build as the Windows installer ----
FROM debian:${DEBIAN}-slim AS ffmpeg
RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential ca-certificates curl xz-utils \
    && rm -rf /var/lib/apt/lists/*
COPY tools/ffmpeg/components.sh tools/ffmpeg/build-linux.sh /build/
RUN bash /build/build-linux.sh /out

# ---- yt-dlp + QuickJS-NG: pinned releases, checked by SHA-256 ----
FROM debian:${DEBIAN}-slim AS tools
ARG TARGETARCH
ARG YTDLP_VERSION=2026.10.26
ARG QJS_VERSION=v0.17.0
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN set -eu; mkdir -p /out; \
    case "$TARGETARCH" in \
      amd64) yt=yt-dlp_linux;         yt_sha=767ba9dce7052aa196f8b12652f433d76de128ba168966fa84b6394d5e6c6795; \
             qjs=qjs-linux-x86_64;    qjs_sha=0bfc02511a9f549c28b53880d988fc7cd5d361e90c5e8afdfcd7dc6774ceace5 ;; \
      arm64) yt=yt-dlp_linux_aarch64; yt_sha=e87e9533e89fe5889abaa21f5114dfe7756d6aa6219b83551add04d48b18e483; \
             qjs=qjs-linux-aarch64;   qjs_sha=3372133484edf50a69f3c67903af41206d22a061e930e3cfb63269272ef56d2e ;; \
      *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /out/yt-dlp "https://github.com/yt-dlp/yt-dlp/releases/download/$YTDLP_VERSION/$yt"; \
    curl -fsSL -o /out/qjs "https://github.com/quickjs-ng/quickjs/releases/download/$QJS_VERSION/$qjs"; \
    echo "$yt_sha  /out/yt-dlp" | sha256sum -c -; \
    echo "$qjs_sha  /out/qjs" | sha256sum -c -; \
    chmod 0755 /out/yt-dlp /out/qjs; \
    curl -fsSL -o /out/yt-dlp-LICENSE.txt "https://raw.githubusercontent.com/yt-dlp/yt-dlp/$YTDLP_VERSION/LICENSE"; \
    curl -fsSL -o /out/quickjs-ng-LICENSE.txt "https://raw.githubusercontent.com/quickjs-ng/quickjs/$QJS_VERSION/LICENSE"

# ---- the image ----
FROM debian:${DEBIAN}-slim
ARG VERSION=dev
LABEL org.opencontainers.image.title="ODM - Open Download Manager" \
      org.opencontainers.image.description="Segmented, resumable download server with a Web UI, HLS and yt-dlp" \
      org.opencontainers.image.source="https://github.com/aoztrk82/open-download-manager" \
      org.opencontainers.image.version="$VERSION"
# ca-certificates: HTTPS downloads; tzdata: the Later queue's schedule uses TZ.
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 1000 --user-group --create-home --home-dir /home/odm odm \
    && mkdir -p /downloads /config /opt/odm/licenses \
    && chown odm:odm /downloads /config
COPY --from=odm /out/odm /opt/odm/odm
COPY --from=ffmpeg /out/ffmpeg /opt/odm/ffmpeg
COPY --from=ffmpeg /out/ffmpeg-LICENSE.txt /out/ffmpeg-README.txt /opt/odm/licenses/
COPY --from=tools /out/yt-dlp /out/qjs /opt/odm/
COPY --from=tools /out/yt-dlp-LICENSE.txt /out/quickjs-ng-LICENSE.txt /opt/odm/licenses/
# The odm user owns the program folder so "Update yt-dlp" (yt-dlp -U) can
# replace yt-dlp in place.
RUN chown -R odm:odm /opt/odm
# Config, database and logs: /config/odm (os.UserConfigDir = $XDG_CONFIG_HOME).
ENV XDG_CONFIG_HOME=/config \
    TZ=UTC
USER odm
WORKDIR /downloads
VOLUME ["/downloads", "/config"]
EXPOSE 6800
ENTRYPOINT ["/opt/odm/odm"]
CMD ["serve", "--listen", ":6800", "--dir", "/downloads"]
