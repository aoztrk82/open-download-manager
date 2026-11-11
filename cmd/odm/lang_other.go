//go:build !windows

package main

import (
	"os"
	"strings"
)

// systemLang reads the POSIX locale (LC_ALL, LC_MESSAGES, LANG).
func systemLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "tr") {
				return "tr"
			}
			return "en"
		}
	}
	return "en"
}
