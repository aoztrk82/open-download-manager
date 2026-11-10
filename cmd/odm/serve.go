package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/aoztrk82/open-download-manager/internal/api"
	"github.com/aoztrk82/open-download-manager/internal/config"
)

func serveCmd() *cobra.Command {
	var (
		local  bool
		listen string
		o      daemonOpts
	)
	c := &cobra.Command{
		Use:   "serve",
		Short: "Run the download daemon in the foreground (local mode or remote server)",
		Long: `Run the download daemon in the foreground. This is the headless server mode;
double-clicking odm.exe instead starts the tray app.

  odm serve --local                 listen on 127.0.0.1 only; token is read from / written to
                                    the per-user config dir (used by the CLI and extensions)
  odm serve --listen :6800 --token  listen on all interfaces; token is mandatory`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if local && listen != "" {
				return errors.New("use either --local or --listen")
			}
			if listen == "" {
				local = true
			}
			o.Addr = listen
			o.Reveal = local
			if local {
				o.Addr = "127.0.0.1:" + strconv.Itoa(config.DefaultPort)
				if o.Token == "" {
					t, err := config.LoadOrCreateToken()
					if err != nil {
						return err
					}
					o.Token = t
				}
			} else if o.Token == "" {
				if o.Token = os.Getenv("ODM_TOKEN"); o.Token == "" {
					return errors.New("--token (or ODM_TOKEN) is required for server mode")
				}
			}
			d, err := startDaemon(o)
			if err != nil {
				return err
			}
			defer d.Close()
			fmt.Fprintf(os.Stderr, "odm %s listening on http://%s\n", api.Version, d.Addr)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			select {
			case <-ctx.Done():
				return nil
			case err := <-d.Done():
				return err
			}
		},
	}
	f := c.Flags()
	f.BoolVar(&local, "local", false, "listen on 127.0.0.1 only (default when --listen is not given)")
	f.StringVar(&listen, "listen", "", "address to listen on, e.g. :6800 (server mode)")
	f.StringVar(&o.Token, "token", "", "API token (server mode; env ODM_TOKEN)")
	f.StringVar(&o.Dir, "dir", "", "default download directory")
	f.IntVar(&o.MaxActive, "max-active", 3, "simultaneous downloads")
	f.IntVar(&o.Connections, "connections", 4, "connections per download")
	f.Int64Var(&o.RateLimit, "limit", 0, "bytes/sec per download (0 = unlimited)")
	f.BoolVar(&o.NoUI, "no-ui", false, "disable the Web UI")
	return c
}
