package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/domain"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func openRepo(t *testing.T) *sqlite.Repo {
	t.Helper()
	r, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	if err := r.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	return r
}

// seedRepo seeds a repo with three sessions:
//   - "sess1" (vendor claude): ~60 mixed-kind messages (text, thinking,
//     tool_use, tool_result, sidechain), each mentioning "widget" so
//     search_history/list_sessions have something to find.
//   - "sess2" (vendor cursor): a couple of messages, distinct vendor for
//     filter tests.
//   - "sess3" (vendor claude): a couple of messages, for list_sessions
//     pagination/ordering.
func seedRepo(t *testing.T) *sqlite.Repo {
	t.Helper()
	r := openRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	sess1 := domain.Session{
		ID: "sess1", Title: "Widget session", ProjectPath: "/proj/widget",
		StartedAt: base, EndedAt: base.Add(time.Hour),
		HasTranscript: true, RecordKind: domain.RecordChat, Vendor: domain.VendorClaude,
	}
	var msgs1 []domain.Message
	for i := 0; i < 60; i++ {
		var role domain.Role
		var kind domain.BlockKind
		var sidechain bool
		var toolName string
		switch i % 6 {
		case 0:
			role, kind = domain.RoleUser, domain.KindText
		case 1:
			role, kind = domain.RoleAssistant, domain.KindText
		case 2:
			role, kind = domain.RoleAssistant, domain.KindThinking
		case 3:
			role, kind, toolName = domain.RoleAssistant, domain.KindToolUse, "Bash"
		case 4:
			role, kind = domain.RoleUser, domain.KindToolResult
		case 5:
			role, kind, sidechain = domain.RoleAssistant, domain.KindText, true
		}
		msgs1 = append(msgs1, domain.Message{
			UUID:        fmt.Sprintf("sess1-m%d", i),
			SessionID:   "sess1",
			Role:        role,
			Kind:        kind,
			ToolName:    toolName,
			Text:        fmt.Sprintf("widget message %d about %s content", i, kind),
			Timestamp:   base.Add(time.Duration(i) * time.Minute),
			Sequence:    i,
			IsSidechain: sidechain,
			Source:      domain.SourceTranscript,
		})
	}
	if err := r.ReplaceSession(ctx, sess1, msgs1); err != nil {
		t.Fatalf("seed sess1: %v", err)
	}

	sess2 := domain.Session{
		ID: "sess2", Title: "Cursor session", ProjectPath: "/proj/other",
		StartedAt: base, EndedAt: base.Add(time.Hour),
		HasTranscript: true, RecordKind: domain.RecordChat, Vendor: domain.VendorCursor,
	}
	msgs2 := []domain.Message{
		{UUID: "sess2-m0", SessionID: "sess2", Role: domain.RoleUser, Kind: domain.KindText, Text: "cursor gadget text", Sequence: 0, Timestamp: base},
		{UUID: "sess2-m1", SessionID: "sess2", Role: domain.RoleAssistant, Kind: domain.KindText, Text: "cursor gadget reply", Sequence: 1, Timestamp: base},
	}
	if err := r.ReplaceSession(ctx, sess2, msgs2); err != nil {
		t.Fatalf("seed sess2: %v", err)
	}

	sess3 := domain.Session{
		ID: "sess3", Title: "Third session", ProjectPath: "/proj/widget",
		StartedAt: base.Add(2 * time.Hour), EndedAt: base.Add(3 * time.Hour),
		HasTranscript: true, RecordKind: domain.RecordChat, Vendor: domain.VendorClaude,
	}
	msgs3 := []domain.Message{
		{UUID: "sess3-m0", SessionID: "sess3", Role: domain.RoleUser, Kind: domain.KindText, Text: "another widget note", Sequence: 0, Timestamp: base},
	}
	if err := r.ReplaceSession(ctx, sess3, msgs3); err != nil {
		t.Fatalf("seed sess3: %v", err)
	}

	return r
}

func newHandlers(r *sqlite.Repo) *handlers {
	return &handlers{d: Deps{Search: app.NewSearchService(r)}}
}

// --- get_session ---------------------------------------------------------

func TestGetSessionDefaultKindsExcludeThinkingAndSidechain(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, out, err := h.getSession(context.Background(), nil, getSessionInput{SessionID: "sess1", Limit: 200})
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	for _, m := range out.Messages {
		if m.Kind == string(domain.KindThinking) {
			t.Fatalf("expected thinking blocks to be excluded by default, got seq %d", m.Seq)
		}
		if m.Sidechain {
			t.Fatalf("expected sidechain messages to be excluded by default, got seq %d", m.Seq)
		}
	}
	// 60 messages, 1/6 thinking + 1/6 sidechain excluded by default = 40 kept.
	if out.Total != 40 {
		t.Fatalf("expected total 40 (text/tool_use/tool_result, non-sidechain), got %d", out.Total)
	}
}

func TestGetSessionPaging(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, first, err := h.getSession(context.Background(), nil, getSessionInput{SessionID: "sess1", Limit: 10})
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	if first.Total != 40 {
		t.Fatalf("expected total 40, got %d", first.Total)
	}
	if first.Returned != 10 {
		t.Fatalf("expected returned 10, got %d", first.Returned)
	}
	if !first.HasMore {
		t.Fatal("expected has_more true")
	}
	if first.NextOffset != 10 {
		t.Fatalf("expected next_offset 10, got %d", first.NextOffset)
	}

	_, last, err := h.getSession(context.Background(), nil, getSessionInput{SessionID: "sess1", Offset: 35, Limit: 10})
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	if last.Returned != 5 {
		t.Fatalf("expected returned 5 on final page, got %d", last.Returned)
	}
	if last.HasMore {
		t.Fatal("expected has_more false on final page")
	}
}

func TestGetSessionKindsFilterAndIncludeSidechain(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, out, err := h.getSession(context.Background(), nil, getSessionInput{
		SessionID:        "sess1",
		Kinds:            []string{"thinking"},
		IncludeSidechain: true,
		Limit:            200,
	})
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	if out.Total != 10 {
		t.Fatalf("expected 10 thinking blocks, got %d", out.Total)
	}
	for _, m := range out.Messages {
		if m.Kind != "thinking" {
			t.Fatalf("expected only thinking kind, got %s", m.Kind)
		}
	}
}

func TestGetSessionTruncatesText(t *testing.T) {
	r := openRepo(t)
	ctx := context.Background()
	longText := strings.Repeat("x", 3000)
	sess := domain.Session{ID: "s1", Title: "t", HasTranscript: true, Vendor: domain.VendorClaude, RecordKind: domain.RecordChat}
	msgs := []domain.Message{
		{UUID: "u1", SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: longText, Sequence: 0},
	}
	if err := r.ReplaceSession(ctx, sess, msgs); err != nil {
		t.Fatal(err)
	}
	h := newHandlers(r)

	_, out, err := h.getSession(ctx, nil, getSessionInput{SessionID: "s1", MaxChars: 100})
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	if len(out.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out.Messages))
	}
	got := out.Messages[0].Text
	if !strings.HasSuffix(got, "…[truncated]") {
		t.Fatalf("expected truncation marker, got suffix %q", got[max(0, len(got)-20):])
	}
	if len([]rune(got)) > 100+len(" …[truncated]") {
		t.Fatalf("expected text capped near 100 runes, got %d", len([]rune(got)))
	}
}

func TestGetSessionNotFound(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, _, err := h.getSession(context.Background(), nil, getSessionInput{SessionID: "nope"})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !strings.Contains(err.Error(), `"nope" not found`) {
		t.Fatalf("expected not-found error, got: %v", err)
	}
}

func TestGetSessionLimitClamping(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, out, err := h.getSession(context.Background(), nil, getSessionInput{SessionID: "sess1", Limit: 100000})
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	if out.Returned > 200 {
		t.Fatalf("expected limit clamped to <= 200, got %d", out.Returned)
	}
}

// --- search_history --------------------------------------------------------

func TestSearchHistoryFindsSeededSessions(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, out, err := h.searchHistory(context.Background(), nil, searchHistoryInput{Query: "widget"})
	if err != nil {
		t.Fatalf("searchHistory: %v", err)
	}
	if len(out.Hits) == 0 {
		t.Fatal("expected at least one hit for 'widget'")
	}
	for _, hit := range out.Hits {
		if hit.SessionID == "sess2" {
			t.Fatal("expected sess2 (gadget) not to match 'widget'")
		}
	}
}

func TestSearchHistoryLimitClamping(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, out, err := h.searchHistory(context.Background(), nil, searchHistoryInput{Query: "widget", Limit: 100000})
	if err != nil {
		t.Fatalf("searchHistory: %v", err)
	}
	if len(out.Hits) > 100 {
		t.Fatalf("expected hits clamped to <= 100, got %d", len(out.Hits))
	}
}

// --- list_sessions --------------------------------------------------------

func TestListSessionsVendorFilter(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, out, err := h.listSessions(context.Background(), nil, listSessionsInput{Vendor: "cursor", Limit: 50})
	if err != nil {
		t.Fatalf("listSessions: %v", err)
	}
	if len(out.Sessions) != 1 || out.Sessions[0].SessionID != "sess2" {
		t.Fatalf("expected only sess2 for vendor=cursor, got %+v", out.Sessions)
	}
}

func TestListSessionsLimitClamping(t *testing.T) {
	r := seedRepo(t)
	h := newHandlers(r)

	_, out, err := h.listSessions(context.Background(), nil, listSessionsInput{Limit: 100000})
	if err != nil {
		t.Fatalf("listSessions: %v", err)
	}
	if len(out.Sessions) > 200 {
		t.Fatalf("expected sessions clamped to <= 200, got %d", len(out.Sessions))
	}
}

// --- nil dependencies -------------------------------------------------------

func TestSemanticSearchNilDependencyReturnsClearError(t *testing.T) {
	h := &handlers{d: Deps{}}
	_, _, err := h.semanticSearch(context.Background(), nil, semanticSearchInput{Query: "anything"})
	if err == nil {
		t.Fatal("expected error for nil Semantic dependency")
	}
	if !strings.Contains(err.Error(), "Ollama") {
		t.Fatalf("expected error to mention Ollama, got: %v", err)
	}
}

func TestSummarizeSessionNilDependencyReturnsClearError(t *testing.T) {
	r := seedRepo(t)
	h := &handlers{d: Deps{Search: app.NewSearchService(r)}}

	_, _, err := h.summarizeSession(context.Background(), nil, summarizeSessionInput{SessionID: "sess1"})
	if err == nil {
		t.Fatal("expected error for nil Summarize dependency")
	}
	if !strings.Contains(err.Error(), "condensed_only") {
		t.Fatalf("expected error to mention condensed_only fallback, got: %v", err)
	}
}

func TestSummarizeSessionNilDependencyCondensedOnlyFallsBack(t *testing.T) {
	r := seedRepo(t)
	h := &handlers{d: Deps{Search: app.NewSearchService(r)}}

	_, out, err := h.summarizeSession(context.Background(), nil, summarizeSessionInput{SessionID: "sess1", CondensedOnly: true})
	if err != nil {
		t.Fatalf("summarizeSession (condensed_only, nil Summarize): %v", err)
	}
	if out.Condensed == "" {
		t.Fatal("expected condensed text from the Condense fallback")
	}
	if out.Model != "" || out.Summary != "" {
		t.Fatalf("expected no model/summary for condensed_only, got model=%q summary=%q", out.Model, out.Summary)
	}
}

// --- end-to-end over the SDK transport --------------------------------------

func TestServerEndToEndListAndCallTools(t *testing.T) {
	r := seedRepo(t)
	deps := Deps{Search: app.NewSearchService(r), Version: "test"}
	srv := NewServer(deps)

	ctx := context.Background()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()

	ss, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	toolsRes, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{
		"search_history": false, "semantic_search": false, "list_sessions": false,
		"get_session": false, "summarize_session": false,
	}
	for _, tool := range toolsRes.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("expected tool %q to be registered, tools: %+v", name, toolsRes.Tools)
		}
	}

	callRes, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "search_history",
		Arguments: map[string]any{"query": "widget"},
	})
	if err != nil {
		t.Fatalf("CallTool search_history: %v", err)
	}
	if callRes.IsError {
		t.Fatalf("expected search_history to succeed, got error content: %+v", callRes.Content)
	}
}
