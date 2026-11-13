package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/ytdlp"
)

// GET /v1/media: is yt-dlp there, which version.
func (s *Server) mediaInfo(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"ytdlp": false}
	if p, err := ytdlp.Find(); err == nil {
		out["ytdlp"] = true
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if v, err := ytdlp.Version(ctx, p); err == nil {
			out["version"] = v
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /v1/media/probe {"url": "..."}: what yt-dlp finds there (qualities, or
// playlist entries) for the yt-dlp dialog.
func (s *Server) mediaProbe(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if err := checkPageURL(body.URL); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	path, err := ytdlp.Find()
	if err != nil {
		writeErr(w, http.StatusNotImplemented, err.Error())
		return
	}
	// A long playlist takes a while: one request per page of 100 entries.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	info, err := ytdlp.Probe(ctx, path, body.URL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// POST /v1/media/update: `yt-dlp -U`. Sites change; yt-dlp follows in days.
func (s *Server) mediaUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	path, err := ytdlp.Find()
	if err != nil {
		writeErr(w, http.StatusNotImplemented, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	out, err := ytdlp.Update(ctx, path)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	v, _ := ytdlp.Version(ctx, path)
	writeJSON(w, http.StatusOK, map[string]string{"output": out, "version": v})
}

// checkPageURL: yt-dlp gets web pages only (no file://, no options).
func checkPageURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("url must be an http(s) address")
	}
	return nil
}
