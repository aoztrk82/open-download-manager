// Package hls reads HLS playlists (RFC 8216) for downloading: which variants a
// master playlist offers, and for a media playlist every file to fetch
// (segments, AES-128 keys, fMP4 init sections) plus a copy of the playlist that
// points at the local copies, ready for ffmpeg to decrypt and join.
//
// Only what a download needs: no live streams, no DRM.
package hls

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// IsMaster reports whether text is a master (multivariant) playlist.
func IsMaster(text string) bool { return strings.Contains(text, "#EXT-X-STREAM-INF") }

// Variant is one rendition offered by a master playlist.
type Variant struct {
	URL       string
	Bandwidth int // bits/s; AVERAGE-BANDWIDTH when given, else BANDWIDTH
	Width     int
	Height    int
}

// ParseMaster lists the variants of a master playlist, best first
// (by height, then bandwidth). base resolves relative URIs.
func ParseMaster(text, base string) ([]Variant, error) {
	if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(text, "\ufeff")), "#EXTM3U") {
		return nil, errors.New("not an HLS playlist")
	}
	var out []Variant
	var pending *Variant
	for _, line := range lines(text) {
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			a := attrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			v := Variant{Bandwidth: atoi(a["BANDWIDTH"])}
			if avg := atoi(a["AVERAGE-BANDWIDTH"]); avg > 0 {
				v.Bandwidth = avg
			}
			if w, h, ok := strings.Cut(a["RESOLUTION"], "x"); ok {
				v.Width, v.Height = atoi(w), atoi(h)
			}
			pending = &v
		case strings.HasPrefix(line, "#"):
		case pending != nil:
			u, err := resolve(base, line)
			if err != nil {
				return nil, err
			}
			pending.URL = u
			out = append(out, *pending)
			pending = nil
		}
	}
	if len(out) == 0 {
		return nil, errors.New("master playlist without variants")
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Height != out[j].Height {
			return out[i].Height > out[j].Height
		}
		return out[i].Bandwidth > out[j].Bandwidth
	})
	return out, nil
}

// Item kinds.
const (
	KindSegment = "segment"
	KindKey     = "key"  // AES-128 key
	KindInit    = "init" // EXT-X-MAP initialization section (fMP4)
)

// Item is one file to download.
type Item struct {
	Kind   string
	URL    string
	Offset int64 // EXT-X-BYTERANGE start
	Length int64 // -1 = whole resource
	Local  string
}

// Media is a media playlist prepared for downloading.
type Media struct {
	Items    []Item
	Segments int
	Duration float64 // seconds
	// Encrypted or fMP4: only ffmpeg can turn the pieces into one file. Plain
	// MPEG-TS segments can also simply be concatenated.
	NeedsFFmpeg bool
	// Local is the playlist rewritten to the Item.Local names (same folder).
	Local string
}

// ErrLive is returned for playlists without #EXT-X-ENDLIST.
var ErrLive = errors.New("live streams are not supported (the playlist has no end)")

// ParseMedia prepares a media playlist. base resolves relative URIs.
func ParseMedia(text, base string) (*Media, error) {
	if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(text, "\ufeff")), "#EXTM3U") {
		return nil, errors.New("not an HLS playlist")
	}
	if IsMaster(text) {
		return nil, errors.New("this is a master playlist; pick a variant first")
	}
	m := &Media{}
	var out strings.Builder
	var (
		ended     bool
		segRange  *[2]int64             // pending EXT-X-BYTERANGE (length, offset)
		nextOff   = map[string]int64{}  // byte range without @offset continues the previous one of that URI
		keys      = map[string]string{} // remote key -> local name, so a repeated key is fetched once
		inits     = map[string]string{}
		segExt    = ".ts"
		segIndex  int
		lineCount int
	)
	if strings.Contains(text, "#EXT-X-MAP") {
		segExt = ".m4s"
	}
	write := func(s string) { out.WriteString(s); out.WriteByte('\n') }

	for _, line := range lines(text) {
		lineCount++
		switch {
		case line == "#EXT-X-ENDLIST":
			ended = true
			write(line)
		case strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:"):
			if strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-PLAYLIST-TYPE:")) == "VOD" {
				ended = true
			}
			write(line)
		case strings.HasPrefix(line, "#EXTINF:"):
			d, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			if f, err := strconv.ParseFloat(strings.TrimSpace(d), 64); err == nil {
				m.Duration += f
			}
			write(line)
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			n, o, err := byteRange(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"))
			if err != nil {
				return nil, err
			}
			segRange = &[2]int64{n, o} // the local copy is the range itself: drop the tag
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			a := attrs(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			switch a["METHOD"] {
			case "NONE":
				write(line)
				continue
			case "AES-128":
			default:
				return nil, fmt.Errorf("encryption %q is DRM and cannot be downloaded", a["METHOD"])
			}
			if kf := a["KEYFORMAT"]; kf != "" && kf != "identity" {
				return nil, fmt.Errorf("key format %q is DRM and cannot be downloaded", kf)
			}
			u, err := resolve(base, a["URI"])
			if err != nil {
				return nil, err
			}
			local, ok := keys[u]
			if !ok {
				local = fmt.Sprintf("k%d.key", len(keys))
				keys[u] = local
				m.Items = append(m.Items, Item{Kind: KindKey, URL: u, Length: -1, Local: local})
			}
			m.NeedsFFmpeg = true
			write(replaceURI(line, local))
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			a := attrs(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			u, err := resolve(base, a["URI"])
			if err != nil {
				return nil, err
			}
			it := Item{Kind: KindInit, URL: u, Length: -1}
			if br := a["BYTERANGE"]; br != "" {
				n, o, err := byteRange(br)
				if err != nil {
					return nil, err
				}
				it.Offset, it.Length = o, n
			}
			id := fmt.Sprintf("%s@%d+%d", u, it.Offset, it.Length)
			local, ok := inits[id]
			if !ok {
				local = fmt.Sprintf("init%d.mp4", len(inits))
				inits[id] = local
				it.Local = local
				m.Items = append(m.Items, it)
			}
			m.NeedsFFmpeg = true
			write(fmt.Sprintf(`#EXT-X-MAP:URI="%s"`, local))
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF"), strings.HasPrefix(line, "#EXT-X-I-FRAME"):
			return nil, errors.New("unexpected master playlist tag in a media playlist")
		case strings.HasPrefix(line, "#"):
			write(line)
		default: // a segment URI
			u, err := resolve(base, line)
			if err != nil {
				return nil, err
			}
			it := Item{Kind: KindSegment, URL: u, Length: -1, Local: fmt.Sprintf("s%05d%s", segIndex, segExt)}
			if segRange != nil {
				off := segRange[1]
				if off < 0 {
					off = nextOff[u]
				}
				it.Offset, it.Length = off, segRange[0]
				nextOff[u] = off + segRange[0]
				segRange = nil
			}
			m.Items = append(m.Items, it)
			segIndex++
			write(it.Local)
		}
	}
	if lineCount == 0 || segIndex == 0 {
		return nil, errors.New("playlist has no segments")
	}
	if !ended {
		return nil, ErrLive
	}
	m.Segments = segIndex
	m.Local = out.String()
	return m, nil
}

// lines returns trimmed, non-empty lines.
func lines(text string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(text, "\ufeff")))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// attrs parses an attribute list: KEY=VALUE,KEY="quoted, with commas".
func attrs(s string) map[string]string {
	out := map[string]string{}
	for len(s) > 0 {
		k, rest, ok := strings.Cut(s, "=")
		if !ok {
			break
		}
		k = strings.TrimSpace(k)
		var v string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				v, rest = rest[1:], ""
			} else {
				v, rest = rest[1:end+1], rest[end+2:]
			}
			rest = strings.TrimPrefix(strings.TrimSpace(rest), ",")
		} else {
			v, rest, _ = strings.Cut(rest, ",")
		}
		out[strings.ToUpper(k)] = strings.TrimSpace(v)
		s = rest
	}
	return out
}

// replaceURI swaps the URI="..." attribute of a tag line.
func replaceURI(line, uri string) string {
	i := strings.Index(line, `URI="`)
	if i < 0 {
		return line
	}
	j := strings.IndexByte(line[i+5:], '"')
	if j < 0 {
		return line
	}
	return line[:i+5] + uri + line[i+5+j:]
}

// byteRange parses "<length>[@<offset>]"; offset -1 = continue after the previous range.
func byteRange(s string) (length, offset int64, err error) {
	n, o, hasOff := strings.Cut(strings.TrimSpace(s), "@")
	length, err = strconv.ParseInt(n, 10, 64)
	if err != nil || length <= 0 {
		return 0, 0, fmt.Errorf("bad byte range %q", s)
	}
	offset = -1
	if hasOff {
		if offset, err = strconv.ParseInt(o, 10, 64); err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("bad byte range %q", s)
		}
	}
	return length, offset, nil
}

func resolve(base, ref string) (string, error) {
	if ref == "" {
		return "", errors.New("empty URI in playlist")
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	u := b.ResolveReference(r)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported URI %q in playlist", ref)
	}
	return u.String(), nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
