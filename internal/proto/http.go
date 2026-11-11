package proto

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// HTTP implements Source for http and https.
type HTTP struct{ client *http.Client }

// NewHTTP returns an HTTP source. A nil client uses http.DefaultClient.
func NewHTTP(c *http.Client) *HTTP {
	if c == nil {
		c = &http.Client{}
	}
	return &HTTP{client: c}
}

func (h *HTTP) newReq(ctx context.Context, method string, r Request) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.URL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	if r.Cookies != "" {
		req.Header.Set("Cookie", r.Cookies)
	}
	if r.Referer != "" {
		req.Header.Set("Referer", r.Referer)
	}
	if r.UserAgent != "" {
		req.Header.Set("User-Agent", r.UserAgent)
	}
	// Avoid transparent gzip: it breaks byte ranges and size accounting.
	req.Header.Set("Accept-Encoding", "identity")
	return req, nil
}

// Probe issues a 1-byte ranged GET (more widely supported than HEAD).
func (h *HTTP) Probe(ctx context.Context, r Request) (Info, error) {
	req, err := h.newReq(ctx, http.MethodGet, r)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := h.client.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))

	info := Info{Size: -1, FinalURL: resp.Request.URL.String()}
	switch resp.StatusCode {
	case http.StatusPartialContent:
		info.AcceptRanges = true
		// Content-Range: bytes 0-0/12345
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndexByte(cr, '/'); i >= 0 {
				if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil {
					info.Size = n
				}
			}
		}
	case http.StatusOK:
		info.AcceptRanges = false
		if resp.ContentLength >= 0 {
			info.Size = resp.ContentLength
		}
	default:
		return Info{}, fmt.Errorf("http status %s", resp.Status)
	}
	info.Filename = filenameFrom(resp)
	return info, nil
}

// Open streams a byte range.
func (h *HTTP) Open(ctx context.Context, r Request, offset, length int64) (io.ReadCloser, error) {
	req, err := h.newReq(ctx, http.MethodGet, r)
	if err != nil {
		return nil, err
	}
	ranged := offset > 0 || length >= 0
	if ranged {
		end := ""
		if length >= 0 {
			end = strconv.FormatInt(offset+length-1, 10)
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%s", offset, end))
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	if ranged && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, fmt.Errorf("server ignored range request: %s", resp.Status)
	}
	if !ranged && resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("http status %s", resp.Status)
	}
	total := resp.ContentLength
	if ranged {
		// Parallel connections write wherever the response says it starts:
		// a response for another offset would corrupt the file.
		start, t, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok || start != offset {
			resp.Body.Close()
			return nil, fmt.Errorf("server sent range starting at %d, asked for %d", start, offset)
		}
		total = t
	}
	return &body{ReadCloser: resp.Body, total: total}, nil
}

// body is a response body that also knows the full size of the resource, so
// the engine can refuse a mirror that serves a different file.
type body struct {
	io.ReadCloser
	total int64 // -1 unknown
}

func (b *body) Total() int64 { return b.total }

// parseContentRange reads "bytes 100-199/1000" (total may be "*").
func parseContentRange(cr string) (start, total int64, ok bool) {
	cr, found := strings.CutPrefix(strings.TrimSpace(cr), "bytes ")
	if !found {
		return 0, -1, false
	}
	rng, tot, found := strings.Cut(cr, "/")
	if !found {
		return 0, -1, false
	}
	a, _, found := strings.Cut(rng, "-")
	if !found {
		return 0, -1, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(a), 10, 64)
	if err != nil {
		return 0, -1, false
	}
	total = -1
	if n, err := strconv.ParseInt(strings.TrimSpace(tot), 10, 64); err == nil {
		total = n
	}
	return start, total, true
}

func filenameFrom(resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if n := params["filename"]; n != "" {
				return path.Base(strings.ReplaceAll(n, "\\", "/"))
			}
		}
	}
	return NameFromURL(resp.Request.URL.String())
}

// NameFromURL derives a file name from the last path element of rawURL.
func NameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "download"
	}
	name, _ := url.PathUnescape(path.Base(u.Path))
	if name == "" || name == "." || name == "/" {
		return "download"
	}
	return name
}
