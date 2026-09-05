package transcript

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-code-hist-viewer/internal/domain"

	_ "modernc.org/sqlite"
)

// Source implements domain.TranscriptSource over the OpenCode SQLite database.
// It opens the DB read-only and never writes or migrates.
type Source struct {
	dbPath string
	opts   domain.IndexOptions

	once sync.Once
	db   *sql.DB
	err  error
}

func NewSource(dbPath string, opts domain.IndexOptions) *Source {
	return &Source{dbPath: dbPath, opts: opts}
}

// partData is the JSON shape stored in part.data.
type partData struct {
	Type     string     `json:"type"`
	Text     string     `json:"text"`
	Tool     string     `json:"tool"`
	CallID   string     `json:"callID"`
	State    *toolState `json:"state"`
	Files    []string   `json:"files"`
	Filename string     `json:"filename"`
	URL      string     `json:"url"`
}

type toolState struct {
	Status   string          `json:"status"`
	Input    json.RawMessage `json:"input"`
	Output   json.RawMessage `json:"output"`
	Metadata *toolMetadata   `json:"metadata"`
}

type toolMetadata struct {
	Output string `json:"output"`
}

func SessionID(id string) string { return "opencode:" + id }

func (s *Source) open() (*sql.DB, error) {
	s.once.Do(func() {
		if _, err := os.Stat(s.dbPath); err != nil {
			s.err = err
			return
		}
		db, err := sql.Open("sqlite", readOnlyDSN(s.dbPath))
		if err != nil {
			s.err = err
			return
		}
		if err := db.Ping(); err != nil {
			db.Close()
			s.err = err
			return
		}
		s.db = db
	})
	return s.db, s.err
}

func (s *Source) Sessions(ctx context.Context) ([]domain.Session, error) {
	db, err := s.open()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
SELECT s.id, s.directory, s.title, s.time_created, s.time_updated,
       COALESCE((SELECT COUNT(*) FROM message m WHERE m.session_id = s.id), 0)
FROM session s
WHERE s.parent_id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []domain.Session
	for rows.Next() {
		var id, directory, title string
		var timeCreated, timeUpdated, msgCount int64
		if err := rows.Scan(&id, &directory, &title, &timeCreated, &timeUpdated, &msgCount); err != nil {
			return nil, err
		}
		started := millisToTime(timeCreated)
		ended := millisToTime(timeUpdated)
		if started.IsZero() {
			started = ended
		}
		if ended.IsZero() {
			ended = started
		}
		if title == "" {
			title = s.deriveTitle(ctx, db, id)
		}
		sessions = append(sessions, domain.Session{
			ID:            SessionID(id),
			Title:         title,
			ProjectPath:   directory,
			StartedAt:     started,
			EndedAt:       ended,
			MessageCount:  int(msgCount),
			HasTranscript: true,
			RecordKind:    domain.RecordChat,
			Vendor:        domain.VendorOpencode,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (s *Source) Messages(ctx context.Context, sessionID string, yield func(domain.Message) error) error {
	db, err := s.open()
	if err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return err
	}
	id := strings.TrimPrefix(sessionID, "opencode:")

	ids := []string{id}
	childRows, err := db.QueryContext(ctx, `SELECT id FROM session WHERE parent_id = ? ORDER BY time_created`, id)
	if err != nil {
		return err
	}
	for childRows.Next() {
		var childID string
		if err := childRows.Scan(&childID); err != nil {
			childRows.Close()
			return err
		}
		ids = append(ids, childID)
	}
	childRows.Close()
	if err := childRows.Err(); err != nil {
		return err
	}

	seq := 0
	for i, sid := range ids {
		isSidechain := i > 0
		if err := s.streamSession(ctx, db, sid, sessionID, isSidechain, &seq, yield); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) TranscriptFingerprint(ctx context.Context, sessionID string) (domain.TranscriptFingerprint, error) {
	if err := ctx.Err(); err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	db, err := s.open()
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	id := strings.TrimPrefix(sessionID, "opencode:")

	var timeUpdated int64
	var msgCount int64
	err = db.QueryRowContext(ctx, `
SELECT s.time_updated,
       (SELECT COUNT(*) FROM message m WHERE m.session_id = s.id)
       + (SELECT COUNT(*) FROM message m JOIN session c ON c.id = m.session_id WHERE c.parent_id = s.id)
FROM session s WHERE s.id = ?`, id).Scan(&timeUpdated, &msgCount)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.TranscriptFingerprint{}, os.ErrNotExist
		}
		return domain.TranscriptFingerprint{}, err
	}

	return domain.TranscriptFingerprint{
		Path: SessionID(id),
		Hash: fmt.Sprintf("opencode-session-meta-v1:%d-%d", timeUpdated, msgCount),
	}, nil
}

func (s *Source) streamSession(ctx context.Context, db *sql.DB, sid, sessionID string, isSidechain bool, seq *int, yield func(domain.Message) error) error {
	rows, err := db.QueryContext(ctx, `
SELECT m.id, m.data, p.id, p.data, p.time_created
FROM message m
LEFT JOIN part p ON p.message_id = m.id
WHERE m.session_id = ?
ORDER BY m.time_created, m.id, p.time_created, p.id`, sid)
	if err != nil {
		return err
	}
	defer rows.Close()

	var curMsgID string
	var curRole domain.Role
	for rows.Next() {
		var msgID, msgData, partID, partData string
		var partTime int64
		var msgPartID, msgPartData sql.NullString
		var partTimeNull sql.NullInt64
		if err := rows.Scan(&msgID, &msgData, &msgPartID, &msgPartData, &partTimeNull); err != nil {
			return err
		}
		partID = msgPartID.String
		partData = msgPartData.String
		partTime = partTimeNull.Int64

		if msgID != curMsgID {
			curMsgID = msgID
			curRole = messageRole(msgData)
		}
		if partID == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.emitPart(ctx, partID, partData, partTime, sessionID, curRole, isSidechain, seq, yield); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Source) emitPart(ctx context.Context, partID, raw string, partTime int64, sessionID string, role domain.Role, isSidechain bool, seq *int, yield func(domain.Message) error) error {
	var p partData
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil // skip bad lines
	}
	ts := millisToTime(partTime)
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	emit := func(uuidSuffix string, kind domain.BlockKind, text, toolName string, msgRole domain.Role) error {
		if !s.opts.ShouldIndex(msgRole, kind) {
			return nil
		}
		shrunk, ok := domain.Shrink(kind, text, s.opts.Shrink)
		if !ok {
			return nil
		}
		m := domain.Message{
			UUID:        partID + uuidSuffix,
			SessionID:   sessionID,
			Role:        msgRole,
			Kind:        kind,
			Text:        shrunk,
			ToolName:    toolName,
			Timestamp:   ts,
			Sequence:    *seq,
			IsSidechain: isSidechain,
			Source:      domain.SourceTranscript,
		}
		if err := yield(m); err != nil {
			return err
		}
		*seq++
		return nil
	}

	switch p.Type {
	case "text":
		return emit("", domain.KindText, p.Text, "", role)
	case "reasoning":
		return emit("", domain.KindThinking, p.Text, "", domain.RoleAssistant)
	case "tool":
		input := rawToString(p.State.Input)
		if err := emit("-use", domain.KindToolUse, input, p.Tool, domain.RoleAssistant); err != nil {
			return err
		}
		output := toolOutputText(p)
		if output != "" {
			return emit("-result", domain.KindToolResult, output, p.Tool, domain.RoleAssistant)
		}
		return nil
	case "patch":
		text := p.Tool
		if len(p.Files) > 0 {
			text = strings.Join(p.Files, "\n")
		}
		return emit("", domain.KindToolResult, text, "patch", domain.RoleAssistant)
	case "file":
		name := p.Filename
		if name == "" {
			name = p.URL
		}
		return emit("", domain.KindToolResult, name, "file", domain.RoleAssistant)
	default:
		return nil // step-start, step-finish, compaction, unknown
	}
}

func (s *Source) deriveTitle(ctx context.Context, db *sql.DB, sessionID string) string {
	var data string
	err := db.QueryRowContext(ctx, `
SELECT p.data
FROM part p
JOIN message m ON m.id = p.message_id
WHERE p.session_id = ? AND json_extract(p.data, '$.type') = 'text'
  AND json_extract(m.data, '$.role') = 'user'
ORDER BY p.time_created LIMIT 1`, sessionID).Scan(&data)
	if err != nil {
		return ""
	}
	var p partData
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return ""
	}
	return domain.DeriveTitle(p.Text, 60)
}

func readOnlyDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "file:" + abs + "?mode=ro&_pragma=busy_timeout(5000)"
}

func millisToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func messageRole(data string) domain.Role {
	var m struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return domain.RoleAssistant
	}
	if m.Role == string(domain.RoleUser) {
		return domain.RoleUser
	}
	return domain.RoleAssistant
}

func toolOutputText(p partData) string {
	if p.State == nil {
		return ""
	}
	out := rawToString(p.State.Output)
	if out == "" && p.State.Metadata != nil {
		out = p.State.Metadata.Output
	}
	return out
}

func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return string(raw)
		}
		return s
	}
	return string(raw)
}
