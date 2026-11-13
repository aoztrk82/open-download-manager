package api

import (
	"encoding/json"
	"maps"
	"net/http"

	"github.com/aoztrk82/open-download-manager/internal/config"
	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

// SettingsStore keeps the Settings dialog's choices. On a server (token
// mode) folders stay inside its download folder; browser and file-type rules
// only matter on the local daemon the extensions talk to.
type SettingsStore interface {
	Settings() config.Settings
	// SetSettings saves normalized settings and applies them (queue, tray).
	SetSettings(config.Settings) error
}

// Autostart is "start ODM with Windows" (the tray menu has the same switch).
type Autostart interface {
	Enabled() bool
	Set(on bool) error
}

// SettingsView is GET/PUT /v1/settings: the stored settings plus a few
// values that live elsewhere or are only shown.
type SettingsView struct {
	config.Settings
	Autostart *bool `json:"autostart,omitempty"` // nil: not supported here
	// CPUConnections is what "automatic" connections means on this machine.
	CPUConnections int `json:"cpuConnections"`
	// CategoryDirs is where each category's files go now (its own folder or
	// the one inside the download folder); read-only.
	CategoryDirs map[string]string `json:"categoryDirs"`
	// CategoryFolders are the folder names used inside the download folder.
	CategoryFolders map[string]string `json:"categoryFolders"`
}

func (s *Server) settingsView() SettingsView {
	v := SettingsView{Settings: s.Settings.Settings(), CPUConnections: engine.DefaultConnections(), CategoryDirs: map[string]string{}, CategoryFolders: map[string]string{}}
	for _, c := range v.Categories {
		v.CategoryDirs[c.ID] = v.CategoryDir(c)
		v.CategoryFolders[c.ID] = config.CategoryFolder(c.ID)
	}
	if s.Autostart != nil {
		on := s.Autostart.Enabled()
		v.Autostart = &on
	}
	return v
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		writeErr(w, http.StatusNotImplemented, "settings are only available on the local daemon")
		return
	}
	writeJSON(w, http.StatusOK, s.settingsView())
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		writeErr(w, http.StatusNotImplemented, "settings are only available on the local daemon")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	// Decoding over the current values lets a client send only what it changes.
	v := SettingsView{Settings: s.Settings.Settings()}
	v.Capture = maps.Clone(v.Capture) // decoding merges into maps: never into the stored one
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&v); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	clean, err := v.Settings.Normalize()
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if !s.AnyDir {
		// A server: the token lets people download, not write anywhere. Its
		// download folder is its --dir; category folders must stay inside it.
		clean.DefaultDir = ""
		for _, c := range clean.Categories {
			if _, err := s.checkDir(c.Dir); err != nil {
				writeErr(w, 400, c.ID+": "+err.Error())
				return
			}
		}
	}
	if err := s.Settings.SetSettings(clean); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if v.Autostart != nil && s.Autostart != nil && *v.Autostart != s.Autostart.Enabled() {
		if err := s.Autostart.Set(*v.Autostart); err != nil {
			writeErr(w, 500, "start with Windows: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, s.settingsView())
}

// defaultDir is where downloads go when no folder is given.
func (s *Server) defaultDir() string {
	if s.Settings != nil && s.AnyDir { // a server always keeps its own folder
		return s.Settings.Settings().Dir()
	}
	if s.Settings != nil && s.DefaultDir == "" {
		return s.Settings.Settings().Dir()
	}
	return s.DefaultDir
}

// showWindow opens a download's progress window (double-click in the list).
func (s *Server) showWindow(w http.ResponseWriter, r *http.Request) {
	if s.ShowWindow == nil {
		writeErr(w, http.StatusNotImplemented, "progress windows are only available in the desktop app")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	d, err := s.Queue.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.ShowWindow(d.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// openFile opens a finished download with its default program (the
// "download complete" window's Open button). Local daemon only, like reveal.
func (s *Server) openFile(w http.ResponseWriter, r *http.Request) {
	if s.Open == nil {
		writeErr(w, http.StatusNotImplemented, "opening files is only available on the local daemon")
		return
	}
	d, err := s.Queue.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if d.Status != store.Completed || d.State.Path == "" {
		writeErr(w, http.StatusConflict, "the download is not finished")
		return
	}
	if err := s.Open(d.State.Path); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
