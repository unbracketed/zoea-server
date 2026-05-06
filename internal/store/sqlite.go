package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    project_id TEXT,
    external_id TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    pi_pid INTEGER,
    working_dir TEXT,
    created_at TEXT NOT NULL,
    last_active_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT,
    model TEXT,
    usage_json TEXT,
    raw_json TEXT,
    timestamp TEXT NOT NULL,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_external
ON sessions(external_id)
WHERE external_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_session ON session_messages(session_id);
`

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(dsn string) (*SQLiteStore, error) {
	if strings.TrimSpace(dsn) == "" {
		dsn = "./.zoea.db"
	}
	if err := ensureParentDir(dsn); err != nil {
		return nil, err
	}
	// Apply pragmas per connection via DSN so they persist across the connection pool.
	// busy_timeout makes concurrent readers/writers wait up to 5s instead of failing
	// immediately with SQLITE_BUSY.
	dsn = appendPragma(dsn, "_pragma=busy_timeout=5000")
	dsn = appendPragma(dsn, "_pragma=foreign_keys=on")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Limit to a single writer connection: SQLite serializes writes anyway, and
	// holding multiple writer connections can amplify SQLITE_BUSY contention.
	db.SetMaxOpenConns(1)
	return &SQLiteStore{db: db}, nil
}

// ensureParentDir creates the parent directory of a sqlite file DSN. SQLite refuses
// to create missing parent directories — without this, a fresh STORE_DSN pointing at
// a not-yet-created path fails with "unable to open database file".
func ensureParentDir(dsn string) error {
	path := dsn
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimPrefix(path, "file:")
	if path == "" || path == ":memory:" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite dir %q: %w", dir, err)
	}
	return nil
}

func appendPragma(dsn, pragma string) string {
	if strings.Contains(dsn, "?") {
		return dsn + "&" + pragma
	}
	return dsn + "?" + pragma
}

func (s *SQLiteStore) Init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, sqliteSchema); err != nil {
		return fmt.Errorf("init sqlite schema: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		return fmt.Errorf("migrate sqlite schema: %w", err)
	}
	return nil
}

// migrate applies additive schema migrations for existing DBs that pre-date a column.
// Each migration is idempotent: if the column already exists, the ALTER fails with a
// "duplicate column name" error which we tolerate.
func (s *SQLiteStore) migrate(ctx context.Context) error {
	migrations := []string{
		`ALTER TABLE session_messages ADD COLUMN raw_json TEXT`,
	}
	for _, stmt := range migrations {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			if isDuplicateColumn(err) {
				continue
			}
			return fmt.Errorf("apply migration %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) CreateSession(ctx context.Context, rec SessionRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, project_id, external_id, status, pi_pid, working_dir, created_at, last_active_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.UserID, nullableString(rec.ProjectID), nullableString(rec.ExternalID), defaultString(rec.Status, "active"), nullableInt(rec.PiPID), nullableString(rec.WorkingDir), toTS(rec.CreatedAt), toTS(rec.LastActiveAt))
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (SessionRecord, error) {
	var rec SessionRecord
	var projectID, externalID, workingDir sql.NullString
	var piPID sql.NullInt64
	var createdAt, lastActiveAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, project_id, external_id, status, pi_pid, working_dir, created_at, last_active_at
		FROM sessions
		WHERE id = ?
	`, id).Scan(&rec.ID, &rec.UserID, &projectID, &externalID, &rec.Status, &piPID, &workingDir, &createdAt, &lastActiveAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionRecord{}, ErrNotFound
		}
		return SessionRecord{}, err
	}
	rec.ProjectID = nullableStringValue(projectID)
	rec.ExternalID = nullableStringValue(externalID)
	rec.WorkingDir = nullableStringValue(workingDir)
	if piPID.Valid {
		rec.PiPID = int(piPID.Int64)
	}
	rec.CreatedAt = fromTS(createdAt)
	rec.LastActiveAt = fromTS(lastActiveAt)
	return rec, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context, q ListSessionsQuery) ([]SessionRecord, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	clauses := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(q.UserID) != "" {
		clauses = append(clauses, "user_id = ?")
		args = append(args, strings.TrimSpace(q.UserID))
	}
	if strings.TrimSpace(q.ExternalID) != "" {
		clauses = append(clauses, "external_id = ?")
		args = append(args, strings.TrimSpace(q.ExternalID))
	}
	if strings.TrimSpace(q.WorkingDir) != "" {
		clauses = append(clauses, "working_dir = ?")
		args = append(args, strings.TrimSpace(q.WorkingDir))
	}

	query := `
		SELECT id, user_id, project_id, external_id, status, pi_pid, working_dir, created_at, last_active_at
		FROM sessions
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SessionRecord{}
	for rows.Next() {
		var rec SessionRecord
		var projectID, externalID, workingDir sql.NullString
		var piPID sql.NullInt64
		var createdAt, lastActiveAt string
		if err := rows.Scan(&rec.ID, &rec.UserID, &projectID, &externalID, &rec.Status, &piPID, &workingDir, &createdAt, &lastActiveAt); err != nil {
			return nil, err
		}
		rec.ProjectID = nullableStringValue(projectID)
		rec.ExternalID = nullableStringValue(externalID)
		rec.WorkingDir = nullableStringValue(workingDir)
		if piPID.Valid {
			rec.PiPID = int(piPID.Int64)
		}
		rec.CreatedAt = fromTS(createdAt)
		rec.LastActiveAt = fromTS(lastActiveAt)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	aff, err := res.RowsAffected()
	if err == nil && aff == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) UpdateSessionActivity(ctx context.Context, id string, t time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_active_at = ? WHERE id = ?`, toTS(t), id)
	if err != nil {
		return err
	}
	aff, err := res.RowsAffected()
	if err == nil && aff == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ReplaceSessionMessages(ctx context.Context, sessionID string, msgs []MessageRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM session_messages WHERE session_id = ?`, sessionID); err != nil {
		return err
	}

	if len(msgs) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO session_messages (session_id, role, content, model, usage_json, raw_json, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, m := range msgs {
			if _, err := stmt.ExecContext(
				ctx,
				sessionID,
				m.Role,
				nullableString(m.Content),
				nullableString(m.Model),
				nullableString(m.UsageJSON),
				nullableString(m.RawJSON),
				toTS(m.Timestamp),
			); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) CountSessions(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&count)
	return count, err
}

func (s *SQLiteStore) GetMaxSessionID(ctx context.Context) (string, error) {
	var id sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM sessions
		WHERE id GLOB 's_[0-9]*'
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if !id.Valid {
		return "", nil
	}
	return id.String, nil
}

func toTS(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func fromTS(raw string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableStringValue(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "constraint failed")
}

func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name")
}
