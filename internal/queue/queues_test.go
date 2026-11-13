package queue

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/proto"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

// slowServer serves small files slowly and records the order and overlap of
// requests per file (probe + body each count once per file).
type slowServer struct {
	*httptest.Server
	mu      sync.Mutex
	order   []string
	running atomic.Int32
	maxRun  atomic.Int32
	started map[string]time.Time
	ended   map[string]time.Time
}

func newSlowServer(t *testing.T, delay time.Duration) *slowServer {
	s := &slowServer{started: map[string]time.Time{}, ended: map[string]time.Time{}}
	data := bytes.Repeat([]byte("q"), 64<<10)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		s.mu.Lock()
		if _, ok := s.started[name]; !ok {
			s.started[name] = time.Now()
			s.order = append(s.order, name)
		}
		s.mu.Unlock()
		n := s.running.Add(1)
		for {
			m := s.maxRun.Load()
			if n <= m || s.maxRun.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(delay)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
		s.running.Add(-1)
		s.mu.Lock()
		s.ended[name] = time.Now()
		s.mu.Unlock()
	}))
	t.Cleanup(s.Close)
	return s
}

func newManager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultDir == "" {
		cfg.DefaultDir = t.TempDir()
	}
	m := New(cfg, st, engine.New())
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(); st.Close() })
	return m
}

func addTo(t *testing.T, m *Manager, url, q string) *store.Download {
	t.Helper()
	d, err := m.Add(AddRequest{Request: proto.Request{URL: url}, Queue: q})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMainQueueRunsOneAfterAnother(t *testing.T) {
	srv := newSlowServer(t, 150*time.Millisecond)
	m := newManager(t, Config{MaxConcurrent: 5})
	var ids []string
	for i := 1; i <= 3; i++ {
		ids = append(ids, addTo(t, m, fmt.Sprintf("%s/m%d.bin", srv.URL, i), store.QueueMain).ID)
	}
	// The last one goes first.
	if err := m.Move(ids[2], -1); err != nil {
		t.Fatal(err)
	}
	if err := m.Move(ids[2], -1); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		waitStatus(t, m, id, store.Completed)
	}
	// Each file: probe + body requests, never two files at once.
	if got := srv.maxRun.Load(); got > 2 {
		t.Fatalf("%d requests at once: the queue ran downloads in parallel", got)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if fmt.Sprint(srv.order) != "[/m1.bin /m3.bin /m2.bin]" && fmt.Sprint(srv.order) != "[/m3.bin /m1.bin /m2.bin]" {
		t.Logf("order %v", srv.order) // m1 may have started before the moves
	}
	if srv.order[len(srv.order)-1] != "/m2.bin" {
		t.Fatalf("order %v: m2 should be last after the moves", srv.order)
	}
	for i := 1; i < len(srv.order); i++ {
		if srv.started[srv.order[i]].Before(srv.ended[srv.order[i-1]]) {
			t.Fatalf("%s started before %s ended", srv.order[i], srv.order[i-1])
		}
	}
}

func TestLaterQueueWaitsStartsAndPauses(t *testing.T) {
	srv := newSlowServer(t, 50*time.Millisecond)
	m := newManager(t, Config{LaterDelay: 700 * time.Millisecond})
	a := addTo(t, m, srv.URL+"/l1.bin", store.QueueLater)
	b := addTo(t, m, srv.URL+"/l2.bin", store.QueueLater)
	now := addTo(t, m, srv.URL+"/now.bin", "")
	waitStatus(t, m, now.ID, store.Completed) // no queue: starts at once
	time.Sleep(300 * time.Millisecond)
	for _, id := range []string{a.ID, b.ID} {
		if d, _ := m.Get(id); d.Status != store.Queued {
			t.Fatalf("later download started on its own: %s", d.Status)
		}
	}
	if q, _ := m.Queues(); q.Later.Running || q.Later.Waiting != 2 {
		t.Fatalf("queues before start: %+v", q)
	}

	m.StartLater("test")
	waitStatus(t, m, a.ID, store.Completed)
	waitStatus(t, m, b.ID, store.Completed)
	srv.mu.Lock()
	gap := srv.started["/l2.bin"].Sub(srv.ended["/l1.bin"])
	srv.mu.Unlock()
	if gap < 600*time.Millisecond {
		t.Fatalf("second later download began %v after the first ended; want the 700ms wait", gap)
	}
	deadline := time.Now().Add(5 * time.Second)
	for m.LaterRunning() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if m.LaterRunning() {
		t.Fatal("the later queue did not stop after its last download")
	}
}

func TestLaterQueueStopAndSchedule(t *testing.T) {
	srv := newSlowServer(t, 3*time.Second) // long enough to be stopped mid-way
	var due atomic.Bool
	m := newManager(t, Config{LaterDue: func(prev, now time.Time) bool { return due.Swap(false) }})
	a := addTo(t, m, srv.URL+"/s1.bin", store.QueueLater)

	due.Store(true) // the schedule's time comes
	waitStatus(t, m, a.ID, store.Active)
	m.StopLater()
	d := waitStatus(t, m, a.ID, store.Queued) // back to waiting, not failed
	if d.Error != "" || d.Queue != store.QueueLater {
		t.Fatalf("after stop: %+v", d)
	}
	time.Sleep(300 * time.Millisecond)
	if d, _ := m.Get(a.ID); d.Status != store.Queued {
		t.Fatalf("restarted without the queue: %s", d.Status)
	}

	// Resuming one download of a stopped later queue starts just that one.
	if err := m.Resume(a.ID); err != nil {
		t.Fatal(err)
	}
	d = waitStatus(t, m, a.ID, store.Active)
	if d.Queue != "" {
		t.Fatalf("resume kept it in the later queue: %q", d.Queue)
	}

	// Moving a running download into a queue sticks although it keeps saving.
	if err := m.SetQueue(a.ID, store.QueueMain); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	if d, _ := m.Get(a.ID); d.Queue != store.QueueMain {
		t.Fatalf("queue change undone by the running download: %q", d.Queue)
	}
	if err := m.SetQueue(a.ID, "nope"); err != ErrUnknownQueue {
		t.Fatalf("unknown queue: %v", err)
	}
}
