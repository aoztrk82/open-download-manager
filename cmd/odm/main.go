// Command odm is the Open Download Manager: daemon/server and CLI in one binary.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aoztrk82/open-download-manager/internal/api"
	"github.com/aoztrk82/open-download-manager/internal/proto"
	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

var (
	serverURL string
	token     string
)

func main() {
	// Browsers launch the native-messaging host as `odm.exe <origin>` /
	// `odm.exe <manifest.json> <id>`: they cannot pass a subcommand.
	if isNativeHostLaunch(os.Args) {
		os.Exit(runNativeHostMain())
	}
	// Cobra refuses to run when launched from Explorer ("This is a command line
	// tool"). Double-clicking is exactly how the tray app is started.
	cobra.MousetrapHelpText = ""
	root := &cobra.Command{
		Use:           "odm",
		Short:         "Open Download Manager",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&serverURL, "server", envOr("ODM_SERVER", ""),
		"daemon URL (default: local daemon, auto-started; env ODM_SERVER)")
	root.PersistentFlags().StringVar(&token, "token", envOr("ODM_TOKEN", ""),
		"API token for a remote --server (local daemon needs none; env ODM_TOKEN)")
	rootRun, guiCmds := guiCommands()
	root.RunE = rootRun // no arguments (double-click) -> tray app on Windows
	root.AddCommand(guiCmds...)
	root.AddCommand(serveCmd(), addCmd(), listCmd(), watchCmd(),
		simpleCmd("pause", "Pause a download", (*api.Client).Pause),
		simpleCmd("resume", "Resume a download", (*api.Client).Resume),
		rmCmd())
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
		return api.NewClient(serverURL, token), nil // a remote server answers 401 if it needs a token
	}
	c := api.NewClient(localURL(), "")
	if err := ensureLocalDaemon(c); err != nil {
		return nil, err
	}
	return c, nil
}

// cliDir makes --dir absolute for the local daemon (it only accepts absolute
// folders, and "./isos" means the shell's directory here). A remote server's
// paths are its own, so they are sent as typed.
func cliDir(dir string, remote bool) (string, error) {
	if dir == "" || remote {
		return dir, nil
	}
	return filepath.Abs(dir)
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
			dir, err := cliDir(dir, serverURL != "")
			if err != nil {
				return err
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

func rmCmd() *cobra.Command {
	var deleteFile bool
	c := &cobra.Command{
		Use: "rm <id>...", Short: "Remove a download from the list", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			for _, id := range args {
				if err := cl.RemoveFile(id, deleteFile); err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&deleteFile, "delete-file", false, "also delete the downloaded file from disk")
	return c
}
