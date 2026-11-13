package hls

import (
	"strings"
	"testing"
)

// Captured from tranimeizle.live (JW Player) on 2026-11-13: served as
// application/octet-stream from a session-bound /w/<token> URL.
const animeMaster = `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-STREAM-INF:BANDWIDTH=7823504,AVERAGE-BANDWIDTH=3114814,RESOLUTION=1920x1080,CODECS="avc1.4d4028,mp4a.40.2"
/w/TOKEN/v0

#EXT-X-STREAM-INF:BANDWIDTH=4577358,AVERAGE-BANDWIDTH=2004592,RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2"
/w/TOKEN/v1

#EXT-X-STREAM-INF:BANDWIDTH=2089974,AVERAGE-BANDWIDTH=949413,RESOLUTION=854x480,CODECS="avc1.4d401e,mp4a.40.2"
/w/TOKEN/v2
`

// Its media playlist: plain MPEG-TS segments disguised as .jpeg on rotating hosts.
const animeMedia = `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXTINF:2.002000,
https://sv65.robintraco.top/plateau/6371c923/v1/file000.jpeg
#EXTINF:2.002000,
https://sv27.robintraco.top/plateau/6371c923/v1/file001.jpeg
#EXTINF:1.501000,
https://sv41.robintraco.top/plateau/6371c923/v1/file002.jpeg
#EXT-X-ENDLIST
`

func TestParseMaster(t *testing.T) {
	v, err := ParseMaster(animeMaster, "https://www.tranimeizle.live/w/TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 3 || v[0].Height != 1080 || v[2].Height != 480 {
		t.Fatalf("variants: %+v", v)
	}
	if v[0].URL != "https://www.tranimeizle.live/w/TOKEN/v0" || v[0].Bandwidth != 3114814 {
		t.Fatalf("best variant: %+v (want the relative URI resolved, AVERAGE-BANDWIDTH)", v[0])
	}
	if !IsMaster(animeMaster) || IsMaster(animeMedia) {
		t.Fatal("IsMaster")
	}
}

func TestParseMediaPlainTS(t *testing.T) {
	m, err := ParseMedia(animeMedia, "https://www.tranimeizle.live/w/TOKEN/v1")
	if err != nil {
		t.Fatal(err)
	}
	if m.Segments != 3 || len(m.Items) != 3 || m.NeedsFFmpeg {
		t.Fatalf("segments=%d items=%d ffmpeg=%v", m.Segments, len(m.Items), m.NeedsFFmpeg)
	}
	if d := m.Duration; d < 5.50 || d > 5.51 {
		t.Fatalf("duration %v", d)
	}
	if it := m.Items[1]; it.URL != "https://sv27.robintraco.top/plateau/6371c923/v1/file001.jpeg" || it.Local != "s00001.ts" || it.Length != -1 {
		t.Fatalf("item: %+v", it)
	}
	for _, want := range []string{"#EXT-X-MEDIA-SEQUENCE:0", "\ns00000.ts\n", "\ns00002.ts\n", "#EXT-X-ENDLIST"} {
		if !strings.Contains(m.Local, want) {
			t.Fatalf("local playlist lacks %q:\n%s", want, m.Local)
		}
	}
	if strings.Contains(m.Local, "robintraco") {
		t.Fatal("local playlist still points at the network")
	}
}

func TestParseMediaKeysMapsRanges(t *testing.T) {
	const p = `#EXTM3U
#EXT-X-TARGETDURATION:4
#EXT-X-MAP:URI="init.mp4",BYTERANGE="720@0"
#EXT-X-KEY:METHOD=AES-128,URI="https://keys.example/k?id=1,2",IV=0x000102030405060708090a0b0c0d0e0f
#EXTINF:4,
#EXT-X-BYTERANGE:1000@720
media.mp4
#EXTINF:4,
#EXT-X-BYTERANGE:500
media.mp4
#EXT-X-KEY:METHOD=AES-128,URI="https://keys.example/k?id=1,2"
#EXTINF:4,
other.m4s
#EXT-X-ENDLIST
`
	m, err := ParseMedia(p, "https://cdn.example/v/index.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	kinds := ""
	for _, it := range m.Items {
		kinds += it.Kind[:1]
	}
	if kinds != "iksss" {
		t.Fatalf("items %q (want init, one shared key, 3 segments)", kinds)
	}
	init, key, s0, s1 := m.Items[0], m.Items[1], m.Items[2], m.Items[3]
	if init.URL != "https://cdn.example/v/init.mp4" || init.Offset != 0 || init.Length != 720 {
		t.Fatalf("init %+v", init)
	}
	if key.URL != "https://keys.example/k?id=1,2" || key.Local != "k0.key" {
		t.Fatalf("key %+v (quoted URI with a comma)", key)
	}
	if s0.Offset != 720 || s0.Length != 1000 || s1.Offset != 1720 || s1.Length != 500 {
		t.Fatalf("byte ranges: %+v %+v", s0, s1)
	}
	if !m.NeedsFFmpeg || s0.Local != "s00000.m4s" {
		t.Fatalf("fMP4 + key must need ffmpeg and use .m4s names: %+v", s0)
	}
	for _, want := range []string{`#EXT-X-MAP:URI="init0.mp4"`, `URI="k0.key",IV=0x0001`, "\ns00002.m4s\n"} {
		if !strings.Contains(m.Local, want) {
			t.Fatalf("local playlist lacks %q:\n%s", want, m.Local)
		}
	}
	if strings.Contains(m.Local, "BYTERANGE") {
		t.Fatal("byte ranges must be dropped: the local files are the ranges")
	}
}

func TestParseMediaRefuses(t *testing.T) {
	cases := map[string]string{
		"live":     "#EXTM3U\n#EXTINF:2,\na.ts\n",
		"drm":      "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://x\"\n#EXTINF:2,\na.ts\n#EXT-X-ENDLIST\n",
		"widevine": "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"https://k\",KEYFORMAT=\"com.widevine\"\n#EXTINF:2,\na.ts\n#EXT-X-ENDLIST\n",
		"master":   animeMaster,
		"html":     "<html>nope</html>",
		"empty":    "#EXTM3U\n#EXT-X-ENDLIST\n",
		"scheme":   "#EXTM3U\n#EXTINF:2,\nfile:///etc/passwd\n#EXT-X-ENDLIST\n",
	}
	for name, p := range cases {
		if _, err := ParseMedia(p, "https://x/i.m3u8"); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
