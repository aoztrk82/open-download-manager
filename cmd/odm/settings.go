package main

import (
	"maps"
	"slices"
	"sync"

	"golang.org/x/time/rate"

	"github.com/aoztrk82/open-download-manager/internal/config"
	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/queue"
)

// settingsStore holds the Settings dialog's choices for the local daemon:
// saved in prefs.json, applied to the queue at once.
type settingsStore struct {
	mu     sync.Mutex
	s      config.Settings
	flags  daemonOpts // `serve --local` flags win over the saved settings
	shared *rate.Limiter
	q      *queue.Manager
	notify func(config.Settings)
}

func newSettingsStore(o daemonOpts) *settingsStore {
	st := &settingsStore{s: config.LoadPrefs().Settings, flags: o, notify: o.OnSettings}
	if n, err := st.s.Normalize(); err == nil {
		st.s = n
	}
	st.shared = engine.NewSharedLimiter(st.s.TotalLimit * 1024)
	return st
}

func (st *settingsStore) Settings() config.Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.copyLocked()
}

// copyLocked hands out a copy nobody can change the stored maps and slices through.
func (st *settingsStore) copyLocked() config.Settings {
	c := st.s
	c.Capture = maps.Clone(c.Capture)
	c.BrowserTypes, c.AutoTypes = slices.Clone(c.BrowserTypes), slices.Clone(c.AutoTypes)
	c.AutoSites, c.SkipSites = slices.Clone(c.AutoSites), slices.Clone(c.SkipSites)
	c.HostConnections = slices.Clone(c.HostConnections)
	c.Categories = slices.Clone(c.Categories)
	for i := range c.Categories {
		c.Categories[i].Exts = slices.Clone(c.Categories[i].Exts)
	}
	if st.flags.Dir != "" {
		c.DefaultDir = st.flags.Dir
	}
	return c
}

func (st *settingsStore) SetSettings(s config.Settings) error {
	p := config.LoadPrefs()
	p.Settings = s
	if err := config.SavePrefs(p); err != nil {
		return err
	}
	st.mu.Lock()
	st.s = s
	cur := st.copyLocked()
	st.mu.Unlock()
	engine.SetShared(st.shared, s.TotalLimit*1024)
	if st.q != nil {
		st.q.SetConfig(st.queueConfig(cur))
	}
	if st.notify != nil {
		st.notify(cur)
	}
	return nil
}

func (st *settingsStore) queueConfig(s config.Settings) queue.Config {
	c := queue.Config{
		DefaultDir: s.Dir(), MaxConcurrent: s.MaxActive, Connections: s.Connections,
		RateLimit: s.SpeedLimit * 1024, Shared: st.shared,
		DirFor: s.DirFor, ConnectionsFor: s.ConnectionsFor,
	}
	if f := st.flags; f.MaxActive > 0 {
		c.MaxConcurrent = f.MaxActive
	}
	if f := st.flags; f.Connections > 0 {
		c.Connections, c.ConnectionsFor = f.Connections, nil
	}
	if f := st.flags; f.RateLimit > 0 {
		c.RateLimit = f.RateLimit
	}
	return c
}
