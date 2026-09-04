package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/config"
	"claude-code-hist-viewer/internal/domain"
)

// fakeTranscriptSource returns a single session with one message.
type fakeTranscriptSource struct {
	sessions     []domain.Session
	msgs         map[string][]domain.Message
	fingerprints map[string]domain.TranscriptFingerprint
	messageCalls map[string]int
}

func (f *fakeTranscriptSource) Sessions(_ context.Context) ([]domain.Session, error) {
	return f.sessions, nil
}

func (f *fakeTranscriptSource) Messages(_ context.Context, id string, yield func(domain.Message) error) error {
	if f.messageCalls != nil {
		f.messageCalls[id]++
	}
	for _, m := range f.msgs[id] {
		if err := yield(m); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeTranscriptSource) TranscriptFingerprint(_ context.Context, id string) (domain.TranscriptFingerprint, error) {
	fp, ok := f.fingerprints[id]
	if !ok {
		return domain.TranscriptFingerprint{}, os.ErrNotExist
	}
	return fp, nil
}

type fakePromptLog struct{ prompts []domain.Prompt }

func (f *fakePromptLog) Prompts(_ context.Context) ([]domain.Prompt, error) {
	return f.prompts, nil
}

type fakePlanSource struct{ paths []string }

func (f *fakePlanSource) PlanPaths(_ context.Context) ([]string, error) {
	return f.paths, nil
}

func openRepo(t *testing.T) *sqlite.Repo {
	t.Helper()
	r, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestNoDuplicateHitsForPromptInBothSources(t *testing.T) {
	ctx := context.Background()
	ts := time.Now().UTC()
	home := t.TempDir()

	src := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-t1", Title: "T1", HasTranscript: true,
			StartedAt: ts, EndedAt: ts,
		}},
		msgs: map[string][]domain.Message{
			"sess-t1": {{
				UUID: "m1", SessionID: "sess-t1",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "unique transcript phrase", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
	}
	log := &fakePromptLog{prompts: []domain.Prompt{
		{SessionID: "sess-t1", Text: "unique transcript phrase", Timestamp: ts, Seq: 0},
	}}

	repo := openRepo(t)
	svc := app.NewIndexService(src, log, nil, repo, domain.DefaultShrinkConfig)
	if _, err := svc.Run(ctx, config.IndexConfig{}, home, nil); err != nil {
		t.Fatal(err)
	}

	hits, err := repo.Search(ctx, "unique transcript", 10)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, h := range hits {
		if h.SessionID == "sess-t1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 hit for sess-t1, got %d", count)
	}
}

func TestOrphanedSessionSearchable(t *testing.T) {
	ctx := context.Background()
	ts := time.Now().UTC()
	home := t.TempDir()

	src := &fakeTranscriptSource{sessions: nil, msgs: nil}
	log := &fakePromptLog{prompts: []domain.Prompt{
		{SessionID: "sess-orphan", Text: "orphaned searchable text", Project: "/proj", Timestamp: ts, Seq: 0},
	}}

	repo := openRepo(t)
	svc := app.NewIndexService(src, log, nil, repo, domain.DefaultShrinkConfig)
	stats, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Orphaned != 1 {
		t.Errorf("expected 1 orphaned, got %d", stats.Orphaned)
	}

	hits, err := repo.Search(ctx, "orphaned searchable", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected orphaned session to be searchable")
	}
	if hits[0].HasTranscript {
		t.Error("orphaned session should have HasTranscript=false")
	}
	if hits[0].SessionID != "sess-orphan" {
		t.Errorf("expected sess-orphan, got %q", hits[0].SessionID)
	}
}

func TestPlansSearchable(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()

	planFile := filepath.Join(home, "PLAN.md")
	if err := os.WriteFile(planFile, []byte("# My Plan\nunique plan phrase here"), 0644); err != nil {
		t.Fatal(err)
	}

	src := &fakeTranscriptSource{sessions: nil, msgs: nil}
	log := &fakePromptLog{}
	plans := &fakePlanSource{paths: []string{planFile}}

	repo := openRepo(t)
	svc := app.NewIndexService(src, log, plans, repo, domain.DefaultShrinkConfig)
	stats, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Plans != 1 {
		t.Errorf("expected 1 plan, got %d", stats.Plans)
	}

	hits, err := repo.Search(ctx, "unique plan phrase", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected plan to be searchable")
	}
	if hits[0].RecordKind != domain.RecordPlan {
		t.Errorf("expected plan record kind, got %q", hits[0].RecordKind)
	}
	if !strings.HasPrefix(hits[0].SessionID, "plan:") {
		t.Errorf("expected plan session id, got %q", hits[0].SessionID)
	}
}

func TestIndexFile(t *testing.T) {
	ctx := context.Background()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "PLAN.md")
	if err := os.WriteFile(src, []byte("# My Doc\nindexfile unique phrase"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := openRepo(t)
	svc := app.NewIndexService(nil, nil, nil, repo, domain.DefaultShrinkConfig)
	stats, p, skipped, err := svc.IndexFile(ctx, src, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if skipped {
		t.Fatal("expected first index not skipped")
	}
	if stats.Plans != 1 || stats.Messages != 1 {
		t.Errorf("stats: %+v", stats)
	}
	if p.Title != "My Doc" {
		t.Errorf("title: got %q", p.Title)
	}
	if !strings.Contains(p.FilePath, "manual") {
		t.Errorf("expected archive under manual dir, got %q", p.FilePath)
	}

	hits, err := repo.Search(ctx, "indexfile unique phrase", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected manual file to be searchable")
	}
	if hits[0].RecordKind != domain.RecordPlan {
		t.Errorf("expected plan record kind, got %q", hits[0].RecordKind)
	}
}

func TestIndexFileHashSkip(t *testing.T) {
	ctx := context.Background()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	src := filepath.Join(t.TempDir(), "PLAN.md")
	if err := os.WriteFile(src, []byte("# Doc\nhash skip phrase"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := openRepo(t)
	svc := app.NewIndexService(nil, nil, nil, repo, domain.DefaultShrinkConfig)

	if _, _, _, err := svc.IndexFile(ctx, src, "", false); err != nil {
		t.Fatal(err)
	}
	stats, _, skipped, err := svc.IndexFile(ctx, src, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !skipped || stats.Skipped != 1 {
		t.Fatalf("expected skip on unchanged file, stats=%+v skipped=%v", stats, skipped)
	}

	stats, _, skipped, err = svc.IndexFile(ctx, src, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if skipped || stats.Plans != 1 {
		t.Fatalf("force should re-index, stats=%+v skipped=%v", stats, skipped)
	}
}

func TestPlanIndexHashSkipOnRun(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	planFile := filepath.Join(home, "PLAN.md")
	if err := os.WriteFile(planFile, []byte("# Plan\ncron hash phrase"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := openRepo(t)
	src := &fakeTranscriptSource{sessions: nil, msgs: nil}
	log := &fakePromptLog{}
	svc := app.NewIndexService(src, log, nil, repo, domain.DefaultShrinkConfig)
	indexCfg := config.IndexConfig{Directories: []string{home}}

	stats1, err := svc.Run(ctx, indexCfg, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Plans != 1 {
		t.Fatalf("expected 1 plan indexed, got %+v", stats1)
	}

	stats2, err := svc.Run(ctx, indexCfg, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Skipped != 1 || stats2.Plans != 0 {
		t.Fatalf("expected skip on second run, got %+v", stats2)
	}
}

func TestTranscriptFingerprintSkipOnRun(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ts := time.Now().UTC()
	fp := domain.TranscriptFingerprint{Path: filepath.Join(home, "session.jsonl"), Hash: "transcript-meta-v1:test"}
	src := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-skip", Title: "Skip", HasTranscript: true,
			FilePath: fp.Path, StartedAt: ts, EndedAt: ts,
		}},
		msgs: map[string][]domain.Message{
			"sess-skip": {{
				UUID: "m1", SessionID: "sess-skip",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "fingerprint skip searchable", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		fingerprints: map[string]domain.TranscriptFingerprint{"sess-skip": fp},
		messageCalls: map[string]int{},
	}

	repo := openRepo(t)
	svc := app.NewIndexService(src, &fakePromptLog{}, nil, repo, domain.DefaultShrinkConfig)
	stats1, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Skipped != 0 || stats1.Messages != 1 {
		t.Fatalf("expected first run to index transcript, got %+v", stats1)
	}

	var debug bytes.Buffer
	stats2, err := svc.RunWithOptions(ctx, config.IndexConfig{}, home, nil, app.IndexRunOptions{Debug: &debug})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Skipped != 1 || stats2.Messages != 0 {
		t.Fatalf("expected second run to skip transcript, got %+v", stats2)
	}
	if src.messageCalls["sess-skip"] != 1 {
		t.Fatalf("expected messages to load once, got %d", src.messageCalls["sess-skip"])
	}
	if !strings.Contains(debug.String(), "skip transcript id=sess-skip") {
		t.Fatalf("expected debug skip line, got %q", debug.String())
	}

	hits, err := repo.Search(ctx, "fingerprint skip searchable", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected skipped transcript to remain searchable")
	}
}

func TestDuplicateTranscriptUsesSelectedSource(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ts := time.Now().UTC()
	plain := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-dupe", Title: "Plain Claude", HasTranscript: true,
			FilePath: filepath.Join(home, "plain.jsonl"), StartedAt: ts, EndedAt: ts,
			Vendor: domain.VendorClaude,
		}},
		msgs: map[string][]domain.Message{
			"sess-dupe": {{
				UUID: "plain-m1", SessionID: "sess-dupe",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "plain source text", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		fingerprints: map[string]domain.TranscriptFingerprint{
			"sess-dupe": {Path: filepath.Join(home, "plain.jsonl"), Hash: "plain"},
		},
		messageCalls: map[string]int{},
	}
	desktop := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-dupe", Title: "Desktop Claude", HasTranscript: true,
			FilePath: filepath.Join(home, "desktop.json"), StartedAt: ts, EndedAt: ts,
			Vendor: domain.VendorClaudeDesktop,
		}},
		msgs: map[string][]domain.Message{
			"sess-dupe": {{
				UUID: "desktop-m1", SessionID: "sess-dupe",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "desktop source text", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		fingerprints: map[string]domain.TranscriptFingerprint{
			"sess-dupe": {Path: filepath.Join(home, "desktop.json"), Hash: "desktop"},
		},
		messageCalls: map[string]int{},
	}

	repo := openRepo(t)
	svc := app.NewIndexServiceMulti(
		[]domain.TranscriptSource{plain, desktop},
		&fakePromptLog{},
		nil,
		repo,
		domain.DefaultShrinkConfig,
	)
	stats, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Sessions != 1 || stats.Messages != 1 {
		t.Fatalf("expected one deduped session/message, got %+v", stats)
	}
	if plain.messageCalls["sess-dupe"] != 0 {
		t.Fatalf("plain source should not load messages, got %d", plain.messageCalls["sess-dupe"])
	}
	if desktop.messageCalls["sess-dupe"] != 1 {
		t.Fatalf("desktop source should load messages once, got %d", desktop.messageCalls["sess-dupe"])
	}

	detail, err := repo.SessionByID(ctx, "sess-dupe")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.Title != "Desktop Claude" {
		t.Fatalf("title = %q, want Desktop Claude", detail.Session.Title)
	}
	if detail.Session.Vendor != domain.VendorClaudeDesktop {
		t.Fatalf("vendor = %q, want claude-desktop", detail.Session.Vendor)
	}
	if len(detail.Messages) != 1 || detail.Messages[0].Text != "desktop source text" {
		t.Fatalf("messages = %+v", detail.Messages)
	}
}

func TestTranscriptFingerprintForceBypassesSkip(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ts := time.Now().UTC()
	fp := domain.TranscriptFingerprint{Path: filepath.Join(home, "session.jsonl"), Hash: "transcript-meta-v1:force"}
	src := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-force", Title: "Force", HasTranscript: true,
			FilePath: fp.Path, StartedAt: ts, EndedAt: ts,
		}},
		msgs: map[string][]domain.Message{
			"sess-force": {{
				UUID: "m1", SessionID: "sess-force",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "force bypass searchable", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		fingerprints: map[string]domain.TranscriptFingerprint{"sess-force": fp},
		messageCalls: map[string]int{},
	}

	repo := openRepo(t)
	svc := app.NewIndexService(src, &fakePromptLog{}, nil, repo, domain.DefaultShrinkConfig)
	if _, err := svc.Run(ctx, config.IndexConfig{}, home, nil); err != nil {
		t.Fatal(err)
	}
	stats, err := svc.RunWithOptions(ctx, config.IndexConfig{}, home, nil, app.IndexRunOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 0 || stats.Messages != 1 {
		t.Fatalf("expected force to re-index transcript, got %+v", stats)
	}
	if src.messageCalls["sess-force"] != 2 {
		t.Fatalf("expected messages to load twice, got %d", src.messageCalls["sess-force"])
	}
}
