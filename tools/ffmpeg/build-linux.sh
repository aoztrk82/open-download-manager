#!/usr/bin/env bash
# Builds ODM's small ffmpeg for Linux (the Docker image; see Dockerfile).
# Same components as the Windows build (components.sh): remux only, LGPL.
#   bash tools/ffmpeg/build-linux.sh <out-dir>
# Needs gcc, make, curl, xz. Output: <out-dir>/ffmpeg plus license and notes.
set -euo pipefail
source "$(dirname "$0")/components.sh"

OUT="${1:?usage: build-linux.sh <out-dir>}"
SRC="$(mktemp -d)"
mkdir -p "$OUT"
tarball="$SRC/ffmpeg-$VERSION.tar.xz"
curl -sSfL -o "$tarball" "$URL"
echo "$SHA256  $tarball" | sha256sum -c -
tar -xJf "$tarball" -C "$SRC"
cd "$SRC/ffmpeg-$VERSION"

CONFIGURE=(
  --prefix="$SRC/install"
  --enable-pthreads
  --extra-ldflags="-static"
  "${COMMON_CONFIGURE[@]}"
)
./configure "${CONFIGURE[@]}"
make -j"$(nproc)"
strip ffmpeg

cp ffmpeg "$OUT/ffmpeg"
cp COPYING.LGPLv2.1 "$OUT/ffmpeg-LICENSE.txt"
cat > "$OUT/ffmpeg-README.txt" <<EOF
This ffmpeg is FFmpeg $VERSION (https://ffmpeg.org), licensed under the GNU
Lesser General Public License 2.1 or later (see ffmpeg-LICENSE.txt). ODM runs
it as a separate program to join downloaded media pieces; it is not linked into
ODM. It was built from the unmodified release source:

  $URL
  sha256 $SHA256

with this configuration ($(gcc --version | head -1)):

  ./configure ${CONFIGURE[*]}

The build script is tools/ffmpeg/build-linux.sh in the ODM repository.
EOF
"$OUT/ffmpeg" -hide_banner -version | head -1
rm -rf "$SRC"
