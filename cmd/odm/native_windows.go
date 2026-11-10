//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"

	"github.com/aoztrk82/open-download-manager/internal/config"
)

// Pinned by the "key" field in extension/manifest.chrome.json, so the id of the
// unpacked extension is the same on every machine.
const chromeExtensionID = "linelmlgjkggjofcfgljpdkfgdjgabkl"

// registerNativeHost tells Chrome, Edge and Firefox that this exe is the native
// host named nativeHostName. Per-user (HKCU), so no admin rights are needed.
// It is idempotent and re-run on every tray start so a moved exe is picked up.
func registerNativeHost() error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "native")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	base := map[string]any{
		"name":        nativeHostName,
		"description": "Starts the Open Download Manager from the browser extension",
		"path":        exe,
		"type":        "stdio",
	}
	write := func(name string, extra map[string]any) (string, error) {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		p := filepath.Join(dir, name)
		return p, os.WriteFile(p, b, 0o644)
	}
	chrome, err := write("odm-native-chrome.json", map[string]any{
		"allowed_origins": []string{"chrome-extension://" + chromeExtensionID + "/"},
	})
	if err != nil {
		return err
	}
	firefox, err := write("odm-native-firefox.json", map[string]any{
		"allowed_extensions": []string{firefoxExtensionID},
	})
	if err != nil {
		return err
	}
	keys := map[string]string{
		`Software\Google\Chrome\NativeMessagingHosts\` + nativeHostName:  chrome,
		`Software\Microsoft\Edge\NativeMessagingHosts\` + nativeHostName: chrome,
		`Software\Mozilla\NativeMessagingHosts\` + nativeHostName:        firefox,
	}
	for path, manifest := range keys {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			return err
		}
		err = k.SetStringValue("", manifest)
		k.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
