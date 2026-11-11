package api

import (
	"encoding/json"
	"errors"
	"testing"
)

type fakeLang struct{ lang string }

func (f *fakeLang) Lang() string { return f.lang }
func (f *fakeLang) SetLang(l string) error {
	if l != "en" && l != "tr" {
		return errors.New("unknown language")
	}
	f.lang = l
	return nil
}

func TestPrefsLang(t *testing.T) {
	if got := do(dialogServer(t, &Server{}), "PUT", "/v1/prefs", self, jsonHdr, `{"lang":"tr"}`).Code; got != 501 {
		t.Fatalf("server without prefs: %d", got)
	}
	f := &fakeLang{lang: "en"}
	h := dialogServer(t, &Server{AnyDir: true, Lang: f})
	for _, c := range []struct {
		name string
		hdr  map[string]string
		body string
		want int
	}{
		{"text/plain", map[string]string{"Content-Type": "text/plain"}, `{"lang":"tr"}`, 415},
		{"hostile", map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example.com"}, `{"lang":"tr"}`, 403},
		{"unknown", jsonHdr, `{"lang":"xx"}`, 400},
		{"ok", jsonHdr, `{"lang":"tr"}`, 204},
	} {
		if got := do(h, "PUT", "/v1/prefs", self, c.hdr, c.body).Code; got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	var info Info
	_ = json.Unmarshal(do(h, "GET", "/v1/info", self, nil, "").Body.Bytes(), &info)
	if info.Lang != "tr" {
		t.Fatalf("info.lang = %q", info.Lang)
	}
}
