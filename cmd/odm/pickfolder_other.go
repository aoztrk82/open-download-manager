//go:build !windows

package main

// The native folder chooser is Windows only so far; the dialog falls back to
// typing a path.
var pickFolder func(initial string) (string, error)
