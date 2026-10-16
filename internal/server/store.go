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
CREATE TABLE IF NOT EXISTS tickets (
	hash       BLOB PRIMARY KEY,
	file_id    TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS reports (
	id         INTEGER PRIMARY KEY,
	file_id    TEXT NOT NULL,
	reason     TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
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

// spendDownload takes one download of a finished, unexpired file, if any are left. The check
// and the count happen in one statement, so two people cannot both take the last one.
func (s *store) spendDownload(ctx context.Context, id string, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE files SET downloads = downloads + 1
		WHERE id = ? AND complete = 1 AND expires_at > ? AND downloads < max_downloads`, id, now.Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *store) insertTicket(ctx context.Context, hash []byte, id string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO tickets (hash, file_id, expires_at) VALUES (?, ?, ?)`, hash, id, expires.Unix())
	return err
}

// ticketValid says whether a ticket (by its hash) still lets its holder fetch the file id.
func (s *store) ticketValid(ctx context.Context, hash []byte, id string, now time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tickets t JOIN files f ON f.id = t.file_id
		WHERE t.hash = ? AND t.file_id = ? AND t.expires_at > ? AND f.expires_at > ?`, hash, id, now.Unix(), now.Unix()).Scan(&n)
	return n == 1, err
}

func (s *store) deleteFile(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id)
	return err
}

func (s *store) addReport(ctx context.Context, id, reason string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reports (file_id, reason, created_at) VALUES (?, ?, ?)`, id, reason, now.Unix())
	return err
}

// finished lists files that may go: expired, never completed within a day, or used up with
// no download still running (every ticket expired).
func (s *store) finished(ctx context.Context, now time.Time, staleUpload time.Duration) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM files f
		WHERE f.expires_at <= ?1
		   OR (f.complete = 0 AND f.created_at <= ?2)
		   OR (f.downloads >= f.max_downloads
		       AND NOT EXISTS (SELECT 1 FROM tickets t WHERE t.file_id = f.id AND t.expires_at > ?1))`,
		now.Unix(), now.Add(-staleUpload).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *store) dropExpiredTickets(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tickets WHERE expires_at <= ?`, now.Unix())
	return err
}

func (s *store) exists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE id = ?`, id).Scan(&n)
	return n == 1, err
}
