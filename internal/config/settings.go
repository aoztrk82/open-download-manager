package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// Settings are the choices in the Settings dialog (local daemon only). They
// live in prefs.json next to the language and take effect without a restart.
type Settings struct {
	// Capture says per browser ("chrome", "edge", "firefox") whether its
	// extension hands downloads to ODM. A missing browser means yes.
	Capture map[string]bool `json:"capture"`

	// File types and sites, applied by the extensions when they capture a
	// download. Extensions are lowercase without the dot; sites are host names
	// and also match their subdomains.
	BrowserTypes []string `json:"browserTypes"` // the browser keeps these (e.g. pdf)
	AutoTypes    []string `json:"autoTypes"`    // start without the download dialog
	AutoSites    []string `json:"autoSites"`    // start without the dialog when from these sites
	SkipSites    []string `json:"skipSites"`    // never capture on these sites

	// DefaultDir is where downloads go; "" = the user's Downloads folder.
	DefaultDir string `json:"defaultDir"`
	// UseCategories sends files without an explicit folder to their
	// category's folder (Video, Music, ...).
	UseCategories bool       `json:"useCategories"`
	Categories    []Category `json:"categories"`

	MaxActive    int  `json:"maxActive"`    // downloads running at once
	ShowProgress bool `json:"showProgress"` // progress window when a download starts
	ShowComplete bool `json:"showComplete"` // "download complete" window at the end

	// Connections is the most connections per download, 0 = automatic (CPU
	// threads); HostConnections overrides it for some sites.
	Connections     int         `json:"connections"`
	HostConnections []HostLimit `json:"hostConnections"`
	SpeedLimit      int64       `json:"speedLimit"` // KiB/s per download, 0 = unlimited
	// Log keeps the download log (<config>/logs/odm.log, rotated): what was
	// asked, what servers answered, why downloads failed.
	Log        bool  `json:"log"`
	TotalLimit int64 `json:"totalLimit"` // KiB/s for all downloads together, 0 = unlimited
}

// Category is a group of file extensions with its own folder.
type Category struct {
	ID   string   `json:"id"` // one of CategoryIDs; the UI shows a translated name
	Exts []string `json:"exts"`
	// Dir is a folder the user chose; "" = CategoryFolder(ID) inside the
	// download folder, so it moves when the download folder does.
	Dir string `json:"dir"`
}

// HostLimit caps the connections for one site (and its subdomains).
type HostLimit struct {
	Host        string `json:"host"`
	Connections int    `json:"connections"`
}

// CategoryIDs in display order, with the folder name and extensions they get
// by default.
var CategoryIDs = []string{"video", "music", "compressed", "documents", "programs", "images"}

var categoryDefaults = map[string]struct {
	folder string
	exts   string
}{
	"video":      {"Video", "mp4 mkv avi mov webm wmv flv m4v mpg mpeg ts 3gp"},
	"music":      {"Music", "mp3 m4a flac wav ogg opus aac wma"},
	"compressed": {"Compressed", "zip rar 7z tar gz tgz bz2 xz zst iso"},
	"documents":  {"Documents", "pdf doc docx xls xlsx ppt pptx odt ods odp txt rtf epub csv"},
	"programs":   {"Programs", "exe msi msix appx apk deb rpm dmg appimage"},
	"images":     {"Images", "jpg jpeg png gif webp bmp svg heic"},
}

// DefaultSettings is what a fresh install uses.
func DefaultSettings() Settings {
	s := Settings{
		Capture:      map[string]bool{"chrome": true, "edge": true, "firefox": true},
		MaxActive:    3,
		ShowProgress: true,
		ShowComplete: true,
		Log:          true,
	}
	for _, id := range CategoryIDs {
		d := categoryDefaults[id]
		s.Categories = append(s.Categories, Category{ID: id, Exts: strings.Fields(d.exts)})
	}
	return s
}

// Limits on what the dialog may set.
const (
	MaxActiveLimit   = 20
	ConnectionsLimit = 32
	listLimit        = 200
)

// Normalize cleans the lists (lowercase, no dots or schemes, no duplicates)
// and checks the numbers and folders. It returns the cleaned copy.
func (s Settings) Normalize() (Settings, error) {
	s.BrowserTypes = cleanExts(s.BrowserTypes)
	s.AutoTypes = cleanExts(s.AutoTypes)
	var err error
	if s.AutoSites, err = cleanHosts(s.AutoSites); err != nil {
		return s, err
	}
	if s.SkipSites, err = cleanHosts(s.SkipSites); err != nil {
		return s, err
	}
	if len(s.BrowserTypes) > listLimit || len(s.AutoTypes) > listLimit {
		return s, errors.New("too many file types")
	}
	if s.MaxActive < 1 || s.MaxActive > MaxActiveLimit {
		return s, fmt.Errorf("downloads at once must be 1..%d", MaxActiveLimit)
	}
	if s.Connections < 0 || s.Connections > ConnectionsLimit {
		return s, fmt.Errorf("connections must be 0 (automatic) or 1..%d", ConnectionsLimit)
	}
	if s.SpeedLimit < 0 || s.TotalLimit < 0 {
		return s, errors.New("speed limits cannot be negative")
	}
	if s.DefaultDir != "" {
		if s.DefaultDir = filepath.Clean(s.DefaultDir); !filepath.IsAbs(s.DefaultDir) {
			return s, errors.New("the download folder must be an absolute path")
		}
	}
	caps := map[string]bool{}
	for b, on := range s.Capture {
		if b == "chrome" || b == "edge" || b == "firefox" {
			caps[b] = on
		}
	}
	s.Capture = caps

	seen := map[string]bool{}
	var cats []Category
	for _, c := range s.Categories {
		if _, ok := categoryDefaults[c.ID]; !ok || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		c.Exts = cleanExts(c.Exts)
		if c.Dir != "" {
			if c.Dir = filepath.Clean(c.Dir); !filepath.IsAbs(c.Dir) {
				return s, fmt.Errorf("the %s folder must be an absolute path", c.ID)
			}
		}
		cats = append(cats, c)
	}
	s.Categories = cats

	hosts := map[string]bool{}
	var limits []HostLimit
	for _, h := range s.HostConnections {
		cl, err := cleanHosts([]string{h.Host})
		if err != nil {
			return s, err
		}
		if len(cl) == 0 || hosts[cl[0]] {
			continue
		}
		if h.Connections < 1 || h.Connections > ConnectionsLimit {
			return s, fmt.Errorf("connections for %s must be 1..%d", cl[0], ConnectionsLimit)
		}
		hosts[cl[0]] = true
		limits = append(limits, HostLimit{Host: cl[0], Connections: h.Connections})
	}
	if len(limits) > listLimit {
		return s, errors.New("too many sites")
	}
	s.HostConnections = limits
	return s, nil
}

func cleanExts(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range in {
		for _, f := range strings.FieldsFunc(e, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
			f = strings.ToLower(strings.TrimLeft(strings.TrimPrefix(strings.TrimSpace(f), "*"), "."))
			if f == "" || len(f) > 16 || seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// cleanHosts accepts "example.com", "*.example.com" or a pasted URL.
func cleanHosts(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, h := range in {
		for _, f := range strings.Fields(strings.ReplaceAll(h, ",", " ")) {
			f = strings.ToLower(f)
			if strings.Contains(f, "://") {
				u, err := url.Parse(f)
				if err != nil || u.Hostname() == "" {
					return nil, fmt.Errorf("not a site: %s", f)
				}
				f = u.Hostname()
			}
			f = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(f, "*"), "."), ".")
			if f == "" || seen[f] {
				continue
			}
			if strings.ContainsAny(f, "/\\?#@ ") || len(f) > 253 {
				return nil, fmt.Errorf("not a site: %s", f)
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	return out, nil
}

// HostMatches reports whether host is one of the sites or a subdomain of one.
func HostMatches(sites []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, s := range sites {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// Dir is the download folder.
func (s Settings) Dir() string {
	if s.DefaultDir != "" {
		return s.DefaultDir
	}
	return DefaultDownloadDir()
}

// DirFor is the folder for a file when the user did not pick one: its
// category folder when categories are on, else the download folder. media is
// for streams whose name is not known yet (they become video files).
func (s Settings) DirFor(name string, media bool) string {
	if s.UseCategories {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		if media && ext == "" {
			ext = "mp4"
		}
		for _, c := range s.Categories {
			for _, e := range c.Exts {
				if e == ext {
					return s.CategoryDir(c)
				}
			}
		}
	}
	return s.Dir()
}

// CategoryFolder is a category's folder name inside the download folder.
func CategoryFolder(id string) string { return categoryDefaults[id].folder }

// CategoryDir is where a category's files go.
func (s Settings) CategoryDir(c Category) string {
	if c.Dir != "" {
		return c.Dir
	}
	return filepath.Join(s.Dir(), CategoryFolder(c.ID))
}

// ConnectionsFor is the connection ceiling for a host (0 = automatic).
func (s Settings) ConnectionsFor(host string) int {
	for _, h := range s.HostConnections {
		if HostMatches([]string{h.Host}, host) {
			return h.Connections
		}
	}
	return s.Connections
}
