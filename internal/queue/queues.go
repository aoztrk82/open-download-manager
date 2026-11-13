package queue

import (
	"errors"
	"sort"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/store"
)

// Queues (owner's design): "Download" starts at once; "Add to queue" puts a
// download in the main queue, which runs one download after another;
// "Later" puts it in the later queue, which waits until someone presses Start
// or its schedule comes, then also runs one after another with a pause
// between two downloads.

// QueueState is GET /v1/queues.
type QueueState struct {
	Main  QueueInfo `json:"main"`
	Later QueueInfo `json:"later"`
}

type QueueInfo struct {
	Running bool `json:"running"` // main: always; later: started and not finished
	Waiting int  `json:"waiting"` // queued, not started yet
	Active  int  `json:"active"`
	// NextAt: the later queue waits between downloads until then.
	NextAt *time.Time `json:"nextAt,omitempty"`
}

// ErrUnknownQueue is returned for a queue name other than "", main or later.
var ErrUnknownQueue = errors.New("unknown queue")

func KnownQueue(q string) bool { return q == "" || q == store.QueueMain || q == store.QueueLater }

// LaterRunning reports whether the later queue is running.
func (m *Manager) LaterRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.laterRunning
}

// StartLater starts the later queue now (Start button or its schedule).
func (m *Manager) StartLater(why string) {
	m.mu.Lock()
	was := m.laterRunning
	m.laterRunning, m.laterNext = true, time.Time{}
	m.mu.Unlock()
	if !was {
		m.log().Info("later queue started", "by", why)
	}
	m.kick()
}

// StopLater stops the later queue. Its running download goes back to waiting
// at the head of the queue (it resumes from where it was when started again).
func (m *Manager) StopLater() {
	m.mu.Lock()
	m.laterRunning = false
	var stop []string
	for id, q := range m.activeQ {
		if q == store.QueueLater {
			stop = append(stop, id)
		}
	}
	m.mu.Unlock()
	m.log().Info("later queue stopped", "running", len(stop))
	for _, id := range stop {
		_ = m.setStatus(id, store.Queued) // before cancel: run() reads it to tell "stopped" from "failed"
		m.mu.Lock()
		if cancel, ok := m.active[id]; ok {
			cancel()
		}
		m.mu.Unlock()
	}
	m.kick()
}

// Queues reports both queues for the window.
func (m *Manager) Queues() (QueueState, error) {
	all, err := m.store.List()
	if err != nil {
		return QueueState{}, err
	}
	var s QueueState
	for _, d := range all {
		var qi *QueueInfo
		switch d.Queue {
		case store.QueueMain:
			qi = &s.Main
		case store.QueueLater:
			qi = &s.Later
		default:
			continue
		}
		switch d.Status {
		case store.Queued:
			qi.Waiting++
		case store.Active:
			qi.Active++
		}
	}
	m.mu.Lock()
	s.Main.Running = true
	s.Later.Running = m.laterRunning
	if m.laterRunning && time.Now().Before(m.laterNext) {
		t := m.laterNext
		s.Later.NextAt = &t
	}
	m.mu.Unlock()
	return s, nil
}

// nextPos is the position after the last download of a queue. Caller holds posMu.
func (m *Manager) nextPos(q string) int64 {
	all, _ := m.store.List()
	var top int64
	for _, d := range all {
		if d.Queue == q && d.QPos > top {
			top = d.QPos
		}
	}
	return top + 1
}

// SetQueue moves a download into a queue (at its end) or out of all queues
// (""). A running download keeps running. A paused or failed one moved into a
// queue waits there; one taken out of a queue keeps its status.
func (m *Manager) SetQueue(id, q string) error {
	if !KnownQueue(q) {
		return ErrUnknownQueue
	}
	m.posMu.Lock()
	defer m.posMu.Unlock()
	d, err := m.store.Get(id)
	if err != nil {
		return err
	}
	if d.Queue == q {
		return nil
	}
	var pos int64
	if q != "" {
		pos = m.nextPos(q)
	}
	if err := m.store.SetQueue(id, q, pos); err != nil {
		return err
	}
	d.Queue, d.QPos = q, pos
	m.mu.Lock()
	_, running := m.active[id]
	if running {
		m.activeQ[id] = q
	}
	m.mu.Unlock()
	if q != "" && !running && (d.Status == store.Paused || d.Status == store.Failed) {
		d.Status, d.Error = store.Queued, ""
		if err := m.store.Save(d); err != nil {
			return err
		}
	}
	m.log().Info("queue changed", "id", id, "queue", q)
	c := *d
	m.publish(Event{Type: "update", Download: &c})
	m.kick()
	return nil
}

// Move shifts a download one place up (delta -1) or down (+1) in its queue.
func (m *Manager) Move(id string, delta int) error {
	m.posMu.Lock()
	defer m.posMu.Unlock()
	d, err := m.store.Get(id)
	if err != nil {
		return err
	}
	if d.Queue == "" {
		return errors.New("the download is not in a queue")
	}
	all, err := m.store.List()
	if err != nil {
		return err
	}
	var same []*store.Download
	for _, x := range all {
		if x.Queue == d.Queue && x.Status != store.Completed {
			same = append(same, x)
		}
	}
	sort.SliceStable(same, func(i, j int) bool { return same[i].QPos < same[j].QPos })
	i := -1
	for k, x := range same {
		if x.ID == id {
			i = k
		}
	}
	j := i + delta
	if i < 0 || j < 0 || j >= len(same) {
		return nil // already first / last
	}
	// Renumber the whole queue so equal or missing positions cannot stick.
	same[i], same[j] = same[j], same[i]
	for k, x := range same {
		if x.QPos != int64(k+1) {
			x.QPos = int64(k + 1)
			if err := m.store.SetQueue(x.ID, x.Queue, x.QPos); err != nil {
				return err
			}
			c := *x
			m.publish(Event{Type: "update", Download: &c})
		}
	}
	m.kick()
	return nil
}
