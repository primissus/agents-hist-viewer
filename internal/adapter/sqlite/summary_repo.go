package sqlite

import (
	"context"
	"database/sql"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

var _ domain.SummaryRepository = (*Repo)(nil)

const summariesDDL = `
CREATE TABLE IF NOT EXISTS summaries (
  session_id TEXT NOT NULL,   -- no FK on purpose: ReplaceSession deletes+reinserts sessions, an FK cascade would wipe this table on every re-index
  model TEXT NOT NULL,
  source_hash TEXT NOT NULL,
  summary TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (session_id, model)
);
`

// InitSummaries creates the summaries table if missing and sweeps orphaned
// rows whose session was deleted (e.g. by a re-index).
func (r *Repo) InitSummaries(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, summariesDDL); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM summaries WHERE session_id NOT IN (SELECT id FROM sessions)`)
	return err
}

func (r *Repo) GetSummary(ctx context.Context, sessionID, model string) (domain.Summary, bool, error) {
	var s domain.Summary
	var createdAt string
	err := r.db.QueryRowContext(ctx,
		`SELECT session_id, model, source_hash, summary, created_at FROM summaries WHERE session_id = ? AND model = ?`,
		sessionID, model,
	).Scan(&s.SessionID, &s.Model, &s.SourceHash, &s.Text, &createdAt)
	if err == sql.ErrNoRows {
		return domain.Summary{}, false, nil
	}
	if err != nil {
		return domain.Summary{}, false, err
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return s, true, nil
}

func (r *Repo) PutSummary(ctx context.Context, s domain.Summary) error {
	createdAt := s.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO summaries(session_id, model, source_hash, summary, created_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(session_id, model) DO UPDATE SET
		   source_hash=excluded.source_hash, summary=excluded.summary, created_at=excluded.created_at`,
		s.SessionID, s.Model, s.SourceHash, s.Text, createdAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}
