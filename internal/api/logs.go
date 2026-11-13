package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/aoztrk82/open-download-manager/internal/applog"
	"github.com/aoztrk82/open-download-manager/internal/queue"
)

// The download log (Settings > General): what was asked, by whom, what the
// server answered and why it failed. Cookies and headers are never written,
// only how many there were.

// clientOf names who sent a request, for the log.
func clientOf(r *http.Request) string {
	o := r.Header.Get("Origin")
	switch {
	case strings.HasPrefix(o, "chrome-extension://"), strings.HasPrefix(o, "moz-extension://"), strings.HasPrefix(o, "safari-web-extension://"):
		return "extension"
	case o != "":
		return "webui"
	default:
		return "cli"
	}
}

func logAdd(r *http.Request, req queue.AddRequest, id, dir string) {
	if !applog.Enabled() {
		return
	}
	q := req.Request
	args := []any{"id", id, "from", clientOf(r), "url", q.URL}
	if q.Referer != "" && q.Referer != q.URL {
		args = append(args, "referer", q.Referer)
	}
	if q.Media != nil {
		args = append(args, "kind", q.Media.Kind)
		if q.Media.Format != "" {
			args = append(args, "format", q.Media.Format)
		}
		if q.Media.Playlist != "" {
			args = append(args, "playlistBytes", len(q.Media.Playlist))
		}
	} else {
		args = append(args, "kind", "file")
	}
	args = append(args, "name", req.Filename, "dir", dir, "queue", req.Queue, "paused", req.Paused,
		"cookies", strings.Count(q.Cookies, "=")) // how many, never the values
	applog.L().Info("add", args...)
}

// LogEvent is POST /v1/log: something an extension decided (capture, video
// menu), so a download that "did nothing" can be traced from the browser on.
type LogEvent struct {
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields"`
}

func (s *Server) postLog(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	var ev LogEvent
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&ev); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if applog.Enabled() && ev.Msg != "" {
		keys := make([]string, 0, len(ev.Fields))
		for k := range ev.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		args := []any{"from", clientOf(r)}
		for _, k := range keys {
			if len(args) >= 60 {
				break
			}
			v := fmt.Sprint(ev.Fields[k])
			if len(v) > 2000 {
				v = v[:2000] + "…"
			}
			args = append(args, k, v)
		}
		applog.L().Info(trim(ev.Msg, 80), args...)
	}
	w.WriteHeader(http.StatusNoContent)
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// getLog returns the last lines (Settings > General > Show log).
func (s *Server) getLog(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		writeErr(w, http.StatusNotImplemented, "the log is only available on the local daemon")
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 || n > 5000 {
		n = 500
	}
	lines, err := applog.Tail(n)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if lines == nil {
		lines = []string{}
	}
	writeJSON(w, 200, map[string]any{"enabled": applog.Enabled(), "path": applog.Path(), "lines": lines})
}

// revealLog shows the log file in Explorer.
func (s *Server) revealLog(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil || s.Reveal == nil || applog.Path() == "" {
		writeErr(w, http.StatusNotImplemented, "the log is only available on the local daemon")
		return
	}
	p := applog.Path()
	if err := s.Reveal(p); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
