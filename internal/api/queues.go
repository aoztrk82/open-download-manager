package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/aoztrk82/open-download-manager/internal/queue"
)

// Queues: the main queue runs downloads one after another; the later queue
// waits for its Start button or schedule (see internal/queue/queues.go).

func (s *Server) queues(w http.ResponseWriter, r *http.Request) {
	q, err := s.Queue.Queues()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, q)
}

func (s *Server) laterStart(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	s.Queue.StartLater(clientOf(r))
	s.queues(w, r)
}

func (s *Server) laterStop(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	s.Queue.StopLater()
	s.queues(w, r)
}

// setQueue: {"queue": "main" | "later" | ""}.
func (s *Server) setQueue(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	var body struct {
		Queue string `json:"queue"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if err := s.Queue.SetQueue(r.PathValue("id"), body.Queue); err != nil {
		if errors.Is(err, queue.ErrUnknownQueue) {
			writeErr(w, 400, err.Error())
			return
		}
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// moveInQueue: {"delta": -1 | 1}, one place up or down in its queue.
func (s *Server) moveInQueue(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	var body struct {
		Delta int `json:"delta"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || (body.Delta != -1 && body.Delta != 1) {
		writeErr(w, 400, `body must be {"delta": -1} or {"delta": 1}`)
		return
	}
	if err := s.Queue.Move(r.PathValue("id"), body.Delta); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
