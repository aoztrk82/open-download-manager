package queue

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/proto"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

func waitStatus(t *testing.T, m *Manager, id string, want store.Status) *store.Download {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		d, err := m.Get(id)
		if err == nil && d.Status == want {
			return d
		}
		time.Sleep(50 * time.Millisecond)
	}
	d, _ := m.Get(id)
	t.Fatalf("download %s never reached %s (now %+v)", id, want, d)
	return nil
}

func TestQueueCompletesAndPersists(t *testing.T) {
	data := bytes.Repeat([]byte("odm"), 400_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	dir := t.TempDir()
	db := filepath.Join(dir, "odm.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{DefaultDir: dir}, st, engine.New())
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	d, err := m.Add(AddRequest{Request: proto.Request{URL: srv.URL + "/a.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, m, d.ID, store.Completed)
	if done.Size != int64(len(data)) {
		t.Fatalf("size %d", done.Size)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "a.bin"))
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
	m.Close()
	st.Close()

	// Reopen: completed entry must survive a restart.
	st2, _ := store.Open(db)
	defer st2.Close()
	all, _ := st2.List()
	if len(all) != 1 || all[0].Status != store.Completed {
		t.Fatalf("not persisted: %+v", all)
	}
}

func TestFailedAndRemove(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	st, _ := store.Open(":memory:")
	defer st.Close()
	m := New(Config{DefaultDir: t.TempDir()}, st, engine.New())
	_ = m.Start()
	defer m.Close()
	d, _ := m.Add(AddRequest{Request: proto.Request{URL: srv.URL + "/missing"}})
	f := waitStatus(t, m, d.ID, store.Failed)
	if f.Error == "" {
		t.Fatal("expected error message")
	}
	if err := m.Remove(d.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(d.ID); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRemoveDeletesFileOnRequest(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 300_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "d.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	dir := t.TempDir()
	st, _ := store.Open(":memory:")
	defer st.Close()
	m := New(Config{DefaultDir: dir}, st, engine.New())
	_ = m.Start()
	defer m.Close()

	keep, _ := m.Add(AddRequest{Request: proto.Request{URL: srv.URL + "/keep.bin"}})
	del, _ := m.Add(AddRequest{Request: proto.Request{URL: srv.URL + "/del.bin"}})
	k := waitStatus(t, m, keep.ID, store.Completed)
	d := waitStatus(t, m, del.ID, store.Completed)
	if d.Path != filepath.Join(dir, "del.bin") {
		t.Fatalf("path not recorded: %q", d.Path)
	}
	if err := m.Remove(keep.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(del.ID, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(d.Path); os.IsNotExist(err) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(d.Path); !os.IsNotExist(err) {
		t.Fatal("file should have been deleted")
	}
	if _, err := os.Stat(k.Path); err != nil {
		t.Fatalf("file must be kept when deleteFile is false: %v", err)
	}
}

func TestConnectionsView(t *testing.T) {
	data := bytes.Repeat([]byte("odm"), 1_000_000) // 3 MB: split into several segments
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "c.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "odm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(Config{DefaultDir: dir, Connections: 4}, st, engine.New())
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	d, err := m.Add(AddRequest{Request: proto.Request{URL: srv.URL + "/c.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, d.ID, store.Completed)

	c, err := m.Connections(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Live || len(c.Connections) < 2 || c.Size != int64(len(data)) {
		t.Fatalf("view: %+v", c)
	}
	var sum int64
	for _, cn := range c.Connections {
		if cn.State != engine.ConnDone {
			t.Fatalf("connection %d: %q", cn.Index, cn.State)
		}
		sum += cn.Done
	}
	if sum != int64(len(data)) {
		t.Fatalf("done bytes %d", sum)
	}
	if _, err := m.Connections("nope"); err == nil {
		t.Fatal("unknown id must fail")
	}
}

type slowWriter struct{ http.ResponseWriter }

func (s slowWriter) Write(b []byte) (int, error) {
	time.Sleep(40 * time.Millisecond)
	return s.ResponseWriter.Write(b)
}

// Polling the connections view while segments are being written must neither
// race with the engine nor disturb the bytes that end up on disk.
func TestConnectionsWhileRunning(t *testing.T) {
	data := testBytes(4 << 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(slowWriter{w}, r, "r.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "odm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(Config{DefaultDir: dir, Connections: 4}, st, engine.New())
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	d, err := m.Add(AddRequest{Request: proto.Request{URL: srv.URL + "/r.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	sawLive := make(chan bool, 1)
	go func() {
		live := false
		for {
			select {
			case <-stop:
				sawLive <- live
				return
			default:
			}
			if c, err := m.Connections(d.ID); err == nil && c.Live {
				live = true
				for _, cn := range c.Connections {
					_ = cn.Done + int64(len(cn.State))
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	done := waitStatus(t, m, d.ID, store.Completed)
	close(stop)
	if !<-sawLive {
		t.Fatal("never saw the download live")
	}
	if filepath.Base(done.Path) != "r.bin" {
		t.Fatalf("final path %q (still the .part?)", done.Path)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "r.bin"))
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func testBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}
