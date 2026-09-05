package transcript_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"claude-code-hist-viewer/internal/adapter/opencode/transcript"
	"claude-code-hist-viewer/internal/domain"

	_ "modernc.org/sqlite"
)

const opencodeSchema = `
CREATE TABLE session (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    parent_id TEXT,
    directory TEXT NOT NULL,
    title TEXT NOT NULL,
    time_created INTEGER NOT NULL,
    time_updated INTEGER NOT NULL
);
CREATE TABLE message (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    time_created INTEGER NOT NULL,
    time_updated INTEGER NOT NULL,
    data TEXT NOT NULL
);
CREATE TABLE part (
    id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    time_created INTEGER NOT NULL,
    time_updated INTEGER NOT NULL,
    data TEXT NOT NULL
);`

func newTestDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "opencode.db")
}

func openOpenCodeAt(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(opencodeSchema); err != nil {
		t.Fatal(err)
	}
	return db
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

func seedOpenCodeData(t *testing.T, db *sql.DB) {
	t.Helper()
	exec(t, db, `INSERT INTO session(id, project_id, directory, title, time_created, time_updated) VALUES('ses_root','p1','/Users/me/src/app','Root title',1700000000000,1700000005000)`)
	exec(t, db, `INSERT INTO session(id, project_id, parent_id, directory, title, time_created, time_updated) VALUES('ses_child','p1','ses_root','/Users/me/src/app','Child',1700000001000,1700000004000)`)

	exec(t, db, `INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES('msg1','ses_root',1700000000100,1700000000100,'{"role":"user"}')`)
	exec(t, db, `INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES('prt1','msg1','ses_root',1700000000100,1700000000100,'{"type":"text","text":"hello opencode unique phrase"}')`)

	exec(t, db, `INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES('msg2','ses_root',1700000000200,1700000000200,'{"role":"assistant"}')`)
	exec(t, db, `INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES('prt2','msg2','ses_root',1700000000200,1700000000200,'{"type":"reasoning","text":"thinking text"}')`)

	exec(t, db, `INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES('msg3','ses_root',1700000000300,1700000000300,'{"role":"assistant"}')`)
	exec(t, db, `INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES('prt3','msg3','ses_root',1700000000300,1700000000300,'{"type":"tool","tool":"read","callID":"call_1","state":{"status":"completed","input":{"filePath":"/tmp/x"},"output":"file contents","metadata":{"truncated":false}}}')`)

	exec(t, db, `INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES('msg4','ses_root',1700000000400,1700000000400,'{"role":"assistant"}')`)
	exec(t, db, `INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES('prt4','msg4','ses_root',1700000000400,1700000000400,'{"type":"patch","hash":"h","files":["/tmp/a.go","/tmp/b.go"]}')`)

	exec(t, db, `INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES('msg5','ses_child',1700000000500,1700000000500,'{"role":"assistant"}')`)
	exec(t, db, `INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES('prt5','msg5','ses_child',1700000000500,1700000000500,'{"type":"text","text":"sidechain text"}')`)
}

func TestMissingDBYieldsNoSessions(t *testing.T) {
	src := transcript.NewSource(filepath.Join(t.TempDir(), "nope.db"), domain.DefaultIndexOptions)
	sessions, err := src.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions for missing DB, got %d", len(sessions))
	}
}

func TestSessionsListsRootOnly(t *testing.T) {
	path := newTestDBPath(t)
	db := openOpenCodeAt(t, path)
	seedOpenCodeData(t, db)
	db.Close()

	src := transcript.NewSource(path, domain.DefaultIndexOptions)
	sessions, err := src.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 root session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.ID != "opencode:ses_root" {
		t.Errorf("id: %q", s.ID)
	}
	if s.Vendor != domain.VendorOpencode {
		t.Errorf("vendor: %q", s.Vendor)
	}
	if s.ProjectPath != "/Users/me/src/app" {
		t.Errorf("project path: %q", s.ProjectPath)
	}
	if s.Title != "Root title" {
		t.Errorf("title: %q", s.Title)
	}
}

func TestTitleFallsBackToFirstUserText(t *testing.T) {
	path := newTestDBPath(t)
	db := openOpenCodeAt(t, path)
	exec(t, db, `INSERT INTO session(id, project_id, directory, title, time_created, time_updated) VALUES('ses_root','p1','/Users/me/src/app','',1700000000000,1700000005000)`)
	exec(t, db, `INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES('msg1','ses_root',1,1,'{"role":"user"}')`)
	exec(t, db, `INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES('prt1','msg1','ses_root',1,1,'{"type":"text","text":"fallback title phrase"}')`)
	db.Close()

	src := transcript.NewSource(path, domain.DefaultIndexOptions)
	sessions, err := src.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Title != "fallback title phrase" {
		t.Fatalf("title fallback: %+v", sessions)
	}
}

func TestMessagesMapPartsAndSidechains(t *testing.T) {
	path := newTestDBPath(t)
	db := openOpenCodeAt(t, path)
	seedOpenCodeData(t, db)
	db.Close()

	src := transcript.NewSource(path, domain.DefaultIndexOptions)
	var msgs []domain.Message
	if err := src.Messages(t.Context(), "opencode:ses_root", func(m domain.Message) error {
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	kinds := map[domain.BlockKind]int{}
	var sidechain bool
	var toolUse, toolResult, patch *domain.Message
	for i := range msgs {
		kinds[msgs[i].Kind]++
		if msgs[i].IsSidechain {
			sidechain = true
		}
		switch msgs[i].Kind {
		case domain.KindToolUse:
			toolUse = &msgs[i]
		case domain.KindToolResult:
			switch msgs[i].ToolName {
			case "read":
				toolResult = &msgs[i]
			case "patch":
				patch = &msgs[i]
			}
		}
	}

	if kinds[domain.KindText] != 2 {
		t.Errorf("expected 2 text messages, got %d", kinds[domain.KindText])
	}
	if kinds[domain.KindThinking] != 1 {
		t.Errorf("expected 1 thinking, got %d", kinds[domain.KindThinking])
	}
	if !sidechain {
		t.Error("expected sidechain message")
	}
	if toolUse == nil || toolUse.ToolName != "read" {
		t.Errorf("tool use: %+v", toolUse)
	}
	if toolResult == nil || !strings.Contains(toolResult.Text, "file contents") {
		t.Errorf("tool result: %+v", toolResult)
	}
	if patch == nil || !strings.Contains(patch.Text, "/tmp/a.go") {
		t.Errorf("patch result: %+v", patch)
	}

	for _, m := range msgs {
		if m.UUID == "" {
			t.Error("expected non-empty UUID")
		}
		if m.Role == "" {
			t.Error("expected non-empty role")
		}
	}
}

func TestFingerprintUsesUpdatedAndCount(t *testing.T) {
	path := newTestDBPath(t)
	db := openOpenCodeAt(t, path)
	exec(t, db, `INSERT INTO session(id, project_id, directory, title, time_created, time_updated) VALUES('ses_root','p1','/Users/me/src/app','Root title',1700000000000,1700000005000)`)
	exec(t, db, `INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES('msg1','ses_root',1,1,'{"role":"user"}')`)
	db.Close()

	src := transcript.NewSource(path, domain.DefaultIndexOptions)
	fp, err := src.TranscriptFingerprint(t.Context(), "opencode:ses_root")
	if err != nil {
		t.Fatal(err)
	}
	if fp.Path != "opencode:ses_root" {
		t.Errorf("fp path: %q", fp.Path)
	}
	if !strings.HasPrefix(fp.Hash, "opencode-session-meta-v1:") {
		t.Errorf("fp hash: %q", fp.Hash)
	}

	if _, err := src.TranscriptFingerprint(t.Context(), "opencode:missing"); err == nil {
		t.Fatal("expected error for missing session")
	}
}
