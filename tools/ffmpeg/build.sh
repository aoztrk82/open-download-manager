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

VERSION=9.0.2
# Verified against ffmpeg.org's detached signature (FFmpeg release signing key
# FCF9 86EA 15E6 E293 A564 4F10 B432 2F04 D676 58D8) on 2026-11-13.
SHA256=8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e
URL="https://ffmpeg.org/releases/ffmpeg-$VERSION.tar.xz"

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

# Containers ODM may meet when saving web video, in and out.
DEMUXERS=mpegts,mov,matroska,hls,concat,aac,mp3,ac3,eac3,flac,ogg,wav,webvtt,flv
MUXERS=mp4,mov,ipod,matroska,webm,mpegts,adts,mp3,ogg,opus,flac,webvtt
PARSERS=h264,hevc,av1,vp8,vp9,mpeg4video,aac,aac_latm,ac3,mpegaudio,opus,vorbis,flac
# Not for transcoding: stream probing decodes a few frames to learn the picture
# size and audio layout. Without them MPEG-TS -> MP4 fails ("dimensions not set").
DECODERS=h264,hevc,vp9,mpeg4,aac,mp3,ac3,opus,vorbis
# aac_adtstoasc: AAC from MPEG-TS into MP4. The others are what the muxers
# above insert by themselves when stream copying.
BSFS=aac_adtstoasc,h264_mp4toannexb,hevc_mp4toannexb,extract_extradata,vp9_superframe,av1_frame_merge,dump_extradata

CONFIGURE=(
  --prefix="$SRC/install"
  --target-os=mingw32 --arch=x86_64
  --enable-static --disable-shared --pkg-config-flags=--static
  --extra-ldflags="-static -static-libgcc"
  --disable-autodetect --enable-w32threads
  --disable-everything --disable-network --disable-doc --disable-debug
  --disable-ffplay --disable-ffprobe --disable-avdevice --disable-swscale --disable-x86asm
  --enable-small
  --enable-protocol=file,pipe,crypto
  --enable-demuxer=$DEMUXERS
  --enable-muxer=$MUXERS
  --enable-parser=$PARSERS
  --enable-decoder=$DECODERS
  --enable-bsf=$BSFS
  --enable-filter=null,anull,format,aformat,trim,atrim,hflip,vflip,transpose
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
