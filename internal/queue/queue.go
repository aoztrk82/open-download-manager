// Package queue schedules downloads, persists their state and publishes events.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/proto"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

// Event is published whenever a download changes.
type Event struct {
	Type     string          `json:"type"` // "update" | "removed"
	Download *store.Download `json:"download,omitempty"`
	ID       string          `json:"id,omitempty"`
}

// AddRequest is what callers (API, CLI, extensions) submit.
type AddRequest struct {
	Request  proto.Request `json:"request"`
	Dir      string        `json:"dir,omitempty"`
	Filename string        `json:"filename,omitempty"`
}

type Config struct {
	DefaultDir    string
	MaxConcurrent int   // simultaneous downloads (default 3)
	Connections   int   // segments per download (default 4)
	RateLimit     int64 // bytes/sec per download, 0 = unlimited
}

type Manager struct {
	cfg    Config
	store  *store.Store
	engine *engine.Engine

	mu      sync.Mutex
	active  map[string]context.CancelFunc
	wake    chan struct{}
	subs    map[chan Event]struct{}
	ctx     context.Context
	stop    context.CancelFunc
	stopped sync.WaitGroup
}

func New(cfg Config, st *store.Store, eng *engine.Engine) *Manager {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Manager{
		cfg: cfg, store: st, engine: eng,
		active: map[string]context.CancelFunc{},
		wake:   make(chan struct{}, 1),
		subs:   map[chan Event]struct{}{},
		ctx:    ctx, stop: stop,
	}
}

// Start resumes interrupted downloads and begins scheduling.
func (m *Manager) Start() error {
	all, err := m.store.List()
	if err != nil {
		return err
	}
	for _, d := range all {
		if d.Status == store.Active { // process died mid-download
			d.Status = store.Queued
			if err := m.store.Save(d); err != nil {
				return err
			}
		}
	}
	m.stopped.Add(1)
	go m.loop()
	m.kick()
	return nil
}

// Close cancels running downloads (their state is saved as paused-resumable
// "queued") and waits for workers to exit.
func (m *Manager) Close() {
	m.stop()
	m.stopped.Wait()
}

func (m *Manager) kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) Add(r AddRequest) (*store.Download, error) {
	if _, err := m.engine.Sources.For(r.Request.URL); err != nil {
		return nil, err
	}
	dir := r.Dir
	if dir == "" {
		dir = m.cfg.DefaultDir
	}
	d := &store.Download{
		ID: newID(), Req: r.Request, Dir: dir, Filename: r.Filename, Status: store.Queued,
	}
	if err := m.store.Save(d); err != nil {
		return nil, err
	}
	m.publish(Event{Type: "update", Download: d})
	m.kick()
	return d, nil
}

func (m *Manager) List() ([]*store.Download, error) { return m.store.List() }

func (m *Manager) Get(id string) (*store.Download, error) { return m.store.Get(id) }

func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	cancel, running := m.active[id]
	m.mu.Unlock()
	if running {
		// worker saves state; mark paused so it isn't requeued on cancellation
		m.setStatus(id, store.Paused)
		cancel()
		return nil
	}
	return m.setStatus(id, store.Paused)
}

func (m *Manager) Resume(id string) error {
	d, err := m.store.Get(id)
	if err != nil {
		return err
	}
	if d.Status == store.Completed || d.Status == store.Active {
		return nil
	}
	d.Status, d.Error = store.Queued, ""
	if err := m.save(d); err != nil {
		return err
	}
	m.kick()
	return nil
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	if cancel, ok := m.active[id]; ok {
		cancel()
	}
	m.mu.Unlock()
	if err := m.store.Delete(id); err != nil {
		return err
	}
	m.publish(Event{Type: "removed", ID: id})
	return nil
}

func (m *Manager) setStatus(id string, s store.Status) error {
	d, err := m.store.Get(id)
	if err != nil {
		return err
	}
	if d.Status == store.Completed {
		return nil
	}
	d.Status = s
	return m.save(d)
}

func (m *Manager) save(d *store.Download) error {
	if err := m.store.Save(d); err != nil {
		return err
	}
	c := *d
	m.publish(Event{Type: "update", Download: &c})
	return nil
}

// ---- scheduling ----

func (m *Manager) loop() {
	defer m.stopped.Done()
	var workers sync.WaitGroup
	defer workers.Wait()
	t := time.NewTicker(2 * time.Second) // safety net if a kick is ever missed
	defer t.Stop()
	for {
		m.schedule(&workers)
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-t.C:
		}
	}
}

func (m *Manager) schedule(workers *sync.WaitGroup) {
	m.mu.Lock()
	free := m.cfg.MaxConcurrent - len(m.active)
	m.mu.Unlock()
	if free <= 0 {
		return
	}
	all, err := m.store.List()
	if err != nil {
		return
	}
	for _, d := range all {
		if free <= 0 {
			break
		}
		if d.Status != store.Queued {
			continue
		}
		m.mu.Lock()
		_, running := m.active[d.ID]
		if running {
			m.mu.Unlock()
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		m.active[d.ID] = cancel
		m.mu.Unlock()
		d.Status = store.Active
		_ = m.save(d)
		free--
		workers.Add(1)
		go func(d *store.Download) {
			defer workers.Done()
			m.run(ctx, d)
			m.mu.Lock()
			delete(m.active, d.ID)
			m.mu.Unlock()
			m.kick()
		}(d)
	}
}

func (m *Manager) run(ctx context.Context, d *store.Download) {
	last := time.Now()
	err := m.engine.Run(ctx, &d.State, engine.Options{
		Req: d.Req, Dir: d.Dir, Filename: d.Filename,
		Connections: m.cfg.Connections, RateLimit: m.cfg.RateLimit,
	}, func(s engine.State) {
		d.Size, d.Downloaded = s.Size, s.Downloaded()
		if d.Filename == "" {
			d.Filename = s.Filename
		}
		d.State = s // always keep the latest so the final save is complete
		c := *d     // subscribers get a copy; d keeps mutating
		m.publish(Event{Type: "update", Download: &c})
		if time.Since(last) >= time.Second { // persist ~1/s
			last = time.Now()
			_ = m.store.Save(d)
		}
	})

	// Re-read to see whether the user paused/removed while we ran.
	cur, gerr := m.store.Get(d.ID)
	if errors.Is(gerr, store.ErrNotFound) {
		return // removed
	}
	switch {
	case err == nil:
		d.Status, d.Error = store.Completed, ""
		d.Size, d.Downloaded = d.State.Size, d.State.Downloaded()
	case ctx.Err() != nil && m.ctx.Err() != nil:
		d.Status = store.Queued // shutdown: resume on next start
	case ctx.Err() != nil && cur != nil && cur.Status == store.Paused:
		d.Status = store.Paused
	default:
		d.Status, d.Error = store.Failed, err.Error()
	}
	_ = m.save(d)
}

// ---- events ----

// Subscribe returns a channel of events and a cancel func.
func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

func (m *Manager) publish(e Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- e:
		default: // slow consumer: drop; clients re-sync via GET /downloads
		}
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
