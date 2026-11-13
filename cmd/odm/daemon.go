package main

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/api"
	"github.com/aoztrk82/open-download-manager/internal/applog"
	"github.com/aoztrk82/open-download-manager/internal/config"
	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/extbundle"
	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
	"github.com/aoztrk82/open-download-manager/internal/web"
)

type daemonOpts struct {
	Addr        string
	Token       string
	Dir         string
	MaxActive   int
	Connections int
	RateLimit   int64
	NoUI        bool
	Reveal      bool              // allow "show in folder" (local daemon only)
	OnLang      func(lang string) // tray: the UI language was changed in the window
	// Local daemon (Reveal): Settings dialog. OnSettings runs after a change.
	OnSettings func(config.Settings)
	Autostart  api.Autostart // nil where ODM cannot start with the OS
	// NoIntegrations: a second local daemon on another port. Installing the
	// browser extensions belongs to the one on the default port they talk to.
	NoIntegrations bool
}

// daemon is a running queue + HTTP API. Shared by `odm serve` and the tray app.
type daemon struct {
	Addr string
	http *http.Server
	q    *queue.Manager
	st   *store.Store
	errc chan error
	// settings is nil for a remote server (its flags are its settings).
	settings *settingsStore
}

func startDaemon(o daemonOpts) (*daemon, error) {
	flagDir := o.Dir
	if o.Dir == "" {
		o.Dir = config.DefaultDownloadDir()
	}
	cfgDir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(cfgDir, "odm.db"))
	if err != nil {
		return nil, err
	}
	qcfg := queue.Config{DefaultDir: o.Dir, MaxConcurrent: o.MaxActive, Connections: o.Connections, RateLimit: o.RateLimit}
	var sets *settingsStore
	if o.Reveal {
		fo := o
		fo.Dir = flagDir // only an explicit --dir overrides the saved folder
		sets = newSettingsStore(fo)
		qcfg = sets.queueConfig(sets.Settings())
	}
	// The download log: Settings > General on the local daemon; always on for a
	// server (its operator reads it on the machine).
	logOn := true
	if sets != nil {
		logOn = sets.Settings().Log
	}
	applog.Setup(filepath.Join(cfgDir, "logs"), logOn)
	applog.L().Info("odm started", "version", api.Version, "addr", o.Addr, "local", o.Reveal)
	qcfg.Log = applog.L()
	q := queue.New(qcfg, st, engine.New())
	if sets != nil {
		sets.q = q
	}
	if err := q.Start(); err != nil {
		st.Close()
		return nil, err
	}
	srv := &api.Server{Queue: q, Token: o.Token, DefaultDir: o.Dir, Extensions: map[string]string{
		"chrome": extbundle.ChromeVersion(), "firefox": extbundle.FirefoxVersion(),
	}}
	if !o.NoUI {
		srv.UI = web.FS()
	}
	if o.Reveal {
		srv.Reveal = revealInFolder
		if !o.NoIntegrations {
			srv.Integrations = newIntegrations()
		}
		srv.AnyDir = true
		srv.PickFolder = pickFolder
		srv.Lang = progLang{onChange: o.OnLang}
		srv.Settings = sets
		srv.Autostart = o.Autostart
		srv.Open = openWithDefault
	}
	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		q.Close()
		st.Close()
		return nil, err
	}
	d := &daemon{
		Addr: ln.Addr().String(),
		http: &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second},
		q:    q, st: st, errc: make(chan error, 1), settings: sets,
	}
	go func() { d.errc <- d.http.Serve(ln) }()
	return d, nil
}

// Done reports a fatal serve error.
func (d *daemon) Done() <-chan error { return d.errc }

func (d *daemon) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.http.Shutdown(ctx)
	d.q.Close() // saves in-flight state so downloads resume next start
	d.st.Close()
}
