// Command odm is the Open Download Manager: daemon/server and CLI in one binary.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aoztrk82/open-download-manager/internal/api"
	"github.com/aoztrk82/open-download-manager/internal/config"
	"github.com/aoztrk82/open-download-manager/internal/proto"
	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

var (
	serverURL string
	token     string
)

func main() {
	root := &cobra.Command{
		Use:           "odm",
		Short:         "Open Download Manager",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&serverURL, "server", envOr("ODM_SERVER", ""),
		"daemon URL (default: local daemon, auto-started; env ODM_SERVER)")
	root.PersistentFlags().StringVar(&token, "token", envOr("ODM_TOKEN", ""),
		"API token (default: local token file; env ODM_TOKEN)")
	root.AddCommand(serveCmd(), addCmd(), listCmd(), watchCmd(),
		simpleCmd("pause", "Pause a download", (*api.Client).Pause),
		simpleCmd("resume", "Resume a download", (*api.Client).Resume),
		simpleCmd("rm", "Remove a download from the list", (*api.Client).Remove))
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// client builds an API client, auto-starting the local daemon when no
// remote --server was given.
func client() (*api.Client, error) {
	if serverURL != "" {
		if token == "" {
			return nil, fmt.Errorf("--token (or ODM_TOKEN) is required with --server")
		}
		return api.NewClient(serverURL, token), nil
	}
	tok := token
	if tok == "" {
		t, err := config.LoadOrCreateToken()
		if err != nil {
			return nil, err
		}
		tok = t
	}
	c := api.NewClient("http://127.0.0.1:"+strconv.Itoa(config.DefaultPort), tok)
	if _, err := c.Info(); err == nil {
		return c, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "serve", "--local")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting local daemon: %w", err)
	}
	_ = cmd.Process.Release()
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := c.Info(); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("local daemon did not start; run `odm serve --local` to see why")
}

func addCmd() *cobra.Command {
	var dir, name, referer, cookies string
	var headers []string
	c := &cobra.Command{
		Use:   "add <url>...",
		Short: "Add one or more URLs (http, https, ftp, ftps)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			hdr := map[string]string{}
			for _, h := range headers {
				k, v, ok := strings.Cut(h, ":")
				if !ok {
					return fmt.Errorf("bad --header %q (want 'Name: value')", h)
				}
				hdr[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
			for _, u := range args {
				d, err := cl.Add(queue.AddRequest{
					Request: proto.Request{URL: u, Headers: hdr, Referer: referer, Cookies: cookies},
					Dir:     dir, Filename: name,
				})
				if err != nil {
					return fmt.Errorf("%s: %w", u, err)
				}
				fmt.Println(d.ID)
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringVarP(&dir, "dir", "d", "", "destination directory (daemon-side path)")
	f.StringVarP(&name, "output", "o", "", "file name")
	f.StringVar(&referer, "referer", "", "Referer header")
	f.StringVar(&cookies, "cookies", "", "Cookie header value")
	f.StringArrayVarP(&headers, "header", "H", nil, "extra header, repeatable")
	return c
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "List downloads",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			all, err := cl.List()
			if err != nil {
				return err
			}
			printTable(all)
			return nil
		},
	}
}

func watchCmd() *cobra.Command {
	return &cobra.Command{
		Use: "watch", Short: "Live view of downloads (Ctrl+C to exit)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			for {
				all, err := cl.List()
				if err != nil {
					return err
				}
				fmt.Print("\x1b[H\x1b[2J") // clear screen (works in Windows Terminal / modern conhost)
				printTable(all)
				time.Sleep(500 * time.Millisecond)
			}
		},
	}
}

func simpleCmd(use, short string, fn func(*api.Client, string) error) *cobra.Command {
	return &cobra.Command{
		Use: use + " <id>...", Short: short, Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			for _, id := range args {
				if err := fn(cl, id); err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
			}
			return nil
		},
	}
}

func printTable(all []*store.Download) {
	fmt.Printf("%-16s %-10s %6s  %-24s %s\n", "ID", "STATUS", "PROG", "SIZE", "FILE")
	for _, d := range all {
		prog := "-"
		if d.Size > 0 {
			prog = fmt.Sprintf("%d%%", d.Downloaded*100/d.Size)
		}
		size := human(d.Downloaded)
		if d.Size > 0 {
			size += " / " + human(d.Size)
		}
		name := d.Filename
		if name == "" {
			name = d.Req.URL
		}
		line := fmt.Sprintf("%-16s %-10s %6s  %-24s %s", d.ID, d.Status, prog, size, name)
		if d.Error != "" {
			line += "  (" + d.Error + ")"
		}
		fmt.Println(line)
	}
}

func human(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for m := n / u; m >= u; m /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
