// Package engine implements segmented, resumable downloading on top of the
// protocol-agnostic proto.Source interface.
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/aoztrk82/open-download-manager/internal/proto"
)

// Segment is a byte range [Start, End) with Done bytes already written.
// End == -1 means "until EOF" (unknown size).
type Segment struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"`
}

// State is everything needed to resume a download. It is persisted by the
// caller (the queue) via the progress callback.
type State struct {
	Size         int64     `json:"size"` // -1 unknown, 0 not yet probed
	AcceptRanges bool      `json:"acceptRanges"`
	Filename     string    `json:"filename"`
	Path         string    `json:"path,omitempty"` // .part while running, final file when done
	Segments     []Segment `json:"segments"`
	// Conns is the live state of each segment's connection, index-aligned with
	// Segments. Only meaningful while Run is going; never persisted.
	Conns []Conn `json:"-"`
	// Limit is the current connection ceiling of this run (configured or
	// learned from the server). Live only, like Conns.
	Limit int `json:"-"`
	// HLS is the progress of a streaming download (see hls.go); Segments is
	// then unused.
	HLS *HLSState `json:"hls,omitempty"`
	// Tool is the progress of a download done by an external program (yt-dlp).
	Tool *ToolState `json:"tool,omitempty"`
}

// ToolState is what an external downloader reported.
type ToolState struct {
	Bytes int64 `json:"bytes"`
}

// Clone returns a copy that shares no slices with s.
func (s State) Clone() State {
	s.Segments = append([]Segment(nil), s.Segments...)
	s.Conns = append([]Conn(nil), s.Conns...)
	if s.HLS != nil {
		h := *s.HLS
		h.Done = append([]bool(nil), h.Done...)
		s.HLS = &h
	}
	if s.Tool != nil {
		t := *s.Tool
		s.Tool = &t
	}
	return s
}

// Connection states shown in the UI.
const (
	ConnWaiting    = "waiting"    // not started yet
	ConnConnecting = "connecting" // opening the request
	ConnActive     = "active"     // receiving data
	ConnRetrying   = "retrying"   // failed, waiting for the next attempt
	ConnDone       = "done"       // its byte range is complete
	ConnStopped    = "stopped"    // cancelled (pause, shutdown, sibling error)
	ConnFailed     = "failed"     // gave up after all retries
)

// Conn is the live view of one segment's connection.
type Conn struct {
	State   string `json:"state"`
	Retries int    `json:"retries"`
	Error   string `json:"error,omitempty"`
}

// Downloaded returns the total bytes written so far.
func (s *State) Downloaded() int64 {
	if s.HLS != nil {
		return s.HLS.Bytes
	}
	if s.Tool != nil {
		return s.Tool.Bytes
	}
	var n int64
	for _, sg := range s.Segments {
		n += sg.Done
	}
	return n
}

// Options configure a single run.
type Options struct {
	Req      proto.Request
	Dir      string // destination directory
	Filename string // overrides the probed name when non-empty
	// Connections is the most parallel connections one download may use
	// (default: CPU threads, at least 2, at most 32). The real number is found
	// while downloading: see pool.go.
	Connections        int
	InitialConnections int           // connections to start with (default 2)
	GrowEvery          time.Duration // how often to consider one more connection (default 2s)
	MinSplit           int64         // never leave a connection less than this to fetch (default 1 MiB)
	Retries            int           // per-segment retries (default 5)
	RateLimit          int64         // bytes/sec across all segments, 0 = unlimited
	// Shared limits all downloads together (Settings > total speed limit);
	// nil = none. Its rate may change while downloads run.
	Shared *rate.Limiter

	bufSize int // read buffer per connection; tests shrink it with MinSplit
}

// DefaultConnections is the per-download ceiling when none is configured.
func DefaultConnections() int { return min(max(runtime.NumCPU(), 2), 32) }

// Engine runs downloads.
type Engine struct {
	Sources proto.Registry

	limMu  sync.Mutex
	limits map[string]int // host -> connections it accepted (learned, in memory)
}

func New() *Engine { return &Engine{Sources: proto.Default()} }

// Run downloads to Dir/Filename, resuming from st if it has segments.
// onProgress is called ~2x/second and once at the end; st is mutated in place.
// The state passed to onProgress is a snapshot and safe to keep.
func (e *Engine) Run(ctx context.Context, st *State, o Options, onProgress func(State)) error {
	if o.Connections <= 0 {
		o.Connections = DefaultConnections()
	}
	if o.InitialConnections <= 0 {
		o.InitialConnections = 2
	}
	if o.GrowEvery <= 0 {
		o.GrowEvery = 2 * time.Second
	}
	if o.MinSplit <= 0 {
		o.MinSplit = 1 << 20
	}
	if o.bufSize <= 0 {
		o.bufSize = 64 << 10
	}
	// A split point is at least MinSplit past any write in flight, and one
	// write is at most one buffer: keep MinSplit above the buffer size.
	if o.MinSplit < int64(2*o.bufSize) {
		o.MinSplit = int64(2 * o.bufSize)
	}
	if o.Retries <= 0 {
		o.Retries = 5
	}
	src, err := e.Sources.For(o.Req.URL)
	if err != nil {
		return err
	}
	if o.Req.Media != nil {
		if o.Req.Media.Kind == proto.MediaYtdlp {
			return runYtdlp(ctx, st, o, onProgress)
		}
		return e.runHLS(ctx, st, o, src, onProgress)
	}

	if len(st.Segments) == 0 {
		info, err := src.Probe(ctx, o.Req)
		if err != nil {
			return fmt.Errorf("probe: %w", err)
		}
		st.Size, st.AcceptRanges, st.Filename = info.Size, info.AcceptRanges, info.Filename
		st.Segments = plan(info)
	}
	name := o.Filename
	if name == "" {
		name = st.Filename
	}
	if name == "" {
		name = "download"
	}
	name = filepath.Base(name)
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return err
	}
	final := filepath.Join(o.Dir, name)
	part := final + ".part"
	st.Path = part

	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if st.Size > 0 {
		if err := f.Truncate(st.Size); err != nil {
			return err
		}
	}
	if !st.AcceptRanges {
		// Cannot resume a non-seekable source: start over.
		for i := range st.Segments {
			st.Segments[i].Done = 0
		}
	}

	lim := o.limiters()

	var mu sync.Mutex
	st.Conns = make([]Conn, len(st.Segments))
	for i, sg := range st.Segments {
		st.Conns[i].State = ConnWaiting
		if sg.End >= 0 && sg.Start+sg.Done >= sg.End {
			st.Conns[i].State = ConnDone
		}
	}
	snapshot := func() State {
		mu.Lock()
		defer mu.Unlock()
		c := *st
		c.Segments = append([]Segment(nil), st.Segments...)
		c.Conns = append([]Conn(nil), st.Conns...)
		return c
	}

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

	p := &pool{e: e, ctx: ctx, cancel: cancel, src: src, o: o, f: f, st: st, mu: &mu, lim: lim}
	first := p.run()
	cancel()
	<-tickDone

	if onProgress != nil {
		onProgress(snapshot())
	}
	if first != nil {
		return first
	}

	if err := verify(st); err != nil {
		return err // never rename a file with holes
	}
	if err := f.Sync(); err != nil {
		return err
	}
	f.Close()
	// Unknown-size downloads: record the real size.
	if st.Size <= 0 {
		st.Size = st.Downloaded()
	}
	dest := uniquePath(final, part)
	if err := os.Rename(part, dest); err != nil {
		return err
	}
	st.Path = dest
	return nil
}

// plan starts with one segment covering the whole file; the pool splits it
// as connections are added.
func plan(info proto.Info) []Segment {
	if info.Size < 0 {
		return []Segment{{Start: 0, End: -1}}
	}
	return []Segment{{Start: 0, End: info.Size}}
}

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
}

// uniquePath returns final, or "name (n).ext" if final already exists.
func uniquePath(final, part string) string {
	if _, err := os.Stat(final); os.IsNotExist(err) {
		return final
	}
	ext := filepath.Ext(final)
	base := final[:len(final)-len(ext)]
	for n := 1; ; n++ {
		p := fmt.Sprintf("%s (%d)%s", base, n, ext)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
}
