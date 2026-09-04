package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/domain"
)

func openTemp(t *testing.T) *sqlite.Repo {
	t.Helper()
	r, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	ctx := context.Background()
	if err := r.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	return r
}

func makeSession(id, title string) domain.Session {
	return domain.Session{
		ID: id, Title: title,
		ProjectPath:   "/tmp/proj",
		StartedAt:     time.Now().UTC(),
		EndedAt:       time.Now().UTC(),
		HasTranscript: true,
		RecordKind:    domain.RecordChat,
		Vendor:        domain.VendorClaude,
	}
}

func makeMsg(sessionID, uuid string, seq int, text string) domain.Message {
	return domain.Message{
		UUID: uuid, SessionID: sessionID,
		Role: domain.RoleUser, Kind: domain.KindText,
		Text: text, Source: domain.SourceTranscript,
		Timestamp: time.Now().UTC(), Sequence: seq,
	}
}

func TestSearchFindsInsertedSession(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "Test Session")
	msgs := []domain.Message{makeMsg("s1", "m1", 0, "hello world unique phrase")}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatalf("replace: %v", err)
	}

	hits, err := r.Search(ctx, "unique phrase", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits, got none")
	}
	if hits[0].SessionID != "s1" {
		t.Errorf("got session %q, want s1", hits[0].SessionID)
	}
	if !strings.Contains(hits[0].Snippet, "[") {
		t.Errorf("expected snippet markers, got %q", hits[0].Snippet)
	}
}

func TestClaudeDesktopVendorPersists(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s-desktop", "Desktop Session")
	s.Vendor = domain.VendorClaudeDesktop
	msgs := []domain.Message{makeMsg("s-desktop", "m1", 0, "desktop vendor phrase")}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatalf("replace: %v", err)
	}

	hits, err := r.Search(ctx, "desktop vendor", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hit")
	}
	if hits[0].Vendor != domain.VendorClaudeDesktop {
		t.Fatalf("search vendor = %q, want claude-desktop", hits[0].Vendor)
	}

	recent, err := r.RecentSessions(ctx, domain.RecentQuery{Limit: 10, Vendor: domain.VendorClaudeDesktop})
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 1 || recent[0].SessionID != "s-desktop" {
		t.Fatalf("recent = %+v", recent)
	}
}

func TestSearchRequiresAllTerms(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s-and", "AND session")
	msgs := []domain.Message{makeMsg("s-and", "m1", 0, "alpha beta gamma")}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatalf("replace: %v", err)
	}

	hits, err := r.Search(ctx, "alpha gamma", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for AND query, got %d", len(hits))
	}

	hits, err = r.Search(ctx, "alpha missing", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected no hits when one term missing, got %d", len(hits))
	}
}

func TestSearchORQuery(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s1 := makeSession("s-or1", "OR one")
	if err := r.ReplaceSession(ctx, s1, []domain.Message{makeMsg("s-or1", "m1", 0, "alpha only")}); err != nil {
		t.Fatal(err)
	}
	s2 := makeSession("s-or2", "OR two")
	if err := r.ReplaceSession(ctx, s2, []domain.Message{makeMsg("s-or2", "m2", 0, "beta only")}); err != nil {
		t.Fatal(err)
	}

	compiled, err := domain.CompileSearchQuery("alpha | beta", domain.SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := r.Search(ctx, compiled, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 OR hits, got %d", len(hits))
	}
}

func TestSearchGroupedAND(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s-grp", "group")
	msgs := []domain.Message{makeMsg("s-grp", "m1", 0, "alpha beta gamma")}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}

	compiled, err := domain.CompileSearchQuery("(alpha + beta) gamma", domain.SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := r.Search(ctx, compiled, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected grouped AND hit, got %d", len(hits))
	}
}

func TestSearchHitIncludesFilePath(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	wantPath := "/tmp/proj/transcript.jsonl"
	s := makeSession("s-fp", "File path session")
	s.FilePath = wantPath
	msgs := []domain.Message{makeMsg("s-fp", "m-fp", 0, "file path keyword here")}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatalf("replace: %v", err)
	}

	hits, err := r.Search(ctx, "file path keyword", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected search hits")
	}
	if hits[0].FilePath != wantPath {
		t.Fatalf("search FilePath = %q, want %q", hits[0].FilePath, wantPath)
	}

	recent, err := r.RecentSessions(ctx, domain.RecentQuery{Limit: 10})
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	var found bool
	for _, h := range recent {
		if h.SessionID == "s-fp" {
			found = true
			if h.FilePath != wantPath {
				t.Fatalf("recent FilePath = %q, want %q", h.FilePath, wantPath)
			}
		}
	}
	if !found {
		t.Fatal("session s-fp not in recent results")
	}
}

func TestSearchFindsSessionOutsideRecentWindow(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()
	base := time.Now().UTC()

	for i := 0; i < 60; i++ {
		id := "s-recent-" + strconv.Itoa(i)
		s := makeSession(id, "recent")
		s.StartedAt = base.Add(time.Duration(i) * time.Minute)
		s.EndedAt = s.StartedAt
		if err := r.ReplaceSession(ctx, s, []domain.Message{
			makeMsg(id, id+"-m", 0, "ordinary recent content"),
		}); err != nil {
			t.Fatal(err)
		}
	}

	old := makeSession("s-old", "old but searchable")
	old.StartedAt = base.Add(-24 * time.Hour)
	old.EndedAt = old.StartedAt
	if err := r.ReplaceSession(ctx, old, []domain.Message{
		makeMsg("s-old", "s-old-m", 0, "needle outside recent window"),
	}); err != nil {
		t.Fatal(err)
	}

	recent, err := r.RecentSessions(ctx, domain.RecentQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range recent {
		if h.SessionID == "s-old" {
			t.Fatal("old session unexpectedly appeared in recent window")
		}
	}

	hits, err := r.Search(ctx, "needle outside", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].SessionID != "s-old" {
		t.Fatalf("search missed old session, hits=%v", hits)
	}
}

func TestSearchRankOrder(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	// s2 has 3 occurrences → higher relevance
	s1 := makeSession("s1", "low")
	m1 := makeMsg("s1", "m1", 0, "gopher appears once here")
	s2 := makeSession("s2", "high")
	m2 := makeMsg("s2", "m2", 0, "gopher gopher gopher three times the gopher")

	if err := r.ReplaceSession(ctx, s1, []domain.Message{m1}); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceSession(ctx, s2, []domain.Message{m2}); err != nil {
		t.Fatal(err)
	}

	hits, err := r.Search(ctx, "gopher", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].SessionID != "s2" {
		t.Errorf("expected s2 first (higher relevance), got %q", hits[0].SessionID)
	}
	// bm25 returns negative scores; closer to 0 = less relevant
	if hits[0].Score >= hits[1].Score {
		t.Errorf("expected score[0] < score[1] (more negative = better in bm25), got %f vs %f", hits[0].Score, hits[1].Score)
	}
}

func TestSessionByIDOrderedBySeq(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "ordered")
	msgs := []domain.Message{
		makeMsg("s1", "m3", 2, "third"),
		makeMsg("s1", "m1", 0, "first"),
		makeMsg("s1", "m2", 1, "second"),
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}

	detail, err := r.SessionByID(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(detail.Messages))
	}
	for i, want := range []string{"m1", "m2", "m3"} {
		if detail.Messages[i].UUID != want {
			t.Errorf("pos %d: got %q, want %q", i, detail.Messages[i].UUID, want)
		}
	}
}

func TestReplaceSessionIdempotent(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "idempotent")
	msgs := []domain.Message{makeMsg("s1", "m1", 0, "searchable content here")}

	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}

	hits, err := r.Search(ctx, "searchable content", 10)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, h := range hits {
		if h.SessionID == "s1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 hit for s1 after idempotent replace, got %d", count)
	}
}

func TestRecordKindPersisted(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	chat := makeSession("s-chat", "chat")
	chat.RecordKind = domain.RecordChat
	plan := makeSession("plan:abc", "plan doc")
	plan.RecordKind = domain.RecordPlan

	for _, s := range []domain.Session{chat, plan} {
		msgs := []domain.Message{makeMsg(s.ID, s.ID+"-m", 0, "shared keyword "+s.ID)}
		if err := r.ReplaceSession(ctx, s, msgs); err != nil {
			t.Fatal(err)
		}
	}

	detail, err := r.SessionByID(ctx, "plan:abc")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.RecordKind != domain.RecordPlan {
		t.Errorf("session detail kind = %q", detail.Session.RecordKind)
	}

	hits, err := r.Search(ctx, "shared keyword", 10)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[domain.RecordKind]bool{}
	for _, h := range hits {
		kinds[h.RecordKind] = true
	}
	if !kinds[domain.RecordChat] || !kinds[domain.RecordPlan] {
		t.Errorf("expected both kinds in hits, got %v", kinds)
	}
}

func TestRecentSessionsFilterByKind(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	chat := makeSession("s-chat", "chat")
	chat.RecordKind = domain.RecordChat
	plan := makeSession("plan:abc", "plan doc")
	plan.RecordKind = domain.RecordPlan

	for _, s := range []domain.Session{chat, plan} {
		msgs := []domain.Message{makeMsg(s.ID, s.ID+"-m", 0, "content")}
		if err := r.ReplaceSession(ctx, s, msgs); err != nil {
			t.Fatal(err)
		}
	}

	plans, err := r.RecentSessions(ctx, domain.RecentQuery{Limit: 10, RecordKind: domain.RecordPlan})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].RecordKind != domain.RecordPlan {
		t.Errorf("got %d hits, kind=%q", len(plans), plans[0].RecordKind)
	}
}

func TestFileHashRoundtrip(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	path := "/tmp/example/PLAN.md"
	hash := "abc123"
	if _, ok, err := r.GetFileHash(ctx, path); err != nil || ok {
		t.Fatalf("expected missing hash, ok=%v err=%v", ok, err)
	}
	if err := r.SetFileHash(ctx, path, "plan:deadbeef", hash); err != nil {
		t.Fatal(err)
	}
	got, ok, err := r.GetFileHash(ctx, path)
	if err != nil || !ok || got != hash {
		t.Fatalf("got %q ok=%v err=%v", got, ok, err)
	}
	if err := r.SetFileHash(ctx, path, "plan:deadbeef", "updated"); err != nil {
		t.Fatal(err)
	}
	got, ok, err = r.GetFileHash(ctx, path)
	if err != nil || !ok || got != "updated" {
		t.Fatalf("got %q ok=%v err=%v", got, ok, err)
	}
}

func TestInitMigratesLegacyDBWithoutVendor(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyDDL := `
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    project_path TEXT NOT NULL DEFAULT '',
    git_branch TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    ended_at TEXT,
    message_count INTEGER NOT NULL DEFAULT 0,
    file_path TEXT NOT NULL DEFAULT '',
    has_transcript INTEGER NOT NULL DEFAULT 0,
    record_kind TEXT NOT NULL DEFAULT 'chat'
);
CREATE TABLE messages (
    rowid INTEGER PRIMARY KEY,
    uuid TEXT NOT NULL,
    parent_uuid TEXT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    kind TEXT NOT NULL,
    tool_name TEXT,
    source TEXT NOT NULL DEFAULT 'transcript',
    timestamp TEXT,
    seq INTEGER NOT NULL,
    is_sidechain INTEGER NOT NULL DEFAULT 0,
    text TEXT NOT NULL DEFAULT ''
);
CREATE VIRTUAL TABLE messages_fts USING fts5(text);
CREATE TABLE file_hashes (
    path TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`
	if _, err := db.ExecContext(ctx, legacyDDL); err != nil {
		t.Fatal(err)
	}
	db.Close()

	r, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if err := r.Init(ctx); err != nil {
		t.Fatalf("init legacy db: %v", err)
	}

	s := makeSession("legacy-s1", "legacy")
	if err := r.ReplaceSession(ctx, s, []domain.Message{
		makeMsg("legacy-s1", "legacy-s1-m", 0, "legacy vendor migration"),
	}); err != nil {
		t.Fatal(err)
	}
	detail, err := r.SessionByID(ctx, "legacy-s1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.Vendor != domain.VendorClaude {
		t.Errorf("vendor after migration: %q", detail.Session.Vendor)
	}
}

func TestVendorPersistedAndFiltered(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	claude := makeSession("s-claude", "claude chat")
	claude.Vendor = domain.VendorClaude
	cursor := makeSession("cursor:abc", "cursor chat")
	cursor.Vendor = domain.VendorCursor
	cursor.ProjectPath = "/other/proj"

	for _, s := range []domain.Session{claude, cursor} {
		msgs := []domain.Message{makeMsg(s.ID, s.ID+"-m", 0, "shared vendor keyword "+s.ID)}
		if err := r.ReplaceSession(ctx, s, msgs); err != nil {
			t.Fatal(err)
		}
	}

	detail, err := r.SessionByID(ctx, "cursor:abc")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.Vendor != domain.VendorCursor {
		t.Errorf("detail vendor: %q", detail.Session.Vendor)
	}

	hits, err := r.Search(ctx, "shared vendor keyword", 10)
	if err != nil {
		t.Fatal(err)
	}
	vendors := map[domain.Vendor]bool{}
	for _, h := range hits {
		vendors[h.Vendor] = true
	}
	if !vendors[domain.VendorClaude] || !vendors[domain.VendorCursor] {
		t.Errorf("expected both vendors in search hits, got %v", vendors)
	}

	cursorOnly, err := r.RecentSessions(ctx, domain.RecentQuery{Limit: 10, Vendor: domain.VendorCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(cursorOnly) != 1 || cursorOnly[0].Vendor != domain.VendorCursor {
		t.Fatalf("recent vendor filter: %+v", cursorOnly)
	}
}
