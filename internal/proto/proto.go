// Package proto defines the protocol-agnostic source interface used by the
// download engine, and a registry that maps URL schemes to implementations.
package proto

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// Request describes what to fetch. Headers/Cookies/Referer are only
// meaningful for HTTP(S) and are ignored by other protocols.
type Request struct {
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Cookies   string            `json:"cookies,omitempty"`
	Referer   string            `json:"referer,omitempty"`
	UserAgent string            `json:"userAgent,omitempty"`
}

// Info is the result of probing a remote resource.
type Info struct {
	Size         int64 // -1 if unknown
	AcceptRanges bool  // true if Open can start at an arbitrary offset
	Filename     string
	FinalURL     string
}

// Source is implemented once per protocol family.
type Source interface {
	// Probe discovers size, range support and a suggested file name.
	Probe(ctx context.Context, req Request) (Info, error)
	// Open streams bytes [offset, offset+length). length < 0 means "to end".
	Open(ctx context.Context, req Request, offset, length int64) (io.ReadCloser, error)
}

// ErrUnsupportedScheme is returned for unknown URL schemes.
type ErrUnsupportedScheme struct{ Scheme string }

func (e ErrUnsupportedScheme) Error() string {
	return fmt.Sprintf("unsupported scheme %q", e.Scheme)
}

// Registry maps URL schemes to Sources.
type Registry map[string]Source

// Default returns a registry with all built-in protocols.
func Default() Registry {
	h := NewHTTP(nil)
	f := NewFTP()
	return Registry{"http": h, "https": h, "ftp": f, "ftps": f}
}

// For returns the Source responsible for rawURL.
func (r Registry) For(rawURL string) (Source, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	s, ok := r[strings.ToLower(u.Scheme)]
	if !ok {
		return nil, ErrUnsupportedScheme{u.Scheme}
	}
	return s, nil
}
