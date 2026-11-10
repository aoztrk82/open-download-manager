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
	if err := m.Remove(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(d.ID); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
