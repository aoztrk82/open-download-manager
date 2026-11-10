package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/api"
	"github.com/aoztrk82/open-download-manager/internal/config"
)

// Native messaging lets the browser extension start the local daemon when it is
// not running: a web extension cannot launch processes itself, but it may talk
// to a registered "native host". The host is this same binary.
const (
	nativeHostName     = "odm_host" // must match the registry keys, manifests and the extension code
	firefoxExtensionID = "odm@open-download-manager.local"
)

func localURL() string { return "http://127.0.0.1:" + strconv.Itoa(config.DefaultPort) }

// ensureLocalDaemon starts the local daemon if c cannot reach it, then waits for it.
func ensureLocalDaemon(c *api.Client) error {
	if _, err := c.Info(); err == nil {
		return nil
	}
	if err := startLocalDaemon(); err != nil {
		return fmt.Errorf("starting local daemon: %w", err)
	}
	for i := 0; i < 80; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := c.Info(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("local daemon did not start; run `odm serve --local` to see why")
}

// isNativeHostLaunch recognizes how browsers invoke a native host. They cannot
// pass our own arguments: Chrome/Edge pass the caller's origin, Firefox passes
// the path of the host manifest followed by the add-on id.
func isNativeHostLaunch(args []string) bool {
	if len(args) < 2 {
		return false
	}
	if strings.HasPrefix(args[1], "chrome-extension://") {
		return true
	}
	base := strings.ToLower(filepath.Base(args[1]))
	return strings.HasPrefix(base, "odm-native-") && strings.HasSuffix(base, ".json")
}

func runNativeHostMain() int {
	ensure := func() error { return ensureLocalDaemon(localClientForHost()) }
	if err := serveNativeHost(os.Stdin, os.Stdout, ensure); err != nil {
		fmt.Fprintln(os.Stderr, "native host:", err) // stderr only: stdout carries the protocol
		return 1
	}
	return 0
}

func localClientForHost() *api.Client {
	c := api.NewClient(localURL(), "")
	c.HTTP.Timeout = 2 * time.Second
	return c
}

type nativeRequest struct {
	Cmd string `json:"cmd"`
}

type nativeReply struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Version string `json:"version,omitempty"`
}

// serveNativeHost speaks the browsers' framing: a 4-byte little-endian length
// followed by that many bytes of JSON, in both directions. Nothing else may be
// written to out.
func serveNativeHost(in io.Reader, out io.Writer, ensure func() error) error {
	for {
		var n uint32
		if err := binary.Read(in, binary.LittleEndian, &n); err != nil {
			return nil // the browser closed the pipe
		}
		if n == 0 || n > 1<<20 {
			return fmt.Errorf("bad frame length %d", n)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(in, buf); err != nil {
			return err
		}
		var req nativeRequest
		reply := nativeReply{OK: true, Version: api.Version}
		if err := json.Unmarshal(buf, &req); err != nil {
			reply = nativeReply{Error: "bad json"}
		} else {
			switch req.Cmd {
			case "ping":
			case "start":
				if err := ensure(); err != nil {
					reply = nativeReply{Error: err.Error()}
				}
			default:
				reply = nativeReply{Error: "unknown command " + req.Cmd}
			}
		}
		b, _ := json.Marshal(reply)
		if err := binary.Write(out, binary.LittleEndian, uint32(len(b))); err != nil {
			return err
		}
		if _, err := out.Write(b); err != nil {
			return err
		}
	}
}
