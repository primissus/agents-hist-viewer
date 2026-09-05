package app_test

import (
	"context"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/domain"
)

func addSessionWithBash(t *testing.T, r interface {
	ReplaceSession(context.Context, domain.Session, []domain.Message) error
}, id string, cmds []string) {
	t.Helper()
	msgs := make([]domain.Message, len(cmds))
	for i, cmd := range cmds {
		msgs[i] = domain.Message{
			UUID: id + "-b" + string(rune('0'+i)), SessionID: id,
			Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Bash",
			Text: `Bash {"command":"` + cmd + `"}`, Sequence: i,
		}
	}
	s := domain.Session{ID: id, Title: id, HasTranscript: true, StartedAt: time.Now(), EndedAt: time.Now(), Vendor: domain.VendorClaude}
	if err := r.ReplaceSession(context.Background(), s, msgs); err != nil {
		t.Fatal(err)
	}
}

func TestPatternServiceFindsRepeatedBashSequence(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)
	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}

	repeated := []string{"go build ./...", "go test ./..."}
	addSessionWithBash(t, r, "sess-1", repeated)
	addSessionWithBash(t, r, "sess-2", repeated)
	addSessionWithBash(t, r, "sess-3", repeated)
	addSessionWithBash(t, r, "sess-4", []string{"ls", "pwd"}) // trivial-only, shouldn't win

	svc := app.NewPatternService(r, nil)
	report, err := svc.Run(ctx, "fake-model", app.PatternOptions{Kind: app.PatternScripts, NoLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Scripts) == 0 {
		t.Fatal("expected at least one script candidate")
	}
	found := false
	for _, c := range report.Scripts {
		if len(c.NGram) == 2 && c.NGram[0] == "go build ./..." && c.NGram[1] == "go test ./..." {
			found = true
			if c.Sessions != 3 {
				t.Fatalf("expected 3 sessions for the repeated n-gram, got %d", c.Sessions)
			}
		}
	}
	if !found {
		t.Fatalf("expected the repeated build+test n-gram among candidates: %+v", report.Scripts)
	}
}

func TestPatternServiceDropsClustersBelowMinSessions(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)
	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}
	// Only 2 sessions share this n-gram — below the min-3 threshold.
	addSessionWithBash(t, r, "sess-1", []string{"docker build .", "docker push x"})
	addSessionWithBash(t, r, "sess-2", []string{"docker build .", "docker push x"})

	svc := app.NewPatternService(r, nil)
	report, err := svc.Run(ctx, "fake-model", app.PatternOptions{Kind: app.PatternScripts, NoLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range report.Scripts {
		if len(c.NGram) == 2 && c.NGram[0] == "docker build ." {
			t.Fatalf("expected 2-session n-gram to be dropped, got candidate: %+v", c)
		}
	}
}

func TestPatternServiceSkillMiningRequiresMinItems(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)
	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.InitEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}

	s := domain.Session{ID: "s1", Title: "t", HasTranscript: true, StartedAt: time.Now(), EndedAt: time.Now(), Vendor: domain.VendorClaude}
	msgs := []domain.Message{
		{UUID: "u1", SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "a real user question about testing", Sequence: 0},
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}
	units, err := r.UnembeddedUnits(ctx, "fake-model", domain.EmbedFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: units[0].MessageRowID, SessionID: "s1", Vector: []float32{1, 0, 0, 0}, Model: "fake-model"},
	}); err != nil {
		t.Fatal(err)
	}

	svc := app.NewPatternService(r, nil)
	report, err := svc.Run(ctx, "fake-model", app.PatternOptions{Kind: app.PatternSkills, NoLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skills) != 0 {
		t.Fatalf("expected no skill candidates with only 1 embedded item, got %+v", report.Skills)
	}
}
