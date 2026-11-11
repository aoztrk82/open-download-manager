package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/proto"
)

// testServer serves data with byte ranges and optional misbehaviour.
type testServer struct {
	data       []byte
	delay      time.Duration // per write (~32 KiB), plus up to the same again as jitter
	maxConns   int           // >0: ranged requests beyond this many concurrent get 503
	ignoreEven bool          // answer every 2nd ranged request with 200 + whole body
	wrongTotal bool          // every 3rd ranged request claims another file size
	noRanges   bool
	slowEvery  int // every Nth ranged request is 8x slower (forces work stealing)

	active, peak atomic.Int32
	n            atomic.Int64
}

type slowW struct {
	http.ResponseWriter
	d time.Duration
}

func (s slowW) Write(b []byte) (int, error) {
	if s.d > 0 {
		time.Sleep(s.d + time.Duration(rand.Int64N(int64(s.d))))
	}
	return s.ResponseWriter.Write(b)
}

func (ts *testServer) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranged := r.Header.Get("Range") != ""
		k := ts.n.Add(1)
		if ts.noRanges {
			r.Header.Del("Range")
			w.Header().Set("Content-Length", strconv.Itoa(len(ts.data)))
			slowW{w, ts.delay}.Write(ts.data)
			return
		}
		if ranged && r.Header.Get("Range") != "bytes=0-0" { // the probe is not counted
			cur := ts.active.Add(1)
			defer ts.active.Add(-1)
			for {
				p := ts.peak.Load()
				if cur <= p || ts.peak.CompareAndSwap(p, cur) {
					break
				}
			}
			if ts.maxConns > 0 && int(cur) > ts.maxConns {
				http.Error(w, "too many connections", http.StatusServiceUnavailable)
				return
			}
			if ts.ignoreEven && k%2 == 0 {
				r.Header.Del("Range")
			}
			if ts.wrongTotal && k%3 == 0 {
				w.Header().Set("Content-Range", "bytes 0-0/1")
				w.WriteHeader(http.StatusPartialContent)
				return
			}
		}
		d := ts.delay
		if ranged && ts.slowEvery > 0 && k%int64(ts.slowEvery) == 0 {
			d *= 8
		}
		http.ServeContent(slowW{w, d}, r, "f.bin", time.Time{}, bytes.NewReader(ts.data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fast options for tests: small buffers and splits, quick growth decisions.
func testOpts(srv *httptest.Server, dir string, conns, initial int) Options {
	return Options{
		Req: proto.Request{URL: srv.URL + "/f.bin"}, Dir: dir,
		Connections: conns, InitialConnections: initial, GrowEvery: 60 * time.Millisecond,
		MinSplit: 64 << 10, bufSize: 8 << 10, Retries: 3,
	}
}

func checkFile(t *testing.T, dir string, want []byte, st *State) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "f.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch (%d vs %d bytes)", len(got), len(want))
	}
	if err := verify(st); err != nil {
		t.Fatal(err)
	}
}

func TestGrowsConnections(t *testing.T) {
	ts := &testServer{data: testData(3 << 20), delay: 2 * time.Millisecond}
	srv := ts.start(t)
	dir := t.TempDir()
	var st State
	if err := New().Run(context.Background(), &st, testOpts(srv, dir, 6, 1), nil); err != nil {
		t.Fatal(err)
	}
	checkFile(t, dir, ts.data, &st)
	if ts.peak.Load() < 2 {
		t.Fatalf("never grew past one connection (peak %d)", ts.peak.Load())
	}
	if ts.peak.Load() > 6 {
		t.Fatalf("went past the ceiling: %d", ts.peak.Load())
	}
}

// Many connections, tiny splits, jittery server: lots of splitting while
// writes are in flight. The bytes must come out exactly right every time.
func TestWorkStealingStress(t *testing.T) {
	maxSegs := 0
	for run := range 6 {
		ts := &testServer{data: testData(2<<20 + rand.IntN(100_000)), delay: 300 * time.Microsecond, slowEvery: 3}
		srv := ts.start(t)
		dir := t.TempDir()
		var st State
		if err := New().Run(context.Background(), &st, testOpts(srv, dir, 8, 8), nil); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		checkFile(t, dir, ts.data, &st)
		maxSegs = max(maxSegs, len(st.Segments))
	}
	if maxSegs <= 8 {
		t.Fatalf("connections never took over work (max %d segments for 8 connections)", maxSegs)
	}
}

func TestServerConnectionLimit(t *testing.T) {
	ts := &testServer{data: testData(2 << 20), delay: time.Millisecond, maxConns: 2}
	srv := ts.start(t)
	dir := t.TempDir()
	e := New()
	var st State
	if err := e.Run(context.Background(), &st, testOpts(srv, dir, 6, 4), nil); err != nil {
		t.Fatal(err)
	}
	checkFile(t, dir, ts.data, &st)
	u, _ := url.Parse(srv.URL)
	if l := e.hostLimit(u.Host); l < 1 || l > 2 {
		t.Fatalf("host limit learned: %d, want 1..2", l)
	}
}

// A server that sometimes ignores Range (200 + whole file) or claims another
// size must never put wrong bytes in the file.
func TestMisbehavingServer(t *testing.T) {
	for _, ts := range []*testServer{
		{data: testData(1 << 20), delay: 200 * time.Microsecond, ignoreEven: true},
		{data: testData(1 << 20), delay: 200 * time.Microsecond, wrongTotal: true},
	} {
		srv := ts.start(t)
		dir := t.TempDir()
		var st State
		err := New().Run(context.Background(), &st, testOpts(srv, dir, 4, 4), nil)
		if err == nil {
			checkFile(t, dir, ts.data, &st)
		} else if _, statErr := os.Stat(filepath.Join(dir, "f.bin")); statErr == nil {
			t.Fatalf("failed (%v) but still produced the final file", err)
		}
	}
}

// Pause after some splits, persist the state as the queue does (JSON), resume.
func TestPauseResumeAfterSplits(t *testing.T) {
	ts := &testServer{data: testData(4 << 20), delay: 4 * time.Millisecond, slowEvery: 2}
	srv := ts.start(t)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	var st State
	var once sync.Once
	o := testOpts(srv, dir, 6, 3)
	err := New().Run(ctx, &st, o, func(s State) {
		if len(s.Segments) >= 4 && s.Downloaded() > 256<<10 {
			once.Do(cancel)
		}
	})
	if err == nil {
		t.Fatal("finished before it could be paused; slow the server down")
	}
	if len(st.Segments) < 4 {
		t.Fatalf("paused with %d segments, want splits first", len(st.Segments))
	}
	b, _ := json.Marshal(st)
	var resumed State
	if err := json.Unmarshal(b, &resumed); err != nil {
		t.Fatal(err)
	}
	if err := New().Run(context.Background(), &resumed, o, nil); err != nil {
		t.Fatal(err)
	}
	checkFile(t, dir, ts.data, &resumed)
}

// State written by 0.3.0 (four fixed segments, partly done) must resume.
func TestResumeOldFixedSegments(t *testing.T) {
	ts := &testServer{data: testData(4 << 20)}
	srv := ts.start(t)
	dir := t.TempDir()
	q := int64(1 << 20)
	st := State{Size: 4 << 20, AcceptRanges: true, Filename: "f.bin"}
	part, _ := os.Create(filepath.Join(dir, "f.bin.part"))
	for i := int64(0); i < 4; i++ {
		done := q / int64(i+2)
		st.Segments = append(st.Segments, Segment{Start: i * q, End: (i + 1) * q, Done: done})
		part.WriteAt(ts.data[i*q:i*q+done], i*q)
	}
	part.Close()
	if err := New().Run(context.Background(), &st, testOpts(srv, dir, 4, 2), nil); err != nil {
		t.Fatal(err)
	}
	checkFile(t, dir, ts.data, &st)
}

func TestSingleStreamFallbacks(t *testing.T) {
	ts := &testServer{data: testData(1 << 20), noRanges: true}
	srv := ts.start(t)
	dir := t.TempDir()
	var st State
	if err := New().Run(context.Background(), &st, testOpts(srv, dir, 8, 4), nil); err != nil {
		t.Fatal(err)
	}
	checkFile(t, dir, ts.data, &st)
	if len(st.Segments) != 1 {
		t.Fatalf("no-range server: %d segments, want 1", len(st.Segments))
	}
}

func TestVerify(t *testing.T) {
	ok := State{Size: 10, Segments: []Segment{{5, 10, 5}, {0, 5, 5}}}
	if err := verify(&ok); err != nil {
		t.Fatal(err)
	}
	for name, st := range map[string]State{
		"gap":        {Size: 10, Segments: []Segment{{0, 4, 4}, {5, 10, 5}}},
		"overlap":    {Size: 10, Segments: []Segment{{0, 6, 6}, {5, 10, 5}}},
		"incomplete": {Size: 10, Segments: []Segment{{0, 5, 4}, {5, 10, 5}}},
		"short":      {Size: 10, Segments: []Segment{{0, 5, 5}}},
	} {
		if verify(&st) == nil {
			t.Errorf("%s: verify passed", name)
		}
	}
}
