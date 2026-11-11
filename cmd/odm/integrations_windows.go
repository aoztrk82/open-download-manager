//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows/registry"

	"github.com/aoztrk82/open-download-manager/internal/api"
	"github.com/aoztrk82/open-download-manager/internal/config"
	"github.com/aoztrk82/open-download-manager/internal/extbundle"
)

// winIntegrations installs the extensions bundled in odm.exe.
//   - Firefox: the Mozilla-signed xpi is saved and opened with firefox.exe,
//     which shows Firefox's own "Add ODM?" prompt.
//   - Chrome/Edge: neither allows installing a non-store extension from outside
//     on Windows, so the unpacked build is kept in a fixed folder the user loads
//     once with "Load unpacked"; the browser re-reads it on restart, so it
//     follows odm.exe updates.
type winIntegrations struct{}

func newIntegrations() api.Integrations { return winIntegrations{} }

var browserExe = map[string]string{"firefox": "firefox.exe", "chrome": "chrome.exe", "edge": "msedge.exe"}

var extensionsPage = map[string]string{"chrome": "chrome://extensions/", "edge": "edge://extensions/"}

// appPath finds a browser through the App Paths key its installer writes.
func appPath(exe string) string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\App Paths\`+exe, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		p, _, err := k.GetStringValue("")
		k.Close()
		if err == nil && p != "" {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func extensionDir() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "extension"), nil
}

// syncChromeFolder refreshes the unpacked Chrome/Edge extension folder.
func syncChromeFolder() (string, error) {
	base, err := extensionDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "chrome")
	_, err = extbundle.SyncChrome(dir, filepath.Join(base, "chrome.hash"))
	return dir, err
}

func (winIntegrations) Browser(id string) (bool, string) {
	installed := appPath(browserExe[id]) != ""
	if api.Family(id) != "chrome" {
		return installed, ""
	}
	base, err := extensionDir()
	if err != nil {
		return installed, ""
	}
	return installed, filepath.Join(base, "chrome")
}

func (w winIntegrations) Install(id string) error {
	exe := appPath(browserExe[id])
	if exe == "" {
		return fmt.Errorf("%s was not found on this computer", browserExe[id])
	}
	if id == "firefox" {
		base, err := extensionDir()
		if err != nil {
			return err
		}
		xpi, err := extbundle.WriteFirefoxXPI(base)
		if errors.Is(err, extbundle.ErrNotBundled) {
			return errors.New("this ODM build has no signed Firefox extension (run `pnpm sign` in extension/ and rebuild)")
		}
		if err != nil {
			return err
		}
		return startDetached(exe, false, xpi)
	}
	if err := w.OpenFolder(id); err != nil {
		return err
	}
	// The extensions page cannot be scripted from outside; opening it is the
	// most we can do. The dialog shows the steps and the folder path as well.
	return startDetached(exe, false, extensionsPage[id])
}

func (winIntegrations) OpenFolder(id string) error {
	if api.Family(id) != "chrome" {
		return errors.New("only Chrome and Edge load the extension from a folder")
	}
	dir, err := syncChromeFolder()
	if errors.Is(err, extbundle.ErrNotBundled) {
		return errors.New("this ODM build has no Chrome extension (run `pnpm build` in extension/ and rebuild)")
	}
	if err != nil {
		return err
	}
	return exec.Command("explorer.exe", dir).Start()
}
