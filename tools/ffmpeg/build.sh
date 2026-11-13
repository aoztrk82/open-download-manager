#!/usr/bin/env bash
# Builds the small ffmpeg.exe that ships with ODM's installer.
#
# ODM downloads media itself (segments, decryption, retries); ffmpeg only has to
# *remux* local files: MPEG-TS / fragmented MP4 / WebM pieces -> one .mp4/.mkv,
# with "-c copy". So this build has no encoders, only containers, parsers,
# bitstream filters and the few decoders stream probing needs. Result: a few MB instead of the
# ~100-200 MB of a general-purpose build, and LGPL (no --enable-gpl/nonfree).
#
# Run from the repo root in an MSYS2 MINGW64 shell (needs mingw-w64-x86_64-gcc,
# make, diffutils, tar):
#   C:\msys64\usr\bin\bash.exe -lc "cd '<repo>' && MSYSTEM=MINGW64 tools/ffmpeg/build.sh"
# Output: dist/ffmpeg/ffmpeg.exe plus the license and build notes the LGPL asks for.
set -euo pipefail

source "$(dirname "$0")/components.sh"

ROOT="$(pwd)"
SRC="$ROOT/dist/ffmpeg-src"
OUT="$ROOT/dist/ffmpeg"
export PATH="/mingw64/bin:$PATH"

mkdir -p "$SRC" "$OUT"
tarball="$SRC/ffmpeg-$VERSION.tar.xz"
[ -f "$tarball" ] || curl -sSfL -o "$tarball" "$URL"
echo "$SHA256  $tarball" | sha256sum -c -

work="$SRC/ffmpeg-$VERSION"
rm -rf "$work"
tar -xJf "$tarball" -C "$SRC"
cd "$work"

CONFIGURE=(
  --prefix="$SRC/install"
  --target-os=mingw32 --arch=x86_64
  --extra-ldflags="-static -static-libgcc"
  --enable-w32threads
  "${COMMON_CONFIGURE[@]}"
)
./configure "${CONFIGURE[@]}"
make -j"$(nproc)"
strip ffmpeg.exe

cp ffmpeg.exe "$OUT/ffmpeg.exe"
cp COPYING.LGPLv2.1 "$OUT/ffmpeg-LICENSE.txt"
cat > "$OUT/ffmpeg-README.txt" <<EOF
This ffmpeg.exe is FFmpeg $VERSION (https://ffmpeg.org), licensed under the
GNU Lesser General Public License 2.1 or later (see ffmpeg-LICENSE.txt).
ODM runs it as a separate program to join downloaded media pieces; it is not
linked into ODM. It was built from the unmodified release source:

  $URL
  sha256 $SHA256

with this configuration (MSYS2 MINGW64, $(gcc --version | head -1)):

  ./configure ${CONFIGURE[*]}

The build script is tools/ffmpeg/build.sh in the ODM repository.
EOF
ls -l "$OUT"
"$OUT/ffmpeg.exe" -hide_banner -version | head -1
