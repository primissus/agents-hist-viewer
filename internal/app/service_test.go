package app_test

import (
	"bytes"
	"context"
	"errors"
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

// fakeCatalogSource exposes a catalog and per-session metadata so the index
// can skip unchanged sessions without calling Sessions.
type fakeCatalogSource struct {
	entries   []domain.TranscriptCatalogEntry
	metas     map[string]domain.Session
	msgs      map[string][]domain.Message
	metaCalls map[string]int
	msgCalls  map[string]int
}

func (f *fakeCatalogSource) Sessions(_ context.Context) ([]domain.Session, error) {
	return nil, os.ErrInvalid
}

func (f *fakeCatalogSource) TranscriptCatalog(_ context.Context) ([]domain.TranscriptCatalogEntry, error) {
	return f.entries, nil
}

func (f *fakeCatalogSource) SessionMeta(_ context.Context, id string) (domain.Session, error) {
	f.metaCalls[id]++
	sess, ok := f.metas[id]
	if !ok {
		return domain.Session{}, os.ErrNotExist
	}
	return sess, nil
}

func (f *fakeCatalogSource) Messages(_ context.Context, id string, yield func(domain.Message) error) error {
	f.msgCalls[id]++
	for _, m := range f.msgs[id] {
		if err := yield(m); err != nil {
			return err
		}
	}
	return nil
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

	hits, err := repo.Search(ctx, "unique transcript", 10, domain.SearchFilter{})
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

	hits, err := repo.Search(ctx, "orphaned searchable", 10, domain.SearchFilter{})
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

	hits, err := repo.Search(ctx, "unique plan phrase", 10, domain.SearchFilter{})
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

	hits, err := repo.Search(ctx, "indexfile unique phrase", 10, domain.SearchFilter{})
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

	hits, err := repo.Search(ctx, "fingerprint skip searchable", 10, domain.SearchFilter{})
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

func TestPromptOnlySkipOnRun(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ts := time.Now().UTC()

	src := &fakeTranscriptSource{sessions: nil, msgs: nil}
	log := &fakePromptLog{prompts: []domain.Prompt{
		{SessionID: "sess-prompt", Text: "prompt only unique phrase", Project: "/proj", Timestamp: ts, Seq: 0},
	}}

	repo := openRepo(t)
	svc := app.NewIndexService(src, log, nil, repo, domain.DefaultShrinkConfig)

	stats1, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Orphaned != 1 || stats1.Messages != 1 {
		t.Fatalf("expected first run to index prompt-only session, got %+v", stats1)
	}

	stats2, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Orphaned != 1 || stats2.Skipped != 1 || stats2.Messages != 0 {
		t.Fatalf("expected second run to skip unchanged prompts, got %+v", stats2)
	}

	stats3, err := svc.RunWithOptions(ctx, config.IndexConfig{}, home, nil, app.IndexRunOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats3.Messages != 1 {
		t.Fatalf("expected force to re-index prompts, got %+v", stats3)
	}

	hits, err := repo.Search(ctx, "prompt only unique", 10, domain.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected prompt-only session to remain searchable")
	}
}

func TestOverlappingCatalogAndSessionsSources(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ts := time.Now().UTC()
	fp := domain.TranscriptFingerprint{Path: filepath.Join(home, "overlap.jsonl"), Hash: "transcript-meta-v1:overlap"}

	catalog := &fakeCatalogSource{
		entries: []domain.TranscriptCatalogEntry{{ID: "sess-overlap", Vendor: domain.VendorClaude, Fingerprint: fp}},
		metas: map[string]domain.Session{"sess-overlap": {
			ID: "sess-overlap", Title: "Catalog Meta", HasTranscript: true,
			FilePath: fp.Path, StartedAt: ts, EndedAt: ts,
		}},
		msgs: map[string][]domain.Message{
			"sess-overlap": {{
				UUID: "catalog-m1", SessionID: "sess-overlap",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "catalog source text", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		metaCalls: map[string]int{},
		msgCalls:  map[string]int{},
	}
	plain := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-overlap", Title: "Plain Title", HasTranscript: true,
			FilePath: filepath.Join(home, "plain.jsonl"), StartedAt: ts, EndedAt: ts,
			Vendor: domain.VendorClaudeDesktop,
		}},
		msgs: map[string][]domain.Message{
			"sess-overlap": {{
				UUID: "plain-m1", SessionID: "sess-overlap",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "plain source text", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		fingerprints: map[string]domain.TranscriptFingerprint{
			"sess-overlap": {Path: filepath.Join(home, "plain.jsonl"), Hash: "plain"},
		},
		messageCalls: map[string]int{},
	}

	repo := openRepo(t)
	svc := app.NewIndexServiceMulti(
		[]domain.TranscriptSource{catalog, plain},
		&fakePromptLog{},
		nil,
		repo,
		domain.DefaultShrinkConfig,
	)

	stats1, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Sessions != 1 || stats1.Indexed != 1 || stats1.Messages != 1 {
		t.Fatalf("expected one indexed session, got %+v", stats1)
	}
	if catalog.msgCalls["sess-overlap"] != 0 {
		t.Fatalf("catalog source should not load messages, got %d", catalog.msgCalls["sess-overlap"])
	}
	if plain.messageCalls["sess-overlap"] != 1 {
		t.Fatalf("later source should load messages once, got %d", plain.messageCalls["sess-overlap"])
	}

	detail, err := repo.SessionByID(ctx, "sess-overlap")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.Title != "Plain Title" {
		t.Fatalf("title = %q, want later source primary metadata", detail.Session.Title)
	}
	if detail.Session.Vendor != domain.VendorClaudeDesktop {
		t.Fatalf("vendor = %q, want later source vendor", detail.Session.Vendor)
	}
	if len(detail.Messages) != 1 || detail.Messages[0].Text != "plain source text" {
		t.Fatalf("messages = %+v", detail.Messages)
	}

	stats2, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Skipped != 1 || stats2.Indexed != 0 || stats2.Messages != 0 {
		t.Fatalf("expected catalog fast path to skip on second run, got %+v", stats2)
	}
	if catalog.metaCalls["sess-overlap"] != 1 {
		t.Fatalf("expected unchanged session not to parse meta again, got %d", catalog.metaCalls["sess-overlap"])
	}
	if plain.messageCalls["sess-overlap"] != 1 {
		t.Fatalf("expected unchanged session not to parse messages again, got %d", plain.messageCalls["sess-overlap"])
	}
}

type failingReplaceRepo struct {
	domain.SearchRepository
	err error
}

func (f failingReplaceRepo) ReplaceSession(context.Context, domain.Session, []domain.Message) error {
	return f.err
}

func TestRunReturnsErrorOnReplaceFailure(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ts := time.Now().UTC()

	src := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-boom", Title: "Boom", HasTranscript: true,
			StartedAt: ts, EndedAt: ts,
		}},
		msgs: map[string][]domain.Message{
			"sess-boom": {{
				UUID: "m1", SessionID: "sess-boom",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "replace failure phrase", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
	}

	repo := openRepo(t)
	wrapped := failingReplaceRepo{SearchRepository: repo, err: errors.New("boom")}
	svc := app.NewIndexService(src, &fakePromptLog{}, nil, wrapped, domain.DefaultShrinkConfig)

	var warn bytes.Buffer
	stats, err := svc.RunWithOptions(ctx, config.IndexConfig{}, home, nil, app.IndexRunOptions{Warn: &warn})
	if err == nil {
		t.Fatal("expected run to return the replace failure")
	}
	if stats.Failed != 1 {
		t.Fatalf("stats.Failed = %d, want 1", stats.Failed)
	}
	if stats.Indexed != 0 {
		t.Fatalf("stats.Indexed = %d, want 0", stats.Indexed)
	}
	if !strings.Contains(warn.String(), "replace transcript failed id=sess-boom") {
		t.Fatalf("expected warning on stderr writer, got %q", warn.String())
	}
}

type hashSpyRepo struct {
	domain.SearchRepository
	batchCalls   int
	batchEntries int
}

func (h *hashSpyRepo) SetFileHashes(ctx context.Context, entries []domain.FileHash) error {
	h.batchCalls++
	h.batchEntries += len(entries)
	return h.SearchRepository.SetFileHashes(ctx, entries)
}

func TestSingleBatchHashFlush(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	ts := time.Now().UTC()
	fp := domain.TranscriptFingerprint{Path: filepath.Join(home, "batch.jsonl"), Hash: "batch-hash"}

	src := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-batch", Title: "Batch", HasTranscript: true,
			FilePath: fp.Path, StartedAt: ts, EndedAt: ts,
		}},
		msgs: map[string][]domain.Message{
			"sess-batch": {{
				UUID: "m1", SessionID: "sess-batch",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "batch flush phrase", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		fingerprints: map[string]domain.TranscriptFingerprint{"sess-batch": fp},
		messageCalls: map[string]int{},
	}
	planFile := filepath.Join(home, "PLAN.md")
	if err := os.WriteFile(planFile, []byte("# Batch Plan\nbatch plan phrase"), 0644); err != nil {
		t.Fatal(err)
	}
	plans := &fakePlanSource{paths: []string{planFile}}

	spy := &hashSpyRepo{SearchRepository: openRepo(t)}
	svc := app.NewIndexService(src, &fakePromptLog{}, plans, spy, domain.DefaultShrinkConfig)

	stats1, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Indexed != 2 {
		t.Fatalf("expected transcript + plan indexed, got %+v", stats1)
	}
	if spy.batchCalls != 1 || spy.batchEntries != 2 {
		t.Fatalf("expected one batch flush with 2 entries, got calls=%d entries=%d", spy.batchCalls, spy.batchEntries)
	}

	stats2, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Skipped != 2 || stats2.Indexed != 0 {
		t.Fatalf("expected second run to skip all, got %+v", stats2)
	}
	if spy.batchCalls != 1 {
		t.Fatalf("expected no flush on all-skip run, got %d calls", spy.batchCalls)
	}
}

type noGetHashRepo struct {
	domain.SearchRepository
	getCalls int
}

func (n *noGetHashRepo) GetFileHash(context.Context, string) (string, bool, error) {
	n.getCalls++
	return "", false, errors.New("unexpected per-plan hash lookup")
}

func TestPlanIndexUsesHashMap(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	planFile := filepath.Join(home, "PLAN.md")
	if err := os.WriteFile(planFile, []byte("# HashMap Plan\nplan hash map phrase"), 0644); err != nil {
		t.Fatal(err)
	}

	spy := &noGetHashRepo{SearchRepository: openRepo(t)}
	svc := app.NewIndexService(&fakeTranscriptSource{}, &fakePromptLog{}, &fakePlanSource{paths: []string{planFile}}, spy, domain.DefaultShrinkConfig)

	stats1, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Plans != 1 {
		t.Fatalf("expected plan indexed, got %+v", stats1)
	}
	stats2, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Skipped != 1 || stats2.Plans != 0 {
		t.Fatalf("expected hash-map skip on second run, got %+v", stats2)
	}
	if spy.getCalls != 0 {
		t.Fatalf("expected plan indexing to use the loaded hash map, got %d GetFileHash calls", spy.getCalls)
	}
}

func TestLoadMessagesDoesNotProbeUnrelatedSources(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ts := time.Now().UTC()

	src := &fakeTranscriptSource{
		sessions: []domain.Session{{
			ID: "sess-solo", Title: "Solo", HasTranscript: true,
			StartedAt: ts, EndedAt: ts,
		}},
		msgs: map[string][]domain.Message{
			"sess-solo": {{
				UUID: "solo-m1", SessionID: "sess-solo",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "solo source phrase", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		messageCalls: map[string]int{},
	}
	unrelated := &fakeTranscriptSource{
		msgs:         map[string][]domain.Message{},
		messageCalls: map[string]int{},
	}

	repo := openRepo(t)
	svc := app.NewIndexServiceMulti(
		[]domain.TranscriptSource{src, unrelated},
		&fakePromptLog{},
		nil,
		repo,
		domain.DefaultShrinkConfig,
	)
	if _, err := svc.Run(ctx, config.IndexConfig{}, home, nil); err != nil {
		t.Fatal(err)
	}
	if src.messageCalls["sess-solo"] != 1 {
		t.Fatalf("owning source message loads = %d, want 1", src.messageCalls["sess-solo"])
	}
	if unrelated.messageCalls["sess-solo"] != 0 {
		t.Fatalf("unrelated source should not be probed, got %d loads", unrelated.messageCalls["sess-solo"])
	}
}

func TestCatalogSourceSkipOnRun(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ts := time.Now().UTC()
	fp := domain.TranscriptFingerprint{Path: filepath.Join(home, "catalog.jsonl"), Hash: "transcript-meta-v1:catalog"}

	src := &fakeCatalogSource{
		entries: []domain.TranscriptCatalogEntry{{ID: "sess-cat", Vendor: domain.VendorClaude, Fingerprint: fp}},
		metas: map[string]domain.Session{"sess-cat": {
			ID: "sess-cat", Title: "Catalog", HasTranscript: true,
			FilePath: fp.Path, StartedAt: ts, EndedAt: ts,
		}},
		msgs: map[string][]domain.Message{
			"sess-cat": {{
				UUID: "m1", SessionID: "sess-cat",
				Role: domain.RoleUser, Kind: domain.KindText,
				Text: "catalog skip phrase", Source: domain.SourceTranscript,
				Timestamp: ts, Sequence: 0,
			}},
		},
		metaCalls: map[string]int{},
		msgCalls:  map[string]int{},
	}

	repo := openRepo(t)
	svc := app.NewIndexService(src, &fakePromptLog{}, nil, repo, domain.DefaultShrinkConfig)

	stats1, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Skipped != 0 || stats1.Messages != 1 {
		t.Fatalf("expected first run to index catalog session, got %+v", stats1)
	}

	stats2, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Skipped != 1 || stats2.Messages != 0 {
		t.Fatalf("expected second run to skip catalog session, got %+v", stats2)
	}
	if src.metaCalls["sess-cat"] != 1 || src.msgCalls["sess-cat"] != 1 {
		t.Fatalf("expected unchanged session not to be parsed again, meta=%d msgs=%d", src.metaCalls["sess-cat"], src.msgCalls["sess-cat"])
	}

	src.entries[0].Fingerprint.Hash = "transcript-meta-v1:changed"
	stats3, err := svc.Run(ctx, config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats3.Messages != 1 {
		t.Fatalf("expected changed fingerprint to re-index, got %+v", stats3)
	}

	stats4, err := svc.RunWithOptions(ctx, config.IndexConfig{}, home, nil, app.IndexRunOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats4.Messages != 1 {
		t.Fatalf("expected force to re-index catalog session, got %+v", stats4)
	}

	detail, err := repo.SessionByID(ctx, "sess-cat")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.Title != "Catalog" {
		t.Fatalf("title = %q, want metadata from SessionMeta", detail.Session.Title)
	}
}
