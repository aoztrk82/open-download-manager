package engine

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/aoztrk82/open-download-manager/internal/proto"
)

// A player page taken for the video (mail.ru: <video src=""> reports the page
// address): with a video name ODM must refuse the HTML instead of saving it.
func TestRefusesPageForVideo(t *testing.T) {
	page := []byte("<!doctype html><title>player</title>" + strings.Repeat("x", 4096))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "", timeZero, bytes.NewReader(page))
	}))
	defer srv.Close()

	var log bytes.Buffer
	dir := t.TempDir()
	o := Options{Req: proto.Request{URL: srv.URL + "/video/449.html"}, Dir: dir, Filename: "Film (720p).mp4",
		Log: slog.New(slog.NewTextHandler(&log, nil))}
	var st State
	err := New().Run(context.Background(), &st, o, nil)
	if err == nil || !strings.Contains(err.Error(), "web page") {
		t.Fatalf("err = %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("left files: %v", ents)
	}
	for _, want := range []string{"msg=probe", "type=\"text/html; charset=utf-8\"", "msg=refused"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %s:\n%s", want, log.String())
		}
	}

	// The same page asked for as a page (or with no name) is just a download.
	o.Filename = "449.html"
	st = State{}
	if err := New().Run(context.Background(), &st, o, nil); err != nil {
		t.Fatalf("html as html: %v", err)
	}
}
