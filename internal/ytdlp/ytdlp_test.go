package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// testdata/bigbuckbunny.json: yt-dlp 2026.10.26 -J for Blender's "Big Buck
// Bunny 60fps 4K" (youtube.com/watch?v=aqz-KE-bpKQ), trimmed to the fields read.
func TestParseInfoVideo(t *testing.T) {
	b, err := os.ReadFile("testdata/bigbuckbunny.json")
	if err != nil {
		t.Fatal(err)
	}
	in, err := parseInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != "video" || in.ID != "aqz-KE-bpKQ" || !strings.Contains(in.Title, "Big Buck Bunny") || in.Duration < 600 {
		t.Fatalf("info: %+v", in)
	}
	var heights []int
	for _, q := range in.Qualities {
		heights = append(heights, q.Height)
	}
	if want := []int{2160, 1440, 1080, 720, 480, 360, 240, 144, 0}; !reflect.DeepEqual(heights, want) {
		t.Fatalf("heights %v, want %v (best first, audio only last)", heights, want)
	}
	q := in.Qualities[0]
	if q.FPS != 60 || q.Size < 1<<30 {
		t.Fatalf("2160p: %+v (60 fps, over 1 GiB with audio)", q)
	}
	audio := in.Qualities[len(in.Qualities)-1]
	if audio.Size <= 0 || audio.Size > 50<<20 {
		t.Fatalf("audio only: %+v", audio)
	}
	for i := 1; i < len(in.Qualities)-1; i++ {
		if in.Qualities[i].Size <= audio.Size {
			t.Fatalf("%dp has no size of its own: %+v", in.Qualities[i].Height, in.Qualities[i])
		}
	}
}

func TestParseInfoPlaylist(t *testing.T) {
	const flat = `{"_type":"playlist","id":"PL1","title":"My list","uploader":"Someone","webpage_url":"https://www.youtube.com/playlist?list=PL1",
	 "entries":[{"_type":"url","id":"a1","title":"First","url":"https://www.youtube.com/watch?v=a1","duration":61},
	            {"_type":"url","id":"a2","title":"[Private video]","url":""},
	            {"_type":"url","id":"a3","title":"Third","url":"https://www.youtube.com/watch?v=a3"}]}`
	in, err := parseInfo([]byte(flat))
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != "playlist" || in.Title != "My list" || len(in.Entries) != 2 {
		t.Fatalf("playlist: %+v (entries without a URL are skipped)", in)
	}
	if e := in.Entries[0]; e.URL != "https://www.youtube.com/watch?v=a1" || e.Duration != 61 {
		t.Fatalf("entry %+v", e)
	}
	if _, err := parseInfo([]byte("not json")); err == nil {
		t.Fatal("bad JSON must fail")
	}
}

func TestFormatFor(t *testing.T) {
	for h, want := range map[int]string{
		0:    "bv*+ba/b",
		-1:   "ba[ext=m4a]/ba/b",
		1080: "bv*[height<=1080]+ba/b[height<=1080]",
	} {
		if got := FormatFor(h); got != want {
			t.Errorf("FormatFor(%d) = %q, want %q", h, got, want)
		}
	}
}

func TestDownloadArgs(t *testing.T) {
	a := DownloadArgs(Options{URL: "https://youtu.be/x", Dir: `C:\dl`, Filename: "Bölüm 1 (1080p).mp4", Format: FormatFor(1080)})
	s := strings.Join(a, "\x00")
	for _, want := range []string{
		"--no-playlist", "-f\x00bv*[height<=1080]+ba/b[height<=1080]", "--merge-output-format\x00mp4",
		"-o\x00" + filepath.Join(`C:\dl`, "Bölüm 1 (1080p).%(ext)s"), "-S\x00res,fps,vcodec:h264,ext:mp4:m4a",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("args lack %q:\n%q", want, a)
		}
	}
	if a[len(a)-2] != "--" || a[len(a)-1] != "https://youtu.be/x" {
		t.Fatalf("the URL must come after --, so it can never be read as an option: %q", a[len(a)-2:])
	}

	a = DownloadArgs(Options{URL: "u", Dir: "d", Container: "mkv", Filename: "50% off. Ep. 5"})
	s = strings.Join(a, "\x00")
	if !strings.Contains(s, "--merge-output-format\x00mkv") || strings.Contains(s, "-S\x00") {
		t.Fatalf("mkv: %q", a)
	}
	if !strings.Contains(s, filepath.Join("d", "50%% off. Ep. 5.%(ext)s")) {
		t.Fatalf("%% must be escaped and a fake extension kept: %q", a)
	}
	if !strings.Contains(strings.Join(DownloadArgs(Options{URL: "u", Dir: "d"}), " "), "%(title).150B.%(ext)s") {
		t.Fatal("no name: the title")
	}
}

func TestTailMessage(t *testing.T) {
	var tl tail
	_, _ = tl.Write([]byte("[youtube] x: Downloading\nERROR: [youtube] x: Video unavailable\n"))
	if m := tl.message(nil); m != "[youtube] x: Video unavailable" {
		t.Fatalf("message %q", m)
	}
}

// Real yt-dlp against YouTube: ODM_YTDLP_NET=1 to run (network, ~30 MB).
func TestRealDownload(t *testing.T) {
	if os.Getenv("ODM_YTDLP_NET") == "" {
		t.Skip("set ODM_YTDLP_NET=1 to download from YouTube")
	}
	path, err := Find()
	if err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	in, err := Probe(ctx, path, "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d qualities", in.Title, len(in.Qualities))
	var last Progress
	file, err := Download(ctx, path, Options{URL: in.URL, Format: FormatFor(240), Dir: t.TempDir(), Filename: "Big Buck Bunny (240p).mp4"},
		func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(file)
	if err != nil || filepath.Base(file) != "Big Buck Bunny (240p).mp4" || st.Size() < 1<<20 {
		t.Fatalf("file %q: %v", file, err)
	}
	if last.Downloaded <= 0 || last.Total <= 0 {
		t.Fatalf("no progress seen: %+v", last)
	}
}
