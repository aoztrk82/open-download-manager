//go:build !windows

package main

// Native-host registration is only implemented for Windows (registry).
func registerNativeHost() error { return nil }
