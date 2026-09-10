package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/domain"
)

func baseSummarizeSession(id string) domain.Session {
	return domain.Session{
		ID: id, Title: "Test session", ProjectPath: "/tmp/proj", GitBranch: "main",
		StartedAt:    time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		EndedAt:      time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC),
		MessageCount: 4, HasTranscript: true, RecordKind: domain.RecordChat, Vendor: domain.VendorClaude,
	}
}

func TestCondenseDropsThinkingImagesSidechainAndCapsPayloads(t *testing.T) {
	longAssistant := strings.Repeat("a", 3000)
	longTool := strings.Repeat("b", 3000)

	detail := domain.SessionDetail{
		Session: baseSummarizeSession("s1"),
		Messages: []domain.Message{
			{SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "help me fix the bug", Sequence: 0},
			{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindThinking, Text: "SECRET_THINKING_MARKER", Sequence: 1},
			{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindText, Text: longAssistant, Sequence: 2},
			{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Bash", Text: longTool, Sequence: 3},
			{SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindImage, Text: "IMAGE_MARKER", Sequence: 4},
			{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindText, Text: "SIDECHAIN_MARKER", Sequence: 5, IsSidechain: true},
		},
	}

	got := app.Condense(detail, app.CondenseOptions{MaxChars: 24000, ToolPayloadCap: 400, AssistantCap: 1500})

	if strings.Contains(got.Text, "SECRET_THINKING_MARKER") {
		t.Fatal("expected thinking block to be dropped")
	}
	if strings.Contains(got.Text, "IMAGE_MARKER") {
		t.Fatal("expected image block to be dropped")
	}
	if strings.Contains(got.Text, "SIDECHAIN_MARKER") {
		t.Fatal("expected sidechain message to be dropped")
	}
	if !strings.Contains(got.Text, "…[truncated]") {
		t.Fatal("expected a truncation marker for capped assistant/tool payloads")
	}
	if strings.Count(got.Text, strings.Repeat("a", 1500)) == 0 {
		t.Fatal("expected assistant text truncated to AssistantCap runes")
	}
	if strings.Count(got.Text, strings.Repeat("b", 400)) == 0 {
		t.Fatal("expected tool payload truncated to ToolPayloadCap runes")
	}
	if strings.Contains(got.Text, strings.Repeat("a", 1501)) {
		t.Fatal("assistant text exceeded AssistantCap")
	}
	if strings.Contains(got.Text, strings.Repeat("b", 401)) {
		t.Fatal("tool payload exceeded ToolPayloadCap")
	}

	// Deterministic: same input, same output.
	again := app.Condense(detail, app.CondenseOptions{MaxChars: 24000, ToolPayloadCap: 400, AssistantCap: 1500})
	if again.Text != got.Text {
		t.Fatal("expected Condense to be deterministic for identical input")
	}
}

func TestCondenseTinyBudgetKeepsAllUserTurns(t *testing.T) {
	long := strings.Repeat("x", 2000)
	msgs := []domain.Message{
		{SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "USER-0", Sequence: 0},
		{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindText, Text: long, Sequence: 1},
		{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Bash", Text: long, Sequence: 2},
		{SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "USER-3", Sequence: 3},
		{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindText, Text: long, Sequence: 4},
		{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Bash", Text: long, Sequence: 5},
		{SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "USER-6", Sequence: 6},
		{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindText, Text: long, Sequence: 7},
		{SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Bash", Text: long, Sequence: 8},
		{SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "USER-9", Sequence: 9},
	}
	detail := domain.SessionDetail{Session: baseSummarizeSession("s1"), Messages: msgs}

	got := app.Condense(detail, app.CondenseOptions{MaxChars: 50, ToolPayloadCap: 400, AssistantCap: 1500})

	if !got.Truncated {
		t.Fatal("expected Truncated with a tiny budget")
	}
	if got.Omitted == 0 {
		t.Fatal("expected some blocks to be omitted")
	}
	for _, want := range []string{"USER-0", "USER-3", "USER-6", "USER-9"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("expected mandatory user turn %q to be present, got: %q", want, got.Text)
		}
	}
	if n := strings.Count(got.Text, "blocks omitted"); n != 1 {
		t.Fatalf("expected exactly one omitted-blocks marker, got %d", n)
	}
}

func seedSummarizeSession(t *testing.T, r domain.SearchRepository, id, userText string) domain.SessionDetail {
	t.Helper()
	if err := r.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := baseSummarizeSession(id)
	msgs := []domain.Message{
		{SessionID: id, Role: domain.RoleUser, Kind: domain.KindText, Text: userText, Sequence: 0},
		{SessionID: id, Role: domain.RoleAssistant, Kind: domain.KindText, Text: "assistant reply", Sequence: 1},
	}
	if err := r.ReplaceSession(context.Background(), s, msgs); err != nil {
		t.Fatal(err)
	}
	return domain.SessionDetail{Session: s, Messages: msgs}
}

func TestSummarizeDetailCacheHitSkipsChat(t *testing.T) {
	r := openRepo(t)
	detail := seedSummarizeSession(t, r, "s1", "please fix the bug")

	chat := &fakeChatModel{answer: "## Goal\n- fix bug"}
	svc := app.NewSummarizeService(chat, "model-a", r, r)

	first, err := svc.SummarizeDetail(context.Background(), detail, app.SummarizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Cached {
		t.Fatal("expected first call to be a cache miss")
	}
	if chat.calls != 1 {
		t.Fatalf("expected 1 chat call, got %d", chat.calls)
	}

	second, err := svc.SummarizeDetail(context.Background(), detail, app.SummarizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached {
		t.Fatal("expected second call to be a cache hit")
	}
	if second.Summary != first.Summary {
		t.Fatalf("expected cached summary to match: %q vs %q", second.Summary, first.Summary)
	}
	if chat.calls != 1 {
		t.Fatalf("expected chat not to be called again on cache hit, got %d calls", chat.calls)
	}
}

func TestSummarizeDetailRefreshRecallsChat(t *testing.T) {
	r := openRepo(t)
	detail := seedSummarizeSession(t, r, "s1", "please fix the bug")

	chat := &fakeChatModel{answer: "## Goal\n- fix bug"}
	svc := app.NewSummarizeService(chat, "model-a", r, r)

	if _, err := svc.SummarizeDetail(context.Background(), detail, app.SummarizeOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SummarizeDetail(context.Background(), detail, app.SummarizeOptions{Refresh: true}); err != nil {
		t.Fatal(err)
	}
	if chat.calls != 2 {
		t.Fatalf("expected Refresh to re-call chat, got %d calls", chat.calls)
	}
}

func TestSummarizeDetailDifferentModelMissesCache(t *testing.T) {
	r := openRepo(t)
	detail := seedSummarizeSession(t, r, "s1", "please fix the bug")

	chatA := &fakeChatModel{answer: "summary-a"}
	svcA := app.NewSummarizeService(chatA, "model-a", r, r)
	if _, err := svcA.SummarizeDetail(context.Background(), detail, app.SummarizeOptions{}); err != nil {
		t.Fatal(err)
	}

	chatB := &fakeChatModel{answer: "summary-b"}
	svcB := app.NewSummarizeService(chatB, "model-b", r, r)
	result, err := svcB.SummarizeDetail(context.Background(), detail, app.SummarizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Cached {
		t.Fatal("expected a different chat model to miss the cache")
	}
	if chatB.calls != 1 {
		t.Fatalf("expected chatB to be called, got %d calls", chatB.calls)
	}
}

func TestSummarizeDetailContentChangeInvalidatesCache(t *testing.T) {
	r := openRepo(t)
	detail := seedSummarizeSession(t, r, "s1", "please fix the bug")

	chat := &fakeChatModel{answer: "first summary"}
	svc := app.NewSummarizeService(chat, "model-a", r, r)
	if _, err := svc.SummarizeDetail(context.Background(), detail, app.SummarizeOptions{}); err != nil {
		t.Fatal(err)
	}

	// Simulate a re-index (ReplaceSession) that changes the message content.
	changed := seedSummarizeSession(t, r, "s1", "please fix a totally different bug")
	chat.answer = "second summary"

	result, err := svc.SummarizeDetail(context.Background(), changed, app.SummarizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Cached {
		t.Fatal("expected changed content to invalidate the cache")
	}
	if chat.calls != 2 {
		t.Fatalf("expected chat to be re-called after content change, got %d calls", chat.calls)
	}
	if result.Summary != "second summary" {
		t.Fatalf("expected fresh summary, got %q", result.Summary)
	}
}

func TestSummarizeDetailNoLLMNeverCallsChat(t *testing.T) {
	r := openRepo(t)
	detail := seedSummarizeSession(t, r, "s1", "please fix the bug")

	chat := &fakeChatModel{answer: "should not be called"}
	svc := app.NewSummarizeService(chat, "model-a", r, r)

	result, err := svc.SummarizeDetail(context.Background(), detail, app.SummarizeOptions{NoLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if chat.calls != 0 {
		t.Fatalf("expected chat never to be called with NoLLM, got %d calls", chat.calls)
	}
	if result.Summary != "" || result.Model != "" {
		t.Fatalf("expected no Summary/Model in NoLLM mode, got %+v", result)
	}
	if result.Condensed == "" {
		t.Fatal("expected Condensed text in NoLLM mode")
	}
}
