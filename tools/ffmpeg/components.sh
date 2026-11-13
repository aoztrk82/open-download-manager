# What ODM's small ffmpeg contains; sourced by build.sh (Windows, MSYS2) and
# build-linux.sh (the Docker image), so both builds are the same program.

VERSION=9.0.2
# Verified against ffmpeg.org's detached signature (FFmpeg release signing key
# FCF9 86EA 15E6 E293 A564 4F10 B432 2F04 D676 58D8) on 2026-11-13.
SHA256=8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e
URL="https://ffmpeg.org/releases/ffmpeg-$VERSION.tar.xz"

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

# Everything else is off: no encoders, no network, no devices, LGPL only.
COMMON_CONFIGURE=(
  --enable-static --disable-shared --pkg-config-flags=--static
  --disable-autodetect
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
