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
	return resp.Body, nil
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
