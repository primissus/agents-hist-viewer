package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"claude-code-hist-viewer/internal/domain"

	_ "modernc.org/sqlite"
)

var _ domain.SearchRepository = (*Repo)(nil)

type Repo struct{ db *sql.DB }

func Open(path string) (*Repo, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Repo{db: db}, nil
}

func (r *Repo) Close() error { return r.db.Close() }

func (r *Repo) Init(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, ddl); err != nil {
		return err
	}
	// Migration: add columns to existing DBs (ignore error if already present).
	r.db.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN text TEXT NOT NULL DEFAULT ''`)
	r.db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN record_kind TEXT NOT NULL DEFAULT 'chat'`)
	r.db.ExecContext(ctx, `UPDATE sessions SET record_kind='plan' WHERE id LIKE 'plan:%' OR id LIKE 'cursor-plan:%'`)
	r.db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN vendor TEXT NOT NULL DEFAULT 'claude'`)
	r.db.ExecContext(ctx, `UPDATE sessions SET vendor='cursor' WHERE id LIKE 'cursor:%' OR id LIKE 'cursor-plan:%'`)
	if _, err := r.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_sessions_filters ON sessions(vendor, record_kind, project_path)`); err != nil {
		return err
	}
	return nil
}

func (r *Repo) ReplaceSession(ctx context.Context, s domain.Session, msgs []domain.Message) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Collect rowids to delete from fts before deleting messages.
	rows, err := tx.QueryContext(ctx, `SELECT rowid FROM messages WHERE session_id = ?`, s.ID)
	if err != nil {
		return err
	}
	var rowids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		rowids = append(rowids, id)
	}
	rows.Close()

	for _, rid := range rowids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM messages_fts WHERE rowid = ?`, rid); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, s.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, s.ID); err != nil {
		return err
	}

	hasTranscript := 0
	if s.HasTranscript {
		hasTranscript = 1
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO sessions(id,title,project_path,git_branch,started_at,ended_at,message_count,file_path,has_transcript,record_kind,vendor)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Title, s.ProjectPath, s.GitBranch,
		s.StartedAt.UTC().Format(time.RFC3339Nano),
		s.EndedAt.UTC().Format(time.RFC3339Nano),
		s.MessageCount, s.FilePath, hasTranscript, recordKind(s.RecordKind), vendor(s.Vendor),
	)
	if err != nil {
		return err
	}

	for _, m := range msgs {
		isSidechain := 0
		if m.IsSidechain {
			isSidechain = 1
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO messages(uuid,parent_uuid,session_id,role,kind,tool_name,source,timestamp,seq,is_sidechain,text)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			m.UUID, nullStr(m.ParentUUID), m.SessionID,
			string(m.Role), string(m.Kind), nullStr(m.ToolName),
			string(m.Source),
			m.Timestamp.UTC().Format(time.RFC3339Nano),
			m.Sequence, isSidechain, m.Text,
		)
		if err != nil {
			return fmt.Errorf("insert message %s: %w", m.UUID, err)
		}
		rowid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages_fts(rowid,text) VALUES(?,?)`, rowid, m.Text); err != nil {
			return fmt.Errorf("insert fts %d: %w", rowid, err)
		}
	}

	return tx.Commit()
}

func (r *Repo) Search(ctx context.Context, query string, limit int) ([]domain.SearchHit, error) {
	const q = `
SELECT m.uuid, m.session_id, m.role, m.kind, m.timestamp,
       s.title, s.project_path, s.file_path, s.has_transcript, s.started_at, s.record_kind, s.vendor,
       snippet(messages_fts, 0, '[', ']', ' … ', 12) AS snip,
       bm25(messages_fts) AS score
FROM messages_fts
JOIN messages m ON m.rowid = messages_fts.rowid
JOIN sessions s ON s.id = m.session_id
WHERE messages_fts MATCH ?
ORDER BY score
LIMIT ?`

	// Fetch a large pool so dedup by session still yields `limit` unique sessions.
	fetchLimit := limit * 10
	if fetchLimit < 500 {
		fetchLimit = 500
	}
	rows, err := r.db.QueryContext(ctx, q, query, fetchLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var hits []domain.SearchHit
	for rows.Next() {
		var h domain.SearchHit
		var tsStr, startedAtStr, recordKindStr, vendorStr string
		var hasTranscript int
		if err := rows.Scan(
			&h.MessageUUID, &h.SessionID, &h.Role, &h.Kind, &tsStr,
			&h.SessionTitle, &h.ProjectPath, &h.FilePath, &hasTranscript, &startedAtStr, &recordKindStr, &vendorStr,
			&h.Snippet, &h.Score,
		); err != nil {
			return nil, err
		}
		if seen[h.SessionID] {
			continue
		}
		seen[h.SessionID] = true
		h.HasTranscript = hasTranscript != 0
		h.RecordKind = parseRecordKind(recordKindStr)
		h.Vendor = parseVendor(vendorStr)
		h.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
		h.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAtStr)
		hits = append(hits, h)
		if len(hits) >= limit {
			break
		}
	}
	return hits, rows.Err()
}

func (r *Repo) RecentSessions(ctx context.Context, q domain.RecentQuery) ([]domain.SearchHit, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	query := `
SELECT id, title, project_path, file_path, has_transcript, started_at, ended_at, record_kind, vendor
FROM sessions
WHERE 1=1`
	var args []any
	if q.RecordKind != "" {
		query += ` AND record_kind = ?`
		args = append(args, recordKind(q.RecordKind))
	}
	if q.ProjectPath != "" {
		query += ` AND project_path = ?`
		args = append(args, q.ProjectPath)
	}
	if q.Vendor != "" {
		query += ` AND vendor = ?`
		args = append(args, vendor(q.Vendor))
	}
	query += ` ORDER BY ended_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []domain.SearchHit
	for rows.Next() {
		var h domain.SearchHit
		var startedAtStr, endedAtStr, recordKindStr, vendorStr string
		var hasTranscript int
		if err := rows.Scan(
			&h.SessionID, &h.SessionTitle, &h.ProjectPath, &h.FilePath,
			&hasTranscript, &startedAtStr, &endedAtStr, &recordKindStr, &vendorStr,
		); err != nil {
			return nil, err
		}
		h.HasTranscript = hasTranscript != 0
		h.RecordKind = parseRecordKind(recordKindStr)
		h.Vendor = parseVendor(vendorStr)
		h.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAtStr)
		h.Timestamp, _ = time.Parse(time.RFC3339Nano, endedAtStr)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (r *Repo) SessionByID(ctx context.Context, id string) (domain.SessionDetail, error) {
	var detail domain.SessionDetail

	row := r.db.QueryRowContext(ctx,
		`SELECT id,title,project_path,git_branch,started_at,ended_at,message_count,file_path,has_transcript,record_kind,vendor
		 FROM sessions WHERE id = ?`, id)
	var startedAt, endedAt, recordKindStr, vendorStr string
	var hasTranscript int
	err := row.Scan(
		&detail.Session.ID, &detail.Session.Title,
		&detail.Session.ProjectPath, &detail.Session.GitBranch,
		&startedAt, &endedAt,
		&detail.Session.MessageCount, &detail.Session.FilePath, &hasTranscript, &recordKindStr, &vendorStr,
	)
	if err != nil {
		return detail, err
	}
	detail.Session.HasTranscript = hasTranscript != 0
	detail.Session.RecordKind = parseRecordKind(recordKindStr)
	detail.Session.Vendor = parseVendor(vendorStr)
	detail.Session.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
	detail.Session.EndedAt, _ = time.Parse(time.RFC3339Nano, endedAt)

	rows, err := r.db.QueryContext(ctx,
		`SELECT uuid,COALESCE(parent_uuid,''),session_id,role,kind,COALESCE(tool_name,''),
		        source,timestamp,seq,is_sidechain,text
		 FROM messages WHERE session_id = ? ORDER BY seq`, id)
	if err != nil {
		return detail, err
	}
	defer rows.Close()

	for rows.Next() {
		var m domain.Message
		var tsStr string
		var isSidechain int
		if err := rows.Scan(
			&m.UUID, &m.ParentUUID, &m.SessionID,
			&m.Role, &m.Kind, &m.ToolName,
			&m.Source, &tsStr, &m.Sequence, &isSidechain, &m.Text,
		); err != nil {
			return detail, err
		}
		m.IsSidechain = isSidechain != 0
		m.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
		detail.Messages = append(detail.Messages, m)
	}
	return detail, rows.Err()
}

func (r *Repo) GetFileHash(ctx context.Context, path string) (string, bool, error) {
	var hash string
	err := r.db.QueryRowContext(ctx, `SELECT content_hash FROM file_hashes WHERE path = ?`, path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}

func (r *Repo) SetFileHash(ctx context.Context, path, sessionID, hash string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO file_hashes(path,session_id,content_hash,updated_at)
		 VALUES(?,?,?,?)
		 ON CONFLICT(path) DO UPDATE SET
		   session_id=excluded.session_id,
		   content_hash=excluded.content_hash,
		   updated_at=excluded.updated_at`,
		path, sessionID, hash, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func recordKind(k domain.RecordKind) string {
	if k == domain.RecordPlan {
		return string(domain.RecordPlan)
	}
	return string(domain.RecordChat)
}

func parseRecordKind(s string) domain.RecordKind {
	if domain.RecordKind(s) == domain.RecordPlan {
		return domain.RecordPlan
	}
	return domain.RecordChat
}

func vendor(v domain.Vendor) string {
	switch v {
	case domain.VendorCursor:
		return string(domain.VendorCursor)
	case domain.VendorClaudeDesktop:
		return string(domain.VendorClaudeDesktop)
	default:
		return string(domain.VendorClaude)
	}
}

func parseVendor(s string) domain.Vendor {
	switch domain.Vendor(s) {
	case domain.VendorCursor:
		return domain.VendorCursor
	case domain.VendorClaudeDesktop:
		return domain.VendorClaudeDesktop
	default:
		return domain.VendorClaude
	}
}
