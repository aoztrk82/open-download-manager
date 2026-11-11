//go:build !windows

package main

import "github.com/aoztrk82/open-download-manager/internal/api"

// Browser-extension install is only implemented for Windows so far.
func newIntegrations() api.Integrations { return nil }
