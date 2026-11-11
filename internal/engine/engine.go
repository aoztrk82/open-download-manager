// Package engine implements segmented, resumable downloading on top of the
// protocol-agnostic proto.Source interface.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	var n int64
	for _, sg := range s.Segments {
		n += sg.Done
	}
	return n
}

// Options configure a single run.
type Options struct {
	Req         proto.Request
	Dir         string // destination directory
	Filename    string // overrides the probed name when non-empty
	Connections int    // parallel segments (default 4)
	MinSplit    int64  // don't split below this size (default 1 MiB)
	Retries     int    // per-segment retries (default 5)
	RateLimit   int64  // bytes/sec across all segments, 0 = unlimited
}

// Engine runs downloads.
type Engine struct {
	Sources proto.Registry
}

func New() *Engine { return &Engine{Sources: proto.Default()} }

// Run downloads to Dir/Filename, resuming from st if it has segments.
// onProgress is called ~2x/second and once at the end; st is mutated in place.
// The state passed to onProgress is a snapshot and safe to keep.
func (e *Engine) Run(ctx context.Context, st *State, o Options, onProgress func(State)) error {
	if o.Connections <= 0 {
		o.Connections = 4
	}
	if o.MinSplit <= 0 {
		o.MinSplit = 1 << 20
	}
	if o.Retries <= 0 {
		o.Retries = 5
	}
	src, err := e.Sources.For(o.Req.URL)
	if err != nil {
		return err
	}

	if len(st.Segments) == 0 {
		info, err := src.Probe(ctx, o.Req)
		if err != nil {
			return fmt.Errorf("probe: %w", err)
		}
		st.Size, st.AcceptRanges, st.Filename = info.Size, info.AcceptRanges, info.Filename
		st.Segments = plan(info, o)
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

	var lim *rate.Limiter
	if o.RateLimit > 0 {
		burst := int(o.RateLimit)
		if burst < 32*1024 {
			burst = 32 * 1024
		}
		lim = rate.NewLimiter(rate.Limit(o.RateLimit), burst)
	}

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

	errc := make(chan error, len(st.Segments))
	var wg sync.WaitGroup
	for i := range st.Segments {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := e.runSegment(ctx, src, o, f, st, i, &mu, lim); err != nil {
				errc <- err
				cancel() // stop siblings
			}
		}(i)
	}
	wg.Wait()
	cancel()
	<-tickDone
	close(errc)

	// Prefer the first real error over the cancellations it triggered.
	var first error
	for err := range errc {
		if first == nil || (errors.Is(first, context.Canceled) && !errors.Is(err, context.Canceled)) {
			first = err
		}
	}
	if onProgress != nil {
		onProgress(snapshot())
	}
	if first != nil {
		return first
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

func plan(info proto.Info, o Options) []Segment {
	if info.Size < 0 {
		return []Segment{{Start: 0, End: -1}}
	}
	if info.Size == 0 {
		return []Segment{{Start: 0, End: 0}}
	}
	n := int64(1)
	if info.AcceptRanges && info.Size >= o.MinSplit {
		n = int64(o.Connections)
		if max := info.Size / (256 << 10); n > max && max > 0 {
			n = max
		}
	}
	size := info.Size / n
	segs := make([]Segment, 0, n)
	for i := int64(0); i < n; i++ {
		s := Segment{Start: i * size, End: (i + 1) * size}
		if i == n-1 {
			s.End = info.Size
		}
		segs = append(segs, s)
	}
	return segs
}

func (e *Engine) runSegment(ctx context.Context, src proto.Source, o Options, f *os.File,
	st *State, i int, mu *sync.Mutex, lim *rate.Limiter) error {

	setConn := func(state string, err error) {
		mu.Lock()
		c := &st.Conns[i]
		c.State = state
		if err != nil {
			c.Error = err.Error()
		} else if state == ConnActive || state == ConnDone {
			c.Error = ""
		}
		mu.Unlock()
	}
	buf := make([]byte, 64<<10)
	var lastErr error
	for attempt := 0; attempt <= o.Retries; attempt++ {
		if attempt > 0 {
			mu.Lock()
			st.Conns[i].Retries = attempt
			mu.Unlock()
			setConn(ConnRetrying, lastErr)
			select {
			case <-ctx.Done():
				setConn(ConnStopped, nil)
				return ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		mu.Lock()
		sg := st.Segments[i]
		mu.Unlock()
		if sg.End >= 0 && sg.Start+sg.Done >= sg.End {
			setConn(ConnDone, nil)
			return nil
		}
		length := int64(-1)
		if sg.End >= 0 && st.AcceptRanges {
			length = sg.End - sg.Start - sg.Done
		}
		setConn(ConnConnecting, nil)
		rc, err := src.Open(ctx, o.Req, sg.Start+sg.Done, length)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				setConn(ConnStopped, nil)
				return ctx.Err()
			}
			continue
		}
		setConn(ConnActive, nil)
		err = copySegment(ctx, rc, f, st, i, mu, lim, buf)
		rc.Close()
		if err == nil {
			setConn(ConnDone, nil)
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			setConn(ConnStopped, nil)
			return ctx.Err()
		}
	}
	setConn(ConnFailed, lastErr)
	return fmt.Errorf("segment %d: %w", i, lastErr)
}

func copySegment(ctx context.Context, rc io.Reader, f *os.File, st *State, i int,
	mu *sync.Mutex, lim *rate.Limiter, buf []byte) error {
	for {
		n, rerr := rc.Read(buf)
		if n > 0 {
			if lim != nil {
				if err := lim.WaitN(ctx, n); err != nil {
					return err
				}
			}
			mu.Lock()
			off := st.Segments[i].Start + st.Segments[i].Done
			mu.Unlock()
			if _, err := f.WriteAt(buf[:n], off); err != nil {
				return err
			}
			mu.Lock()
			st.Segments[i].Done += int64(n)
			mu.Unlock()
		}
		if rerr == io.EOF {
			mu.Lock()
			sg := st.Segments[i]
			mu.Unlock()
			if sg.End >= 0 && sg.Start+sg.Done < sg.End {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
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
