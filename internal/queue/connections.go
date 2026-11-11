package queue

import "github.com/aoztrk82/open-download-manager/internal/engine"

// Connection is one parallel segment of a download, as the UI shows it.
type Connection struct {
	Index   int    `json:"index"`
	Start   int64  `json:"start"`
	End     int64  `json:"end"` // -1: until EOF (size unknown)
	Done    int64  `json:"done"`
	State   string `json:"state"` // engine.Conn* constants
	Retries int    `json:"retries"`
	Error   string `json:"error,omitempty"`
}

// Connections is the per-segment view of a download.
type Connections struct {
	Size         int64        `json:"size"`
	AcceptRanges bool         `json:"acceptRanges"`
	Live         bool         `json:"live"` // true while the engine runs it
	Connections  []Connection `json:"connections"`
}

// Connections returns the segments of a download: live connection states while
// it runs, otherwise what the persisted resume state says.
func (m *Manager) Connections(id string) (*Connections, error) {
	m.mu.Lock()
	st, live := m.live[id]
	m.mu.Unlock()
	if !live {
		d, err := m.store.Get(id)
		if err != nil {
			return nil, err
		}
		st = d.State
	}
	out := &Connections{Size: st.Size, AcceptRanges: st.AcceptRanges, Live: live, Connections: []Connection{}}
	for i, sg := range st.Segments {
		c := Connection{Index: i, Start: sg.Start, End: sg.End, Done: sg.Done}
		switch {
		case live && i < len(st.Conns):
			c.State, c.Retries, c.Error = st.Conns[i].State, st.Conns[i].Retries, st.Conns[i].Error
		case sg.End >= 0 && sg.Start+sg.Done >= sg.End:
			c.State = engine.ConnDone
		default:
			c.State = engine.ConnStopped
		}
		out.Connections = append(out.Connections, c)
	}
	return out, nil
}

// setLive records the newest engine snapshot of a running download; nil clears it.
func (m *Manager) setLive(id string, st *engine.State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st == nil {
		delete(m.live, id)
		return
	}
	m.live[id] = *st
}
