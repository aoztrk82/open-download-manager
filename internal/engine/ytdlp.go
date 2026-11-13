package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/ytdlp"
)

// runYtdlp hands the download to yt-dlp (YouTube and other sites it knows).
// Pause/shutdown stop it; yt-dlp resumes its own .part files next time.
func runYtdlp(ctx context.Context, st *State, o Options, onProgress func(State)) error {
	path, err := ytdlp.Find()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return err
	}
	if st.Tool == nil {
		st.Tool = &ToolState{}
	}
	if st.Filename == "" {
		st.Filename = o.Filename
	}
	st.AcceptRanges = true
	if st.Size == 0 {
		st.Size = -1
	}

	var mu sync.Mutex
	snapshot := func() State {
		mu.Lock()
		defer mu.Unlock()
		return st.Clone()
	}
	var last time.Time
	file, err := ytdlp.Download(ctx, path, ytdlp.Options{
		URL: o.Req.URL, Format: o.Req.Media.Format, Container: o.Req.Media.Container,
		Dir: o.Dir, Filename: o.Filename,
	}, func(p ytdlp.Progress) {
		mu.Lock()
		st.Tool.Bytes = p.Downloaded
		if p.Total > 0 {
			st.Size = p.Total
		}
		if p.Part != "" {
			st.Path = p.Part // what "delete file" removes while unfinished
		}
		mu.Unlock()
		if onProgress != nil && (time.Since(last) >= 500*time.Millisecond || p.Merging) {
			last = time.Now()
			onProgress(snapshot())
		}
	})
	if onProgress != nil {
		onProgress(snapshot())
	}
	if err != nil {
		return err
	}
	fi, err := os.Stat(file)
	if err != nil {
		return err
	}
	st.Path, st.Filename, st.Size, st.Tool.Bytes = file, filepath.Base(file), fi.Size(), fi.Size()
	return nil
}
