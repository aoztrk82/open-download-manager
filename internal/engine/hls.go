package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/ffmpeg"
	"github.com/aoztrk82/open-download-manager/internal/hls"
	"github.com/aoztrk82/open-download-manager/internal/proto"
)

// HLSState is the resumable progress of a streaming download.
type HLSState struct {
	// Variant and Playlist are set when ODM picked a variant from a master
	// playlist itself, so a resume uses the same media playlist.
	Variant  string `json:"variant,omitempty"`
	Playlist string `json:"playlist,omitempty"`
	Done     []bool `json:"done"` // per hls.Media item
	Bytes    int64  `json:"bytes"`
	Segments int    `json:"segments"`
}

const maxPlaylist = 16 << 20

// runHLS downloads every piece of an HLS stream into "<name>.parts", then
// joins them into one file: ffmpeg (decrypts, handles fMP4, writes MP4) when
// available, else plain MPEG-TS concatenation.
func (e *Engine) runHLS(ctx context.Context, st *State, o Options, src proto.Source, onProgress func(State)) error {
	if k := o.Req.Media.Kind; k != proto.MediaHLS {
		return fmt.Errorf("unsupported media kind %q", k)
	}
	if st.HLS == nil {
		st.HLS = &HLSState{}
	}
	h := st.HLS

	text, base := o.Req.Media.Playlist, o.Req.URL
	if h.Playlist != "" {
		text, base = h.Playlist, h.Variant
	}
	var err error
	if text == "" {
		if text, err = fetchText(ctx, src, o.Req); err != nil {
			return fmt.Errorf("playlist: %w", err)
		}
	}
	if hls.IsMaster(text) {
		vs, err := hls.ParseMaster(text, base)
		if err != nil {
			return err
		}
		r := forURL(o.Req, vs[0].URL) // best quality; the extension sends a chosen media playlist instead
		if text, err = fetchText(ctx, src, r); err != nil {
			return fmt.Errorf("variant playlist: %w", err)
		}
		h.Variant, h.Playlist, base = vs[0].URL, text, vs[0].URL
	}
	m, err := hls.ParseMedia(text, base)
	if err != nil {
		return err
	}
	if len(h.Done) != len(m.Items) { // new download, or the playlist changed
		h.Done, h.Bytes = make([]bool, len(m.Items)), 0
	}
	h.Segments = m.Segments

	ffPath, ffErr := ffmpeg.Find()
	if m.NeedsFFmpeg && ffErr != nil {
		return fmt.Errorf("this stream is encrypted or fragmented MP4 and needs ffmpeg: %w", ffErr)
	}
	name := o.Filename
	if name == "" {
		name = st.Filename
	}
	if name == "" {
		name = "video"
	}
	name = streamName(filepath.Base(name), ffErr == nil)
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return err
	}
	final := filepath.Join(o.Dir, name)
	parts := final + ".parts"
	if err := os.MkdirAll(parts, 0o755); err != nil {
		return err
	}
	st.Filename, st.Path, st.AcceptRanges, st.Size = name, parts, true, -1

	// Trust the disk over the saved flags: a piece marked done must exist.
	h.Bytes = 0
	for i, it := range m.Items {
		if !h.Done[i] {
			continue
		}
		if fi, err := os.Stat(filepath.Join(parts, it.Local)); err == nil {
			h.Bytes += fi.Size()
		} else {
			h.Done[i] = false
		}
	}

	lim := o.limiters()
	var (
		mu       sync.Mutex
		inflight atomic.Int64 // bytes of pieces still being fetched
	)
	snapshot := func() State {
		mu.Lock()
		defer mu.Unlock()
		c := st.Clone()
		c.HLS.Bytes += inflight.Load()
		done := 0
		for i, it := range m.Items {
			if h.Done[i] && it.Kind == hls.KindSegment {
				done++
			}
		}
		if done > 0 { // estimate: average piece so far times the number of pieces
			c.Size = h.Bytes / int64(done) * int64(m.Segments)
		}
		return c
	}

	parent := ctx // cancelled by pause / shutdown; ctx below also by a failed piece
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tickDone := make(chan struct{})
	go func() {
		defer close(tickDone)
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if onProgress != nil {
					onProgress(snapshot())
				}
			}
		}
	}()

	jobs := make(chan int)
	var (
		wg       sync.WaitGroup
		firstErr error
		errOnce  sync.Once
	)
	for w := 0; w < min(o.Connections, 16); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				it := m.Items[i]
				n, err := fetchPiece(ctx, src, forURL(o.Req, it.URL), it, filepath.Join(parts, it.Local), o.Retries, lim, &inflight)
				if err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("%s %d: %w", it.Kind, i, err) })
					cancel()
					continue
				}
				mu.Lock()
				h.Done[i], h.Bytes = true, h.Bytes+n
				mu.Unlock()
			}
		}()
	}
feed:
	for i := range m.Items {
		if h.Done[i] {
			continue
		}
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	cancel()
	<-tickDone
	if onProgress != nil {
		onProgress(snapshot())
	}
	if parent.Err() != nil {
		return parent.Err() // paused / shutting down: pieces stay for the resume
	}
	if firstErr != nil {
		return firstErr
	}

	// All pieces are here: join them.
	dest := uniquePath(final, "")
	if ffErr == nil {
		index := filepath.Join(parts, "index.m3u8")
		if err := os.WriteFile(index, []byte(m.Local), 0o644); err != nil {
			return err
		}
		ext := filepath.Ext(dest)
		tmp := strings.TrimSuffix(dest, ext) + ".joining" + ext
		// Not ctx: pause/shutdown must not leave a half-written file; joining
		// is a local copy and takes seconds.
		if err := ffmpeg.Remux(context.Background(), ffPath, []string{index}, tmp); err != nil {
			return err
		}
		if err := os.Rename(tmp, dest); err != nil {
			return err
		}
	} else if err := concatPieces(parts, m, dest); err != nil {
		return err
	}
	fi, err := os.Stat(dest)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(parts)
	st.Path, st.Size, h.Bytes = dest, fi.Size(), fi.Size()
	return nil
}

// forURL is req aimed at another URL of the same stream. Page cookies only go
// to the page's own host, never to CDNs the playlist points at.
func forURL(req proto.Request, u string) proto.Request {
	r := req
	r.URL, r.Media = u, nil
	if !sameHost(req.URL, u) {
		r.Cookies = ""
	}
	return r
}

func sameHost(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	return err1 == nil && err2 == nil && strings.EqualFold(ua.Hostname(), ub.Hostname())
}

func fetchText(ctx context.Context, src proto.Source, req proto.Request) (string, error) {
	rc, err := src.Open(ctx, req, 0, -1)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxPlaylist+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxPlaylist {
		return "", errors.New("playlist too large")
	}
	return string(b), nil
}

// fetchPiece downloads one item to path (via path+".tmp"), with retries.
// Bytes in flight are counted in inflight for the progress display.
func fetchPiece(ctx context.Context, src proto.Source, req proto.Request, it hls.Item, path string, retries int, lim limiters, inflight *atomic.Int64) (int64, error) {
	var last error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(backoff(attempt - 1)):
			}
		}
		n, err := fetchOnce(ctx, src, req, it, path, lim, inflight)
		if err == nil {
			return n, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		last = err
	}
	return 0, last
}

func fetchOnce(ctx context.Context, src proto.Source, req proto.Request, it hls.Item, path string, lim limiters, inflight *atomic.Int64) (n int64, err error) {
	rc, err := src.Open(ctx, req, it.Offset, it.Length)
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	defer func() {
		inflight.Add(-n) // counted again as done (or dropped) by the caller
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	buf := make([]byte, 64<<10)
	for {
		k, rerr := rc.Read(buf)
		if k > 0 {
			if err := lim.wait(ctx, k); err != nil {
				return n, err
			}
			if _, err := f.Write(buf[:k]); err != nil {
				return n, err
			}
			n += int64(k)
			inflight.Add(int64(k))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, rerr
		}
	}
	if it.Length >= 0 && n != it.Length {
		return n, fmt.Errorf("got %d bytes, expected %d", n, it.Length)
	}
	if n == 0 {
		return n, errors.New("empty response")
	}
	if err := f.Close(); err != nil {
		return n, err
	}
	return n, os.Rename(tmp, path)
}

// concatPieces joins plain MPEG-TS segments without ffmpeg: TS is designed to
// be cut and concatenated.
func concatPieces(parts string, m *hls.Media, dest string) error {
	out, err := os.Create(dest + ".joining")
	if err != nil {
		return err
	}
	for _, it := range m.Items {
		if it.Kind != hls.KindSegment {
			continue
		}
		in, err := os.Open(filepath.Join(parts, it.Local))
		if err != nil {
			out.Close()
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if err != nil {
			out.Close()
			return err
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(dest+".joining", dest)
}

var realExt = regexp.MustCompile(`^\.[A-Za-z0-9]{1,5}$`)

// streamName gives the output a container extension: MP4 when ffmpeg joins
// the pieces (keeping .mkv/.mov if asked for), MPEG-TS when they are concatenated.
func streamName(name string, withFFmpeg bool) string {
	ext := filepath.Ext(name)
	if !realExt.MatchString(ext) { // "Ep. 5 (720p)" has no extension
		ext = ""
	}
	base := strings.TrimSuffix(name, ext)
	if !withFFmpeg {
		return base + ".ts"
	}
	switch strings.ToLower(ext) {
	case ".mp4", ".mkv", ".mov", ".m4v":
		return name
	}
	return base + ".mp4"
}
