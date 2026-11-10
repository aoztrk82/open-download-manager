package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/aoztrk82/open-download-manager/internal/api"
	"github.com/aoztrk82/open-download-manager/internal/config"
	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
	"github.com/aoztrk82/open-download-manager/internal/web"
)

func serveCmd() *cobra.Command {
	var (
		local       bool
		listen      string
		token       string
		dir         string
		maxActive   int
		connections int
		rateLimit   int64
		noUI        bool
	)
	c := &cobra.Command{
		Use:   "serve",
		Short: "Run the download daemon (local mode or remote server)",
		Long: `Run the download daemon.

  odm serve --local                 listen on 127.0.0.1 only; token is read from / written to
                                    the per-user config dir (this is what the CLI and the
                                    browser extension use on a desktop)
  odm serve --listen :6800 --token  listen on all interfaces; token is mandatory`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if local && listen != "" {
				return errors.New("use either --local or --listen")
			}
			if !local && listen == "" {
				local = true
			}
			addr := listen
			if local {
				addr = "127.0.0.1:" + strconv.Itoa(config.DefaultPort)
				if listen != "" {
					addr = listen
				}
				if token == "" {
					t, err := config.LoadOrCreateToken()
					if err != nil {
						return err
					}
					token = t
				}
			} else if token == "" {
				if token = os.Getenv("ODM_TOKEN"); token == "" {
					return errors.New("--token (or ODM_TOKEN) is required for server mode")
				}
			}
			if dir == "" {
				dir = config.DefaultDownloadDir()
			}
			cfgDir, err := config.Dir()
			if err != nil {
				return err
			}
			st, err := store.Open(filepath.Join(cfgDir, "odm.db"))
			if err != nil {
				return err
			}
			defer st.Close()

			q := queue.New(queue.Config{
				DefaultDir: dir, MaxConcurrent: maxActive, Connections: connections, RateLimit: rateLimit,
			}, st, engine.New())
			if err := q.Start(); err != nil {
				return err
			}
			defer q.Close()

			srv := &api.Server{Queue: q, Token: token, DefaultDir: dir}
			if !noUI {
				srv.UI = web.FS()
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			fmt.Fprintf(os.Stderr, "odm %s listening on http://%s (downloads -> %s)\n", api.Version, ln.Addr(), dir)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			errc := make(chan error, 1)
			go func() { errc <- hs.Serve(ln) }()
			select {
			case <-ctx.Done():
			case err := <-errc:
				return err
			}
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return hs.Shutdown(sctx)
		},
	}
	f := c.Flags()
	f.BoolVar(&local, "local", false, "listen on 127.0.0.1 only (default when --listen is not given)")
	f.StringVar(&listen, "listen", "", "address to listen on, e.g. :6800 (server mode)")
	f.StringVar(&token, "token", "", "API token (server mode; env ODM_TOKEN)")
	f.StringVar(&dir, "dir", "", "default download directory")
	f.IntVar(&maxActive, "max-active", 3, "simultaneous downloads")
	f.IntVar(&connections, "connections", 4, "connections per download")
	f.Int64Var(&rateLimit, "limit", 0, "bytes/sec per download (0 = unlimited)")
	f.BoolVar(&noUI, "no-ui", false, "disable the Web UI")
	return c
}
