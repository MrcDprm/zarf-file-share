package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite" // pure Go: builds for any platform without a C compiler
)

// The database holds only what the server needs to do its job: sizes, limits, times and
// hashes of tokens. No file contents, names or keys, and no network addresses.
const schema = `
CREATE TABLE IF NOT EXISTS files (
	id                TEXT PRIMARY KEY,
	size              INTEGER NOT NULL,
	part_size         INTEGER NOT NULL,
	upload_token_hash BLOB NOT NULL,
	delete_token_hash BLOB NOT NULL,
	dl_auth_hash      BLOB NOT NULL,
	max_downloads     INTEGER NOT NULL,
	downloads         INTEGER NOT NULL DEFAULT 0,
	complete          INTEGER NOT NULL DEFAULT 0,
	created_at        INTEGER NOT NULL,
	expires_at        INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS parts (
	file_id TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
	n       INTEGER NOT NULL,
	size    INTEGER NOT NULL,
	PRIMARY KEY (file_id, n)
);
CREATE INDEX IF NOT EXISTS files_expires ON files(expires_at);
`

var errNotFound = errors.New("not found")

type fileRow struct {
	ID              string
	Size, PartSize  int64
	UploadTokenHash []byte
	DeleteTokenHash []byte
	DLAuthHash      []byte
	MaxDownloads    int
	Downloads       int
	Complete        bool
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

func (f *fileRow) parts() int64 { return (f.Size + f.PartSize - 1) / f.PartSize }

// partLen is the exact length of part n; only the last part may be shorter.
func (f *fileRow) partLen(n int64) int64 { return min(f.PartSize, f.Size-n*f.PartSize) }

type store struct{ db *sql.DB }

func openStore(path string) (*store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// One connection: SQLite has one writer anyway, and this rules out "database is locked".
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &store{db: db}, nil
}

func (s *store) close() error { return s.db.Close() }

func (s *store) insertFile(ctx context.Context, f *fileRow) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO files
		(id, size, part_size, upload_token_hash, delete_token_hash, dl_auth_hash, max_downloads, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.Size, f.PartSize, f.UploadTokenHash, f.DeleteTokenHash, f.DLAuthHash, f.MaxDownloads, f.CreatedAt.Unix(), f.ExpiresAt.Unix())
	return err
}

func (s *store) file(ctx context.Context, id string) (*fileRow, error) {
	f := &fileRow{ID: id}
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT size, part_size, upload_token_hash, delete_token_hash, dl_auth_hash,
		max_downloads, downloads, complete, created_at, expires_at FROM files WHERE id = ?`, id).
		Scan(&f.Size, &f.PartSize, &f.UploadTokenHash, &f.DeleteTokenHash, &f.DLAuthHash,
			&f.MaxDownloads, &f.Downloads, &f.Complete, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	f.CreatedAt, f.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
	return f, err
}

// reservedBytes is the space promised to files that still exist: the storage quota counts it.
func (s *store) reservedBytes(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT SUM(size) FROM files`).Scan(&n)
	return n.Int64, err
}

func (s *store) recordPart(ctx context.Context, id string, n, size int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO parts (file_id, n, size) VALUES (?, ?, ?)
		ON CONFLICT (file_id, n) DO UPDATE SET size = excluded.size`, id, n, size)
	return err
}

func (s *store) receivedParts(ctx context.Context, id string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n FROM parts WHERE file_id = ? ORDER BY n`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := []int64{}
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		parts = append(parts, n)
	}
	return parts, rows.Err()
}

func (s *store) markComplete(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE files SET complete = 1 WHERE id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM parts WHERE file_id = ?`, id)
	return err
}

// hashToken is how tokens are kept: whoever reads the database cannot use them.
func hashToken(token []byte) []byte {
	sum := sha256.Sum256(token)
	return sum[:]
}

func tokenMatches(token, hash []byte) bool {
	return len(hash) == sha256.Size && subtle.ConstantTimeCompare(hashToken(token), hash) == 1
}
