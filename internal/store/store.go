// Package store persists downloads in SQLite (pure Go driver, no cgo).
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/proto"
)

type Status string

const (
	Queued    Status = "queued"
	Active    Status = "active"
	Paused    Status = "paused"
	Completed Status = "completed"
	Failed    Status = "failed"
)

// Download is the persisted/API representation of one download.
type Download struct {
	ID         string        `json:"id"`
	Req        proto.Request `json:"request"`
	Dir        string        `json:"dir"`
	Filename   string        `json:"filename"`
	Path       string        `json:"path,omitempty"`
	Status     Status        `json:"status"`
	Size       int64         `json:"size"`
	Downloaded int64         `json:"downloaded"`
	Error      string        `json:"error,omitempty"`
	// Queue is "" (starts as soon as a slot is free), QueueMain (one after
	// another) or QueueLater (waits until the later queue is started by hand or
	// by its schedule). QPos orders a queue; smaller runs first.
	Queue     string       `json:"queue,omitempty"`
	QPos      int64        `json:"qpos,omitempty"`
	State     engine.State `json:"-"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

// Queues a download can be in.
const (
	QueueMain  = "main"
	QueueLater = "later"
)

var ErrNotFound = errors.New("download not found")

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS downloads (
  id TEXT PRIMARY KEY,
  req TEXT NOT NULL,
  dir TEXT NOT NULL,
  filename TEXT NOT NULL,
  status TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  downloaded INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`

// Open opens (creating if needed) the database at path. Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // serialize; plenty for this workload and avoids SQLITE_BUSY
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	// Columns added after the first release: older databases get them here.
	for _, col := range []string{"queue TEXT NOT NULL DEFAULT ''", "qpos INTEGER NOT NULL DEFAULT 0"} {
		if _, err := db.Exec(`ALTER TABLE downloads ADD COLUMN ` + col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Save(d *Download) error {
	req, _ := json.Marshal(d.Req)
	st, _ := json.Marshal(d.State)
	now := time.Now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	_, err := s.db.Exec(`
INSERT INTO downloads (id,req,dir,filename,status,size,downloaded,error,state,created_at,updated_at,queue,qpos)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET req=excluded.req, dir=excluded.dir, filename=excluded.filename,
  status=excluded.status, size=excluded.size, downloaded=excluded.downloaded, error=excluded.error,
  state=excluded.state, updated_at=excluded.updated_at`,
		d.ID, string(req), d.Dir, d.Filename, string(d.Status), d.Size, d.Downloaded, d.Error,
		string(st), d.CreatedAt.UnixMilli(), d.UpdatedAt.UnixMilli(), d.Queue, d.QPos)
	return err
}

const cols = `id,req,dir,filename,status,size,downloaded,error,state,created_at,updated_at,queue,qpos`

func scan(sc interface{ Scan(...any) error }) (*Download, error) {
	var d Download
	var req, st, status string
	var created, updated int64
	if err := sc.Scan(&d.ID, &req, &d.Dir, &d.Filename, &status, &d.Size, &d.Downloaded,
		&d.Error, &st, &created, &updated, &d.Queue, &d.QPos); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Status = Status(status)
	_ = json.Unmarshal([]byte(req), &d.Req)
	_ = json.Unmarshal([]byte(st), &d.State)
	d.Path = d.State.Path
	d.CreatedAt, d.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
	return &d, nil
}

// SetQueue changes only a download's queue and position. Save leaves both
// alone on existing rows: a running download saves its own copy every second,
// which must not undo a move made meanwhile.
func (s *Store) SetQueue(id, queue string, pos int64) error {
	r, err := s.db.Exec(`UPDATE downloads SET queue=?, qpos=? WHERE id=?`, queue, pos, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Get(id string) (*Download, error) {
	return scan(s.db.QueryRow(`SELECT `+cols+` FROM downloads WHERE id=?`, id))
}

func (s *Store) List() ([]*Download, error) {
	rows, err := s.db.Query(`SELECT ` + cols + ` FROM downloads ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Download
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM downloads WHERE id=?`, id)
	return err
}
