package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/aoztrk82/open-download-manager/internal/proto"
)

// Dynamic connections.
//
// A download starts with a couple of connections and adds one at a time while
// that still raises the total speed, up to Options.Connections. A connection
// that finishes its byte range takes over half of the largest range still
// running ("work stealing"), so all connections stay busy until the end. A
// server that refuses extra connections lowers the limit (remembered per host
// for the life of the process) instead of failing the download.
//
// Safety rule for splitting: a running segment only ever writes at
// Start+Done .. Start+Done+inflight, and a split point is chosen at or after
// Start+Done+inflight+MinSplit. Every write is clamped to the segment's
// current End under the lock, so two connections never write the same byte.

// errGaveUp: an extra connection could not open (the server refused it); its
// range was handed back to the others. Not a download failure.
var errGaveUp = errors.New("connection not accepted by the server")

// Total is implemented by a source's reader when the server reported the full
// size of the resource with the range (HTTP Content-Range).
type totaler interface{ Total() int64 }

type pool struct {
	e      *Engine
	ctx    context.Context
	cancel context.CancelFunc // stops every connection after a real failure
	src    proto.Source
	o      Options
	f      *os.File
	st     *State
	mu     *sync.Mutex
	lim    *rate.Limiter

	// guarded by mu, index-aligned with st.Segments
	owned    []bool
	inflight []int64

	active   atomic.Int32
	received atomic.Int64 // bytes written in this run
}

func (p *pool) remaining(i int) int64 {
	sg := p.st.Segments[i]
	if sg.End < 0 {
		return 1 << 62 // unknown size: "everything left"
	}
	return sg.End - (sg.Start + sg.Done)
}

func (p *pool) finished(i int) bool {
	sg := p.st.Segments[i]
	if sg.End < 0 {
		return p.st.Conns[i].State == ConnDone
	}
	return sg.Start+sg.Done >= sg.End
}

// claim gives a worker a segment: an unowned unfinished one if any (resume,
// or a range handed back), else the second half of the largest running one.
func (p *pool) claim() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	best := -1
	for i := range p.st.Segments {
		if !p.owned[i] && !p.finished(i) && (best < 0 || p.remaining(i) > p.remaining(best)) {
			best = i
		}
	}
	if best >= 0 {
		p.owned[best] = true
		return best, true
	}
	j := p.splittable()
	if j < 0 {
		return -1, false
	}
	sg := &p.st.Segments[j]
	cur := sg.Start + sg.Done + p.inflight[j]
	mid := cur + (sg.End-cur)/2
	p.st.Segments = append(p.st.Segments, Segment{Start: mid, End: sg.End})
	p.st.Segments[j].End = mid // index again: append may have moved the array
	p.st.Conns = append(p.st.Conns, Conn{State: ConnWaiting})
	p.owned = append(p.owned, true)
	p.inflight = append(p.inflight, 0)
	return len(p.st.Segments) - 1, true
}

// splittable returns the running segment with the most bytes left, if it is
// big enough to give two connections at least MinSplit each. Caller holds mu.
func (p *pool) splittable() int {
	if !p.st.AcceptRanges {
		return -1
	}
	best, bestLeft := -1, int64(0)
	for i, sg := range p.st.Segments {
		if !p.owned[i] || sg.End < 0 {
			continue
		}
		left := sg.End - (sg.Start + sg.Done + p.inflight[i])
		if left >= 2*p.o.MinSplit && left > bestLeft {
			best, bestLeft = i, left
		}
	}
	return best
}

// hasWork reports whether a new worker would find something to do.
func (p *pool) hasWork() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.st.Segments {
		if !p.owned[i] && !p.finished(i) {
			return true
		}
	}
	return p.splittable() >= 0
}

func (p *pool) setConn(i int, state string, err error) {
	p.mu.Lock()
	c := &p.st.Conns[i]
	c.State = state
	if err != nil {
		c.Error = err.Error()
	} else if state == ConnActive || state == ConnDone {
		c.Error = ""
	}
	p.mu.Unlock()
}

// worker runs segments until there is nothing left to take.
func (p *pool) worker() error {
	p.active.Add(1)
	defer p.active.Add(-1)
	buf := make([]byte, p.o.bufSize)
	for {
		i, ok := p.claim()
		if !ok {
			return nil
		}
		err := p.runSegment(i, buf)
		if err == nil {
			continue
		}
		p.mu.Lock()
		p.owned[i] = false
		p.mu.Unlock()
		return err
	}
}

func (p *pool) runSegment(i int, buf []byte) error {
	var lastErr error
	for attempt := 0; attempt <= p.o.Retries; attempt++ {
		if attempt > 0 {
			p.mu.Lock()
			p.st.Conns[i].Retries = attempt
			p.mu.Unlock()
			p.setConn(i, ConnRetrying, lastErr)
			select {
			case <-p.ctx.Done():
				p.setConn(i, ConnStopped, nil)
				return p.ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		p.mu.Lock()
		sg := p.st.Segments[i]
		done := p.finished(i)
		p.mu.Unlock()
		if done {
			p.setConn(i, ConnDone, nil)
			return nil
		}
		length := int64(-1)
		if sg.End >= 0 && p.st.AcceptRanges {
			length = sg.End - sg.Start - sg.Done
		}
		p.setConn(i, ConnConnecting, nil)
		rc, err := p.src.Open(p.ctx, p.o.Req, sg.Start+sg.Done, length)
		if err == nil {
			err = p.checkTotal(rc)
		}
		if err != nil {
			lastErr = err
			if p.ctx.Err() != nil {
				p.setConn(i, ConnStopped, nil)
				return p.ctx.Err()
			}
			// Another connection is still working: this one is probably over
			// the server's limit. Hand the range back instead of hammering.
			if p.active.Load() > 1 {
				p.setConn(i, ConnWaiting, err)
				return fmt.Errorf("%w: %v", errGaveUp, err)
			}
			continue
		}
		p.setConn(i, ConnActive, nil)
		err = p.copySegment(rc, i, buf)
		rc.Close()
		if err == nil {
			p.setConn(i, ConnDone, nil)
			return nil
		}
		lastErr = err
		if p.ctx.Err() != nil {
			p.setConn(i, ConnStopped, nil)
			return p.ctx.Err()
		}
	}
	p.setConn(i, ConnFailed, lastErr)
	return fmt.Errorf("segment %d: %w", i, lastErr)
}

// checkTotal rejects a response for a resource of another size (a stale
// mirror behind a redirector); its bytes must not be mixed into the file.
func (p *pool) checkTotal(rc io.ReadCloser) error {
	t, ok := rc.(totaler)
	if !ok || t.Total() <= 0 || p.st.Size <= 0 || t.Total() == p.st.Size {
		return nil
	}
	rc.Close()
	return fmt.Errorf("server reports size %d, expected %d", t.Total(), p.st.Size)
}

func (p *pool) copySegment(rc io.Reader, i int, buf []byte) error {
	for {
		n, rerr := rc.Read(buf)
		if n > 0 {
			if p.lim != nil {
				if err := p.lim.WaitN(p.ctx, n); err != nil {
					return err
				}
			}
			p.mu.Lock()
			sg := p.st.Segments[i]
			off := sg.Start + sg.Done
			if sg.End >= 0 {
				left := sg.End - off
				if left <= 0 { // another connection took the rest of this range
					p.mu.Unlock()
					return nil
				}
				if int64(n) > left {
					n = int(left)
				}
			}
			p.inflight[i] = int64(n)
			p.mu.Unlock()
			_, werr := p.f.WriteAt(buf[:n], off)
			p.mu.Lock()
			p.inflight[i] = 0
			if werr == nil {
				p.st.Segments[i].Done += int64(n)
			}
			sg = p.st.Segments[i]
			p.mu.Unlock()
			if werr != nil {
				return werr
			}
			p.received.Add(int64(n))
			if sg.End >= 0 && sg.Start+sg.Done >= sg.End {
				return nil // done, possibly early because the range was split
			}
		}
		if rerr == io.EOF {
			p.mu.Lock()
			sg := p.st.Segments[i]
			p.mu.Unlock()
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

// run drives the workers and grows their number while it helps.
func (p *pool) run() error {
	p.owned = make([]bool, len(p.st.Segments))
	p.inflight = make([]int64, len(p.st.Segments))

	limit := p.o.Connections
	host := hostOf(p.o.Req.URL)
	if c := p.e.hostLimit(host); c > 0 && c < limit {
		limit = c
	}
	start := p.o.InitialConnections
	if !p.st.AcceptRanges || p.st.Size <= 0 {
		limit, start = 1, 1 // one stream: no ranges, or nothing to split
	}
	start = min(start, limit)
	setLimit := func(n int) {
		p.mu.Lock()
		p.st.Limit = n
		p.mu.Unlock()
	}
	setLimit(limit)

	results := make(chan error)
	running := 0
	spawn := func() {
		running++
		go func() { results <- p.worker() }()
	}
	for range start {
		spawn()
	}

	tick := time.NewTicker(p.o.GrowEvery)
	defer tick.Stop()
	growing := limit > start
	lastBytes, lastRate := p.received.Load(), 0.0
	lastT := time.Now()
	var first error
	for running > 0 {
		select {
		case err := <-results:
			running--
			switch {
			case err == nil:
			case errors.Is(err, errGaveUp):
				// The server allows fewer connections than we tried.
				growing = false
				if running > 0 && running < limit {
					limit = running
					p.e.setHostLimit(host, limit)
					setLimit(limit)
				}
			default:
				if first == nil || (errors.Is(first, context.Canceled) && !errors.Is(err, context.Canceled)) {
					first = err
				}
				p.cancel()
			}
			// Everyone left but work remains (the last ones gave up at the
			// same time): keep one connection going, it retries normally.
			if running == 0 && first == nil && p.ctx.Err() == nil && p.hasWork() {
				spawn()
			}
		case now := <-tick.C:
			bytes := p.received.Load()
			rate := float64(bytes-lastBytes) / now.Sub(lastT).Seconds()
			if growing && first == nil && p.ctx.Err() == nil {
				// Add a connection while the last one raised the speed by >10%.
				if lastRate > 0 && rate < lastRate*1.10 {
					growing = false
				} else if running < limit && rate > 0 && p.hasWork() {
					spawn()
				}
			}
			lastBytes, lastRate, lastT = bytes, rate, now
		}
	}
	return first
}

// verify checks that the segments tile [0, Size) exactly and are complete
// before the .part file is renamed.
func verify(st *State) error {
	if st.Size <= 0 {
		return nil // unknown size: a single stream to EOF
	}
	segs := append([]Segment(nil), st.Segments...)
	sort.Slice(segs, func(a, b int) bool { return segs[a].Start < segs[b].Start })
	var at int64
	for _, sg := range segs {
		if sg.Start != at {
			return fmt.Errorf("segments do not line up at byte %d", at)
		}
		if sg.End < sg.Start || sg.Done != sg.End-sg.Start {
			return fmt.Errorf("segment at byte %d is incomplete", sg.Start)
		}
		at = sg.End
	}
	if at != st.Size {
		return fmt.Errorf("segments end at %d, size is %d", at, st.Size)
	}
	return nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func (e *Engine) hostLimit(host string) int {
	e.limMu.Lock()
	defer e.limMu.Unlock()
	return e.limits[host]
}

func (e *Engine) setHostLimit(host string, n int) {
	e.limMu.Lock()
	defer e.limMu.Unlock()
	if e.limits == nil {
		e.limits = map[string]int{}
	}
	if old, ok := e.limits[host]; !ok || n < old {
		e.limits[host] = n
	}
}
