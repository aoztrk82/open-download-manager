// Package ytdlp runs yt-dlp for sites that do not hand out plain files or
// playlists (YouTube and ~1800 others): reading what a URL offers (qualities,
// playlist entries) and downloading one video with progress.
//
// yt-dlp is a separate program (Unlicense), shipped by the installer next to
// odm.exe together with qjs.exe (QuickJS-NG, MIT), the small JavaScript runtime
// yt-dlp needs for YouTube, and ODM's own ffmpeg for merging video and audio.
package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aoztrk82/open-download-manager/internal/ffmpeg"
)

// EnvPath overrides discovery, e.g. ODM_YTDLP=D:\tools\yt-dlp.exe.
const EnvPath = "ODM_YTDLP"

var ErrNotFound = errors.New("yt-dlp not found (reinstall ODM, or set " + EnvPath + ")")

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// Find returns the yt-dlp to use: $ODM_YTDLP, next to odm.exe, then PATH.
func Find() (string, error) {
	if p := strings.TrimSpace(os.Getenv(EnvPath)); p != "" {
		if isFile(p) {
			return p, nil
		}
		return "", fmt.Errorf("%s=%s: no such file", EnvPath, p)
	}
	if self, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(self), exe("yt-dlp")); isFile(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p, nil
	}
	return "", ErrNotFound
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// baseArgs are passed to every run. A user's own yt-dlp config must not change
// what ODM asked for, and yt-dlp gets ODM's ffmpeg and, when installed next to
// it, QuickJS for YouTube's JavaScript challenges.
func baseArgs(ytdlp string) []string {
	args := []string{"--ignore-config", "--no-colors", "--encoding", "utf-8"}
	for _, dir := range []string{filepath.Dir(ytdlp), selfDir()} {
		if dir == "" {
			continue
		}
		if q := filepath.Join(dir, exe("qjs")); isFile(q) {
			args = append(args, "--js-runtimes", "quickjs:"+q)
			break
		}
	}
	if ff, err := ffmpeg.Find(); err == nil {
		args = append(args, "--ffmpeg-location", ff)
	}
	return args
}

func selfDir() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(self)
}

func command(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	// yt-dlp is Python: make its console I/O UTF-8 whatever the Windows code page.
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8")
	prepare(cmd) // no console window; the whole process tree dies with it (proc_*.go)
	return cmd
}

var versions sync.Map // path -> version

// Version returns `yt-dlp --version`, cached per path (it takes a moment to start).
func Version(ctx context.Context, path string) (string, error) {
	if v, ok := versions.Load(path); ok {
		return v.(string), nil
	}
	out, err := run(ctx, command(ctx, path, "--version"))
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(out))
	versions.Store(path, v)
	return v, nil
}

// Update runs `yt-dlp -U` (sites change and yt-dlp follows within days) and
// returns its output. Only works where the exe is writable (per-user install).
func Update(ctx context.Context, path string) (string, error) {
	out, err := run(ctx, command(ctx, path, "-U"))
	versions.Delete(path)
	return strings.TrimSpace(string(out)), err
}

// ---- probing ----

// Quality is one choice for the download dialog.
type Quality struct {
	Height int   `json:"height"` // 0 = audio only
	FPS    int   `json:"fps,omitempty"`
	Size   int64 `json:"size,omitempty"` // best video at this height + best audio, estimated
}

// Entry is one item of a playlist.
type Entry struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Duration float64 `json:"duration,omitempty"`
}

// Info is what a URL offers.
type Info struct {
	Kind      string    `json:"kind"` // "video" | "playlist"
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Uploader  string    `json:"uploader,omitempty"`
	Duration  float64   `json:"duration,omitempty"`
	Thumbnail string    `json:"thumbnail,omitempty"`
	URL       string    `json:"url"`
	Extractor string    `json:"extractor,omitempty"`
	Qualities []Quality `json:"qualities,omitempty"` // best first; last is audio only when available
	Entries   []Entry   `json:"entries,omitempty"`
}

// Probe asks yt-dlp what url offers. Playlists are listed flat (titles and
// URLs only): reading every entry would take a request per video.
func Probe(ctx context.Context, path, url string) (*Info, error) {
	args := append(baseArgs(path), "-J", "--flat-playlist", "--no-warnings", "--", url)
	out, err := run(ctx, command(ctx, path, args...))
	if err != nil {
		return nil, err
	}
	return parseInfo(out)
}

type rawFormat struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	Height         int     `json:"height"`
	FPS            float64 `json:"fps"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	Filesize       int64   `json:"filesize"`
	FilesizeApprox int64   `json:"filesize_approx"`
	TBR            float64 `json:"tbr"`
	Protocol       string  `json:"protocol"`
}

type rawInfo struct {
	Type       string      `json:"_type"`
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Uploader   string      `json:"uploader"`
	Channel    string      `json:"channel"`
	Duration   float64     `json:"duration"`
	Thumbnail  string      `json:"thumbnail"`
	WebpageURL string      `json:"webpage_url"`
	Extractor  string      `json:"extractor_key"`
	Formats    []rawFormat `json:"formats"`
	Entries    []struct {
		ID       string  `json:"id"`
		Title    string  `json:"title"`
		URL      string  `json:"url"`
		Duration float64 `json:"duration"`
	} `json:"entries"`
}

func parseInfo(b []byte) (*Info, error) {
	var r rawInfo
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("yt-dlp output: %w", err)
	}
	in := &Info{ID: r.ID, Title: r.Title, Uploader: r.Uploader, Duration: r.Duration, Thumbnail: r.Thumbnail,
		URL: r.WebpageURL, Extractor: r.Extractor}
	if in.Uploader == "" {
		in.Uploader = r.Channel
	}
	if r.Type == "playlist" {
		in.Kind = "playlist"
		for _, e := range r.Entries {
			if e.URL == "" {
				continue
			}
			in.Entries = append(in.Entries, Entry{ID: e.ID, Title: e.Title, URL: e.URL, Duration: e.Duration})
		}
		return in, nil
	}
	in.Kind = "video"
	in.Qualities = qualities(r.Formats, r.Duration)
	return in, nil
}

func size(f rawFormat, duration float64) int64 {
	switch {
	case f.Filesize > 0:
		return f.Filesize
	case f.FilesizeApprox > 0:
		return f.FilesizeApprox
	case f.TBR > 0 && duration > 0:
		return int64(f.TBR * 1000 / 8 * duration)
	}
	return 0
}

// qualities lists the heights on offer, best first, each sized as its largest
// video stream plus the best audio (what "bv*[height<=H]+ba" picks, roughly),
// then an audio-only entry.
func qualities(fs []rawFormat, duration float64) []Quality {
	var bestAudio int64
	hasAudio := false
	byHeight := map[int]Quality{}
	for _, f := range fs {
		if f.Protocol == "mhtml" { // storyboards (thumbnail sheets)
			continue
		}
		video := f.VCodec != "" && f.VCodec != "none"
		audio := f.ACodec != "" && f.ACodec != "none"
		if audio && !video {
			hasAudio = true
			bestAudio = max(bestAudio, size(f, duration))
		}
		if !video || f.Height <= 0 {
			continue
		}
		q := byHeight[f.Height]
		q.Height = f.Height
		q.FPS = max(q.FPS, int(f.FPS+0.5))
		q.Size = max(q.Size, size(f, duration))
		byHeight[f.Height] = q
	}
	out := make([]Quality, 0, len(byHeight)+1)
	for _, q := range byHeight {
		if q.Size > 0 {
			q.Size += bestAudio
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Height > out[j].Height })
	if hasAudio {
		out = append(out, Quality{Height: 0, Size: bestAudio})
	}
	return out
}

// ---- downloading ----

// Options for one download.
type Options struct {
	URL string
	// Format is a yt-dlp format selector; empty = FormatFor(0).
	Format string
	// Container for merged video+audio: "mp4" (default) or "mkv".
	Container string
	Dir       string
	// Filename (any extension is replaced by yt-dlp's real one); empty = the title.
	Filename string
	// RateLimit in bytes/sec, 0 = unlimited (yt-dlp's own --limit-rate; the
	// shared total limit cannot reach another process).
	RateLimit int64
}

// FormatFor is the selector for a quality: 0 = best, -1 = audio only, else the
// best video up to that height with the best audio.
func FormatFor(height int) string {
	switch {
	case height < 0:
		return "ba[ext=m4a]/ba/b"
	case height == 0:
		return "bv*+ba/b"
	default:
		h := strconv.Itoa(height)
		return "bv*[height<=" + h + "]+ba/b[height<=" + h + "]"
	}
}

// Progress is reported while yt-dlp works. A video is usually two downloads
// (video, then audio) followed by a merge.
type Progress struct {
	Downloaded int64  // bytes so far, all streams
	Total      int64  // best known total, 0 = unknown
	Part       string // file being written (yt-dlp's .part), for "delete file"
	Merging    bool
}

const (
	tagStart = "ODMSTART"
	tagProg  = "ODMPROG"
	tagFile  = "ODMFILE"
	tagMerge = "[Merger]"
)

// DownloadArgs builds the command line (without baseArgs).
func DownloadArgs(o Options) []string {
	format := o.Format
	if format == "" {
		format = FormatFor(0)
	}
	container := "mp4"
	if o.Container == "mkv" {
		container = "mkv"
	}
	tmpl := "%(title).150B.%(ext)s"
	if name := strings.TrimSpace(o.Filename); name != "" {
		// "<title> (1080p).mp4" -> "<title> (1080p).%(ext)s": the real extension
		// depends on the formats (m4a, webm, mkv, ...).
		if ext := filepath.Ext(name); len(ext) >= 2 && len(ext) <= 5 && !strings.ContainsAny(ext, " ()") {
			name = strings.TrimSuffix(name, ext)
		}
		tmpl = strings.ReplaceAll(filepath.Base(name), "%", "%%") + ".%(ext)s"
	}
	args := []string{
		"--no-playlist", "--continue", "--newline", "--quiet", "--progress", "--no-warnings",
		"-f", format, "--merge-output-format", container,
		"-o", filepath.Join(o.Dir, tmpl),
		"--progress-template", "download:" + tagProg + " %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s %(info.format_id)s %(progress.tmpfilename)s",
		"--print", "before_dl:" + tagStart + " %(filesize,filesize_approx)s",
		"--print", "after_move:" + tagFile + " %(filepath)s",
	}
	if container == "mp4" {
		// MP4 means "plays everywhere": at the same resolution prefer H.264 + AAC
		// (YouTube's default pick is AV1, which older Windows cannot play without
		// an extra codec). Above 1080p YouTube has no H.264; VP9/AV1 then. MKV
		// keeps yt-dlp's own best-quality order.
		args = append(args, "-S", "res,fps,vcodec:h264,ext:mp4:m4a")
	}
	if o.RateLimit > 0 {
		args = append(args, "--limit-rate", strconv.FormatInt(o.RateLimit, 10))
	}
	return append(args, "--", o.URL)
}

// Download runs yt-dlp and returns the final file. Cancelling ctx stops the
// whole process tree; yt-dlp resumes its .part files on the next run.
func Download(ctx context.Context, path string, o Options, onProgress func(Progress)) (string, error) {
	cmd := command(ctx, path, append(baseArgs(path), DownloadArgs(o)...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr tail
	cmd.Stderr = &stderr
	cleanup, err := start(cmd)
	if err != nil {
		return "", err
	}
	defer cleanup()
	var (
		final         string
		total         int64
		done          int64 // bytes of streams already finished
		cur           string
		curBytes      int64
		lastFormatEnd int64
	)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, tagStart+" "):
			if n, err := strconv.ParseFloat(strings.TrimSpace(line[len(tagStart):]), 64); err == nil && n > 0 {
				total = int64(n)
			}
		case strings.HasPrefix(line, tagProg+" "):
			f := strings.SplitN(line[len(tagProg)+1:], " ", 5)
			if len(f) < 5 {
				continue
			}
			got, _ := strconv.ParseFloat(f[0], 64)
			tot, _ := strconv.ParseFloat(f[1], 64)
			if tot <= 0 {
				tot, _ = strconv.ParseFloat(f[2], 64)
			}
			if f[3] != cur { // next stream (video -> audio)
				done += lastFormatEnd
				cur = f[3]
			}
			curBytes, lastFormatEnd = int64(got), int64(got)
			t := total
			if t < done+int64(tot) {
				t = done + int64(tot)
			}
			if onProgress != nil {
				onProgress(Progress{Downloaded: done + curBytes, Total: t, Part: f[4]})
			}
		case strings.HasPrefix(line, tagFile+" "):
			final = strings.TrimSpace(line[len(tagFile):])
		case strings.HasPrefix(line, tagMerge):
			if onProgress != nil {
				onProgress(Progress{Downloaded: done + curBytes, Total: max(total, done+curBytes), Merging: true})
			}
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("yt-dlp: %s", stderr.message(err))
	}
	if final == "" {
		return "", errors.New("yt-dlp finished without reporting the file")
	}
	return final, nil
}

// run executes cmd and returns stdout; errors carry yt-dlp's last ERROR line.
func run(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var out bytes.Buffer
	var stderr tail
	cmd.Stdout, cmd.Stderr = &out, &stderr
	cleanup, err := start(cmd)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return out.Bytes(), fmt.Errorf("yt-dlp: %s", stderr.message(err))
	}
	return out.Bytes(), nil
}

// tail keeps the end of stderr for error messages.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 16<<10 {
		t.buf = t.buf[len(t.buf)-16<<10:]
	}
	return len(p), nil
}

// message is the most useful line: the last "ERROR:", else the last line.
func (t *tail) message(err error) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "ERROR:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "ERROR:"))
		}
	}
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

var _ io.Writer = (*tail)(nil)
