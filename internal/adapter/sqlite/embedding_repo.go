package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

var _ domain.EmbeddingRepository = (*Repo)(nil)

const embeddingsDDL = `
CREATE TABLE IF NOT EXISTS embeddings (
  message_rowid INTEGER NOT NULL REFERENCES messages(rowid) ON DELETE CASCADE,
  model TEXT NOT NULL,
  dim INTEGER NOT NULL,
  vector BLOB NOT NULL,
  text_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (message_rowid, model)
);
CREATE INDEX IF NOT EXISTS idx_embeddings_model ON embeddings(model);
`

// embeddableWhere selects message blocks eligible for embedding: user/assistant
// text, or Bash tool_use commands. See app.cleanEmbedUnit for further filtering
// (min length, truncation) applied after fetch.
const embeddableWhere = `((m.kind = 'text' AND m.role IN ('user','assistant')) OR (m.kind = 'tool_use' AND m.tool_name = 'Bash'))`

func (r *Repo) InitEmbeddings(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, embeddingsDDL)
	return err
}

func (r *Repo) UnembeddedUnits(ctx context.Context, model string, f domain.EmbedFilter, limit int) ([]domain.EmbedUnit, error) {
	if limit <= 0 {
		limit = 1000
	}

	q := `
SELECT m.rowid, m.session_id, m.seq, m.role, m.kind, COALESCE(m.tool_name,''), m.text
FROM messages m
JOIN sessions s ON s.id = m.session_id`
	args := []any{}
	if !f.Force {
		q += ` LEFT JOIN embeddings e ON e.message_rowid = m.rowid AND e.model = ?`
		args = append(args, model)
	}
	q += ` WHERE ` + embeddableWhere
	if !f.Force {
		q += ` AND e.message_rowid IS NULL`
	}
	if f.AfterRowID > 0 {
		q += ` AND m.rowid > ?`
		args = append(args, f.AfterRowID)
	}
	q, args = appendEmbedFilter(q, args, f)
	q += ` ORDER BY m.rowid LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var units []domain.EmbedUnit
	for rows.Next() {
		var u domain.EmbedUnit
		var role, kind string
		if err := rows.Scan(&u.MessageRowID, &u.SessionID, &u.Seq, &role, &kind, &u.ToolName, &u.Text); err != nil {
			return nil, err
		}
		u.Role = domain.Role(role)
		u.Kind = domain.BlockKind(kind)
		units = append(units, u)
	}
	return units, rows.Err()
}

func (r *Repo) PutEmbeddings(ctx context.Context, embs []domain.Embedding) error {
	if len(embs) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, e := range embs {
		var text string
		if err := tx.QueryRowContext(ctx, `SELECT text FROM messages WHERE rowid = ?`, e.MessageRowID).Scan(&text); err != nil {
			return fmt.Errorf("lookup message %d: %w", e.MessageRowID, err)
		}
		vec := normalizeVector(e.Vector)
		_, err := tx.ExecContext(ctx,
			`INSERT INTO embeddings(message_rowid, model, dim, vector, text_hash, created_at)
			 VALUES(?,?,?,?,?,?)
			 ON CONFLICT(message_rowid, model) DO UPDATE SET
			   dim=excluded.dim, vector=excluded.vector, text_hash=excluded.text_hash, created_at=excluded.created_at`,
			e.MessageRowID, e.Model, len(vec), encodeVector(vec), textHash(text), now,
		)
		if err != nil {
			return fmt.Errorf("insert embedding %d: %w", e.MessageRowID, err)
		}
	}
	return tx.Commit()
}

func (r *Repo) Nearest(ctx context.Context, model string, q []float32, k int, f domain.EmbedFilter) ([]domain.VectorHit, error) {
	query := `
SELECT e.message_rowid, m.session_id, e.vector
FROM embeddings e
JOIN messages m ON m.rowid = e.message_rowid
JOIN sessions s ON s.id = m.session_id
WHERE e.model = ?`
	args := []any{model}
	query, args = appendEmbedFilter(query, args, f)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	qn := normalizeVector(q)
	var hits []domain.VectorHit
	for rows.Next() {
		var rowID int64
		var sessionID string
		var blob []byte
		if err := rows.Scan(&rowID, &sessionID, &blob); err != nil {
			return nil, err
		}
		hits = append(hits, domain.VectorHit{
			MessageRowID: rowID,
			SessionID:    sessionID,
			Score:        domain.Cosine(qn, decodeVector(blob)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func (r *Repo) EmbeddingsWithContext(ctx context.Context, model string, f domain.EmbedFilter) ([]domain.EmbeddingContext, error) {
	query := `
SELECT e.message_rowid, m.session_id, s.project_path, s.vendor, m.timestamp, m.text, e.vector
FROM embeddings e
JOIN messages m ON m.rowid = e.message_rowid
JOIN sessions s ON s.id = m.session_id
WHERE e.model = ?`
	args := []any{model}
	query, args = appendEmbedFilter(query, args, f)
	query += ` ORDER BY m.session_id, m.seq`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.EmbeddingContext
	for rows.Next() {
		var ec domain.EmbeddingContext
		var vendorStr, tsStr string
		var blob []byte
		if err := rows.Scan(&ec.MessageRowID, &ec.SessionID, &ec.ProjectPath, &vendorStr, &tsStr, &ec.Text, &blob); err != nil {
			return nil, err
		}
		ec.Vendor = parseVendor(vendorStr)
		ec.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
		ec.Vector = decodeVector(blob)
		out = append(out, ec)
	}
	return out, rows.Err()
}

func (r *Repo) MessageByRowID(ctx context.Context, rowid int64) (domain.Message, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT uuid,COALESCE(parent_uuid,''),session_id,role,kind,COALESCE(tool_name,''),source,timestamp,seq,is_sidechain,text
		 FROM messages WHERE rowid = ?`, rowid)
	return scanMessage(row)
}

func (r *Repo) Neighbors(ctx context.Context, sessionID string, seq, radius int) ([]domain.Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT uuid,COALESCE(parent_uuid,''),session_id,role,kind,COALESCE(tool_name,''),source,timestamp,seq,is_sidechain,text
		 FROM messages WHERE session_id = ? AND seq BETWEEN ? AND ? ORDER BY seq`,
		sessionID, seq-radius, seq+radius)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (r *Repo) BashUnits(ctx context.Context, f domain.EmbedFilter) ([]domain.EmbedUnit, error) {
	q := `
SELECT m.rowid, m.session_id, m.seq, m.role, m.kind, COALESCE(m.tool_name,''), m.text
FROM messages m
JOIN sessions s ON s.id = m.session_id
WHERE m.kind = 'tool_use' AND m.tool_name = 'Bash'`
	var args []any
	q, args = appendEmbedFilter(q, args, f)
	q += ` ORDER BY m.session_id, m.seq`

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var units []domain.EmbedUnit
	for rows.Next() {
		var u domain.EmbedUnit
		var role, kind string
		if err := rows.Scan(&u.MessageRowID, &u.SessionID, &u.Seq, &role, &kind, &u.ToolName, &u.Text); err != nil {
			return nil, err
		}
		u.Role = domain.Role(role)
		u.Kind = domain.BlockKind(kind)
		units = append(units, u)
	}
	return units, rows.Err()
}

// appendEmbedFilter extends q/args with the optional EmbedFilter conditions.
// Callers must already have joined `sessions s` and `messages m`.
func appendEmbedFilter(q string, args []any, f domain.EmbedFilter) (string, []any) {
	if f.Vendor != "" {
		q += ` AND s.vendor = ?`
		args = append(args, vendor(f.Vendor))
	}
	if f.ProjectPath != "" {
		q += ` AND s.project_path = ?`
		args = append(args, f.ProjectPath)
	}
	if !f.Since.IsZero() {
		q += ` AND m.timestamp >= ?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	if len(f.Roles) > 0 {
		placeholders := make([]string, len(f.Roles))
		for i, role := range f.Roles {
			placeholders[i] = "?"
			args = append(args, string(role))
		}
		q += ` AND m.role IN (` + strings.Join(placeholders, ",") + `)`
	}
	if len(f.Kinds) > 0 {
		placeholders := make([]string, len(f.Kinds))
		for i, k := range f.Kinds {
			placeholders[i] = "?"
			args = append(args, string(k))
		}
		q += ` AND m.kind IN (` + strings.Join(placeholders, ",") + `)`
	}
	return q, args
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessage(row rowScanner) (domain.Message, error) {
	var m domain.Message
	var tsStr string
	var isSidechain int
	err := row.Scan(&m.UUID, &m.ParentUUID, &m.SessionID, &m.Role, &m.Kind, &m.ToolName, &m.Source, &tsStr, &m.Sequence, &isSidechain, &m.Text)
	if err != nil {
		return m, err
	}
	m.IsSidechain = isSidechain != 0
	m.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
	return m, nil
}

func encodeVector(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func normalizeVector(v []float32) []float32 {
	var sumSq float64
	for _, f := range v {
		sumSq += float64(f) * float64(f)
	}
	if sumSq == 0 {
		return v
	}
	norm := float32(math.Sqrt(sumSq))
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = f / norm
	}
	return out
}

func textHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
