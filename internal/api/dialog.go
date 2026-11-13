package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/proto"
)

// Endpoints behind the "where should this download go?" dialog.

// requireJSON refuses bodies a hostile page could send cross-origin without a
// preflight (form posts, text/plain). Used on every POST that does something.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	return true
}

// checkDir validates a client-chosen download folder. It must be absolute. A
// remote server (AnyDir false) only accepts folders inside its own download
// directory: the token lets people download, not write anywhere on the server.
func (s *Server) checkDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	clean := filepath.Clean(dir)
	if !filepath.IsAbs(clean) {
		return "", errors.New("folder must be an absolute path")
	}
	if s.AnyDir {
		return clean, nil
	}
	if s.DefaultDir == "" {
		return "", errors.New("this server does not accept a folder")
	}
	rel, err := filepath.Rel(filepath.Clean(s.DefaultDir), clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("folder must be inside the server's download folder " + s.DefaultDir)
	}
	return clean, nil
}

// ProbeResult is what the dialog shows before the download starts.
type ProbeResult struct {
	Size         int64  `json:"size"` // -1 unknown
	AcceptRanges bool   `json:"acceptRanges"`
	Filename     string `json:"filename"`
}

func (s *Server) probe(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	var body struct {
		Request proto.Request `json:"request"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&body); err != nil { // may carry a playlist
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if body.Request.URL == "" {
		writeErr(w, 400, "request.url is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	info, err := s.Queue.Probe(ctx, body.Request)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, 200, ProbeResult{Size: info.Size, AcceptRanges: info.AcceptRanges, Filename: info.Filename})
}

func (s *Server) pickFolder(w http.ResponseWriter, r *http.Request) {
	if s.PickFolder == nil {
		writeErr(w, http.StatusNotImplemented, "choosing a folder is only available on the local daemon")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	var body struct {
		Initial string `json:"initial"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body)
	if body.Initial == "" {
		body.Initial = s.DefaultDir
	}
	path, err := s.PickFolder(body.Initial)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"path": path}) // "" = cancelled
}
