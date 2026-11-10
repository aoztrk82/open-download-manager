package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

// Client is the Go client for the API, used by the CLI.
type Client struct {
	Base  string // e.g. http://127.0.0.1:6800
	Token string
	HTTP  *http.Client
}

func NewClient(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token,
		HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, rd)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var e struct{ Error string }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) Info() (Info, error) {
	var i Info
	return i, c.do("GET", "/v1/info", nil, &i)
}

func (c *Client) Add(r queue.AddRequest) (*store.Download, error) {
	var d store.Download
	return &d, c.do("POST", "/v1/downloads", r, &d)
}

func (c *Client) List() ([]*store.Download, error) {
	var l []*store.Download
	return l, c.do("GET", "/v1/downloads", nil, &l)
}

func (c *Client) Get(id string) (*store.Download, error) {
	var d store.Download
	return &d, c.do("GET", "/v1/downloads/"+id, nil, &d)
}

func (c *Client) Pause(id string) error { return c.do("POST", "/v1/downloads/"+id+"/pause", nil, nil) }
func (c *Client) Resume(id string) error {
	return c.do("POST", "/v1/downloads/"+id+"/resume", nil, nil)
}
func (c *Client) Remove(id string) error { return c.RemoveFile(id, false) }

// RemoveFile removes the download; deleteFile also deletes the file on the daemon's disk.
func (c *Client) RemoveFile(id string, deleteFile bool) error {
	p := "/v1/downloads/" + id
	if deleteFile {
		p += "?deleteFile=true"
	}
	return c.do("DELETE", p, nil, nil)
}
