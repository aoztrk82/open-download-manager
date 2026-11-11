package api

import (
	"encoding/json"
	"net/http"
)

// LangPref is the program's UI language, shared by the window, the Web UI and
// the tray. Local daemon only: on a server each browser keeps its own choice.
type LangPref interface {
	// Lang is the saved choice, or the system language when none is saved.
	Lang() string
	// SetLang saves the choice and tells the tray.
	SetLang(lang string) error
}

func (s *Server) setPrefs(w http.ResponseWriter, r *http.Request) {
	if s.Lang == nil {
		writeErr(w, http.StatusNotImplemented, "program settings are only available on the local daemon")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	var body struct {
		Lang string `json:"lang"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if err := s.Lang.SetLang(body.Lang); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
