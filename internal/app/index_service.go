package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	cursorplan "claude-code-hist-viewer/internal/adapter/cursor/plan"
	"claude-code-hist-viewer/internal/adapter/plan"
	"claude-code-hist-viewer/internal/config"
	"claude-code-hist-viewer/internal/domain"
)

type IndexStats struct {
	Sessions int
	Orphaned int
	Plans    int
	Skipped  int
	Messages int
	Elapsed  time.Duration
}

type IndexRunOptions struct {
	Force bool
	Debug io.Writer
}

type IndexService struct {
	transcripts []domain.TranscriptSource
	prompts     domain.PromptLog
	plans       []domain.PlanSource
	repo        domain.SearchRepository
	cfg         domain.ShrinkConfig
}

func NewIndexService(t domain.TranscriptSource, p domain.PromptLog, pl domain.PlanSource, r domain.SearchRepository, cfg domain.ShrinkConfig) *IndexService {
	var transcripts []domain.TranscriptSource
	if t != nil {
		transcripts = append(transcripts, t)
	}
	var plans []domain.PlanSource
	if pl != nil {
		plans = append(plans, pl)
	}
	return &IndexService{transcripts: transcripts, prompts: p, plans: plans, repo: r, cfg: cfg}
}

func NewIndexServiceMulti(transcripts []domain.TranscriptSource, p domain.PromptLog, plans []domain.PlanSource, r domain.SearchRepository, cfg domain.ShrinkConfig) *IndexService {
	return &IndexService{transcripts: transcripts, prompts: p, plans: plans, repo: r, cfg: cfg}
}

func (s *IndexService) Run(ctx context.Context, indexCfg config.IndexConfig, home string, progress func(done, total int)) (IndexStats, error) {
	return s.RunWithOptions(ctx, indexCfg, home, progress, IndexRunOptions{})
}

// discoveredTranscript is a session found during discovery. Exactly one of
// session/catalog is the primary source; the other, when set, carries richer
// metadata merged in at write time (catalog entries have no metadata, legacy
// Sessions cannot skip without parsing).
type discoveredTranscript struct {
	source  domain.TranscriptSource
	session *domain.Session
	catalog *domain.TranscriptCatalogEntry
	meta    domain.TranscriptMetaSource
}

// indexRun carries per-run state shared by the session handlers.
type indexRun struct {
	svc    *IndexService
	opts   IndexRunOptions
	hashes map[string]string
	stats  *IndexStats
}

func (s *IndexService) RunWithOptions(ctx context.Context, indexCfg config.IndexConfig, home string, progress func(done, total int), opts IndexRunOptions) (IndexStats, error) {
	start := time.Now()
	debugf(opts.Debug, "index start force=%t shrink_enabled=%t shrink_cap=%d", opts.Force, s.cfg.Enabled, s.cfg.ToolPayloadCap)

	if err := s.repo.Init(ctx); err != nil {
		return IndexStats{}, fmt.Errorf("init: %w", err)
	}
	hashes, err := s.repo.FileHashes(ctx)
	if err != nil {
		return IndexStats{}, fmt.Errorf("file hashes: %w", err)
	}
	debugf(opts.Debug, "loaded file hashes=%d", len(hashes))

	dMap := make(map[string]discoveredTranscript)
	for _, src := range s.transcripts {
		if cat, ok := src.(domain.TranscriptCatalogSource); ok {
			if meta, ok := src.(domain.TranscriptMetaSource); ok {
				entries, err := cat.TranscriptCatalog(ctx)
				if err != nil {
					return IndexStats{}, fmt.Errorf("transcript catalog: %w", err)
				}
				for i := range entries {
					d := discoveredTranscript{source: src, catalog: &entries[i], meta: meta}
					if prev, ok := dMap[entries[i].ID]; ok && prev.session != nil {
						d.session = prev.session
					}
					dMap[entries[i].ID] = d
				}
				debugf(opts.Debug, "discovered transcript source=%T catalog=%d", src, len(entries))
				continue
			}
		}
		tSessions, err := src.Sessions(ctx)
		if err != nil {
			return IndexStats{}, fmt.Errorf("transcript sessions: %w", err)
		}
		for _, sess := range tSessions {
			d := discoveredTranscript{source: src}
			if prev, ok := dMap[sess.ID]; ok {
				if prev.session != nil {
					sess = mergeSessionMetadata(sess, *prev.session)
				} else if prev.meta != nil {
					d.meta = prev.meta
				}
			}
			d.session = &sess
			dMap[sess.ID] = d
		}
		debugf(opts.Debug, "discovered transcript source=%T sessions=%d", src, len(tSessions))
	}

	var pAll []domain.Prompt
	if s.prompts != nil {
		var err error
		pAll, err = s.prompts.Prompts(ctx)
		if err != nil {
			return IndexStats{}, fmt.Errorf("prompts: %w", err)
		}
	}
	debugf(opts.Debug, "discovered prompts=%d", len(pAll))
	pMap := make(map[string][]domain.Prompt)
	for _, p := range pAll {
		pMap[p.SessionID] = append(pMap[p.SessionID], p)
	}

	var sessionIDs []string
	for id := range dMap {
		sessionIDs = append(sessionIDs, id)
	}
	for id := range pMap {
		if _, ok := dMap[id]; !ok {
			sessionIDs = append(sessionIDs, id)
		}
	}

	total := len(sessionIDs)
	var stats IndexStats
	stats.Sessions = total
	debugf(opts.Debug, "index sessions total=%d transcript=%d prompt_only=%d", total, len(dMap), len(pMap))

	run := &indexRun{svc: s, opts: opts, hashes: hashes, stats: &stats}
	for i, id := range sessionIDs {
		if progress != nil {
			progress(i, total)
		}
		d, ok := dMap[id]
		switch {
		case ok && d.catalog != nil:
			run.catalogSession(ctx, id, d)
		case ok:
			run.transcriptSession(ctx, id, d)
		default:
			run.promptOnlySession(ctx, id, pMap[id])
		}
	}

	if progress != nil {
		progress(total, total)
	}

	planStats, err := s.indexPlans(ctx, indexCfg, home, opts)
	if err != nil {
		return stats, err
	}
	stats.Plans = planStats.Plans
	stats.Skipped += planStats.Skipped
	stats.Messages += planStats.Messages

	stats.Elapsed = time.Since(start)
	debugf(opts.Debug, "index done sessions=%d orphaned=%d plans=%d skipped=%d messages=%d elapsed=%s", stats.Sessions, stats.Orphaned, stats.Plans, stats.Skipped, stats.Messages, stats.Elapsed.Round(time.Millisecond))
	return stats, nil
}

// transcriptSession indexes a session whose metadata came from Sessions.
func (r *indexRun) transcriptSession(ctx context.Context, id string, d discoveredTranscript) {
	sessionStart := time.Now()
	sess := *d.session
	skipped, fp, matchedSource := r.svc.shouldSkipTranscript(ctx, id, d.source, r.opts, r.hashes)
	if skipped {
		r.stats.Skipped++
		debugf(r.opts.Debug, "skip transcript id=%s path=%s elapsed=%s", id, fp.Path, time.Since(sessionStart).Round(time.Millisecond))
		return
	}

	firstSource := matchedSource
	if firstSource == nil {
		firstSource = d.source
	}
	msgs, loadedSource, loaded := r.svc.loadMessages(ctx, id, firstSource, r.opts.Debug)
	if !loaded {
		debugf(r.opts.Debug, "skip transcript id=%s reason=no_messages elapsed=%s", id, time.Since(sessionStart).Round(time.Millisecond))
		return
	}
	if d.meta != nil {
		if fallback, err := d.meta.SessionMeta(ctx, id); err == nil {
			sess = mergeSessionMetadata(sess, fallback)
		} else {
			debugf(r.opts.Debug, "session meta fallback failed id=%s err=%v", id, err)
		}
	}
	r.writeSession(ctx, id, sess, msgs, fp, loadedSource, sessionStart)
}

// catalogSession indexes a session discovered through a cheap catalog: the
// stored hash decides the skip before any file content is parsed.
func (r *indexRun) catalogSession(ctx context.Context, id string, d discoveredTranscript) {
	sessionStart := time.Now()
	fp := d.catalog.Fingerprint
	if !r.opts.Force && fp.Path != "" && fp.Hash != "" {
		if stored, ok := r.hashes[fp.Path]; ok && stored == fp.Hash {
			r.stats.Skipped++
			debugf(r.opts.Debug, "skip transcript id=%s path=%s elapsed=%s", id, fp.Path, time.Since(sessionStart).Round(time.Millisecond))
			return
		}
	}

	sess, err := d.meta.SessionMeta(ctx, id)
	if err != nil {
		debugf(r.opts.Debug, "session meta failed id=%s err=%v", id, err)
		return
	}
	if d.session != nil {
		sess = mergeSessionMetadata(sess, *d.session)
	}
	msgs, loadedSource, loaded := r.svc.loadMessages(ctx, id, d.source, r.opts.Debug)
	if !loaded {
		debugf(r.opts.Debug, "skip transcript id=%s reason=no_messages elapsed=%s", id, time.Since(sessionStart).Round(time.Millisecond))
		return
	}
	r.writeSession(ctx, id, sess, msgs, fp, loadedSource, sessionStart)
}

func (r *indexRun) writeSession(ctx context.Context, id string, sess domain.Session, msgs []domain.Message, fp domain.TranscriptFingerprint, loadedSource domain.TranscriptSource, sessionStart time.Time) {
	sess.MessageCount = len(msgs)
	sess.RecordKind = domain.RecordChat
	if sess.Vendor == "" {
		sess.Vendor = domain.VendorClaude
	}
	if err := r.svc.repo.ReplaceSession(ctx, sess, msgs); err != nil {
		debugf(r.opts.Debug, "replace transcript failed id=%s err=%v", id, err)
		return
	}
	if fp.Path == "" || fp.Hash == "" {
		fp = r.svc.transcriptFingerprint(ctx, loadedSource, id, r.opts.Debug)
	}
	if fp.Path != "" && fp.Hash != "" {
		if err := r.svc.repo.SetFileHash(ctx, fp.Path, sess.ID, fp.Hash); err != nil {
			debugf(r.opts.Debug, "store transcript fingerprint failed id=%s path=%s err=%v", id, fp.Path, err)
		} else {
			r.hashes[fp.Path] = fp.Hash
		}
	}
	r.stats.Messages += len(msgs)
	debugf(r.opts.Debug, "indexed transcript id=%s messages=%d path=%s elapsed=%s", id, len(msgs), fp.Path, time.Since(sessionStart).Round(time.Millisecond))
}

// promptOnlySession indexes prompts with no transcript. The stored digest of
// the prompts decides the skip, so re-runs stop rewriting these sessions.
func (r *indexRun) promptOnlySession(ctx context.Context, id string, prompts []domain.Prompt) {
	if len(prompts) == 0 {
		return
	}
	sessionStart := time.Now()
	r.stats.Orphaned++

	key := "history:" + id
	digest := promptDigest(prompts)
	if !r.opts.Force {
		if stored, ok := r.hashes[key]; ok && stored == digest {
			r.stats.Skipped++
			debugf(r.opts.Debug, "skip prompt-only id=%s elapsed=%s", id, time.Since(sessionStart).Round(time.Millisecond))
			return
		}
	}

	sess := synthesizeSession(id, prompts)
	msgs := promptsToMessages(id, prompts)
	if err := r.svc.repo.ReplaceSession(ctx, sess, msgs); err != nil {
		debugf(r.opts.Debug, "replace prompt-only failed id=%s err=%v", id, err)
		return
	}
	r.hashes[key] = digest
	if err := r.svc.repo.SetFileHash(ctx, key, id, digest); err != nil {
		debugf(r.opts.Debug, "store prompt-only digest failed id=%s err=%v", id, err)
	}
	r.stats.Messages += len(msgs)
	debugf(r.opts.Debug, "indexed prompt-only id=%s messages=%d elapsed=%s", id, len(msgs), time.Since(sessionStart).Round(time.Millisecond))
}

// promptDigest hashes everything that shapes a prompt-only session, so a
// changed prompt list invalidates the skip and a stable one does not.
func promptDigest(prompts []domain.Prompt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "history-prompts-v1\n")
	for _, p := range prompts {
		fmt.Fprintf(&b, "%d\t%s\t%s\t%s\n", p.Seq, p.Timestamp.UTC().Format(time.RFC3339Nano), p.Project, p.Text)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "history-prompts-v1:" + hex.EncodeToString(sum[:])
}

// loadMessages streams a session from the first source that yields messages.
func (s *IndexService) loadMessages(ctx context.Context, sessionID string, first domain.TranscriptSource, debug io.Writer) ([]domain.Message, domain.TranscriptSource, bool) {
	for _, src := range orderedTranscriptSources(s.transcripts, first) {
		var candidate []domain.Message
		if err := src.Messages(ctx, sessionID, func(m domain.Message) error {
			candidate = append(candidate, m)
			return nil
		}); err == nil && len(candidate) > 0 {
			return candidate, src, true
		} else if err != nil {
			debugf(debug, "load transcript failed id=%s source=%T err=%v", sessionID, src, err)
		} else {
			debugf(debug, "load transcript empty id=%s source=%T", sessionID, src)
		}
	}
	return nil, nil, false
}

func (s *IndexService) indexPlans(ctx context.Context, indexCfg config.IndexConfig, home string, opts IndexRunOptions) (IndexStats, error) {
	var stats IndexStats

	sources := s.plans
	if len(sources) == 0 {
		sources = []domain.PlanSource{plan.NewSource(home, indexCfg)}
	}

	for _, src := range sources {
		paths, err := src.PlanPaths(ctx)
		if err != nil {
			return IndexStats{}, fmt.Errorf("plan paths: %w", err)
		}
		debugf(opts.Debug, "discovered plan source=%T paths=%d", src, len(paths))
		for _, path := range paths {
			planStart := time.Now()
			p, skipped, err := s.indexPlanPath(ctx, path, "", opts.Force, domain.VendorClaude)
			if err != nil {
				debugf(opts.Debug, "index plan failed path=%s err=%v", path, err)
				continue
			}
			if skipped {
				stats.Skipped++
				debugf(opts.Debug, "skip plan id=%s path=%s elapsed=%s", p.ID, path, time.Since(planStart).Round(time.Millisecond))
				continue
			}
			stats.Plans++
			stats.Messages++
			debugf(opts.Debug, "indexed plan id=%s path=%s elapsed=%s", p.ID, path, time.Since(planStart).Round(time.Millisecond))
		}
	}

	cursorSrc := cursorplan.NewSource(home)
	cursorPaths, err := cursorSrc.PlanPaths(ctx)
	if err != nil {
		return stats, fmt.Errorf("cursor plan paths: %w", err)
	}
	debugf(opts.Debug, "discovered cursor plans paths=%d", len(cursorPaths))
	for _, path := range cursorPaths {
		planStart := time.Now()
		p, skipped, err := s.indexCursorPlanPath(ctx, path, opts.Force)
		if err != nil {
			debugf(opts.Debug, "index cursor plan failed path=%s err=%v", path, err)
			continue
		}
		if skipped {
			stats.Skipped++
			debugf(opts.Debug, "skip cursor plan id=%s path=%s elapsed=%s", p.ID, path, time.Since(planStart).Round(time.Millisecond))
			continue
		}
		stats.Plans++
		stats.Messages++
		debugf(opts.Debug, "indexed cursor plan id=%s path=%s elapsed=%s", p.ID, path, time.Since(planStart).Round(time.Millisecond))
	}

	return stats, nil
}

func (s *IndexService) indexPlanPath(ctx context.Context, absPath, titleOverride string, force bool, vendor domain.Vendor) (domain.Plan, bool, error) {
	absPath, err := filepath.Abs(absPath)
	if err != nil {
		return domain.Plan{}, false, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return domain.Plan{}, false, err
	}
	hash := plan.ContentHash(data)

	if !force {
		stored, ok, err := s.repo.GetFileHash(ctx, absPath)
		if err != nil {
			return domain.Plan{}, false, err
		}
		if ok && stored == hash {
			p := domain.Plan{
				ID:     plan.PlanID(absPath),
				Title:  plan.TitleForContent(string(data), titleOverride, filepath.Base(absPath)),
				Vendor: vendor,
			}
			return p, true, nil
		}
	}

	p, err := plan.FromFile(absPath, titleOverride, config.ManualFilesDir())
	if err != nil {
		return domain.Plan{}, false, err
	}
	p.Vendor = vendor

	sess := planToSession(p)
	msgs := planToMessages(p)
	if err := s.repo.ReplaceSession(ctx, sess, msgs); err != nil {
		return domain.Plan{}, false, err
	}
	if err := s.repo.SetFileHash(ctx, absPath, p.ID, hash); err != nil {
		return domain.Plan{}, false, err
	}
	return p, false, nil
}

func (s *IndexService) indexCursorPlanPath(ctx context.Context, absPath string, force bool) (domain.Plan, bool, error) {
	absPath, err := filepath.Abs(absPath)
	if err != nil {
		return domain.Plan{}, false, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return domain.Plan{}, false, err
	}
	hash := plan.ContentHash(data)

	if !force {
		stored, ok, err := s.repo.GetFileHash(ctx, absPath)
		if err != nil {
			return domain.Plan{}, false, err
		}
		if ok && stored == hash {
			p := domain.Plan{
				ID:     cursorplan.PlanID(absPath),
				Title:  cursorplan.TitleForContent(string(data), "", filepath.Base(absPath)),
				Vendor: domain.VendorCursor,
			}
			return p, true, nil
		}
	}

	p, err := cursorplan.FromFile(absPath, "", cursorplan.DefaultArchiveDir())
	if err != nil {
		return domain.Plan{}, false, err
	}

	sess := planToSession(p)
	msgs := planToMessages(p)
	if err := s.repo.ReplaceSession(ctx, sess, msgs); err != nil {
		return domain.Plan{}, false, err
	}
	if err := s.repo.SetFileHash(ctx, absPath, p.ID, hash); err != nil {
		return domain.Plan{}, false, err
	}
	return p, false, nil
}

func (s *IndexService) IndexFile(ctx context.Context, path, title string, force bool) (IndexStats, domain.Plan, bool, error) {
	return s.IndexFileWithOptions(ctx, path, title, IndexRunOptions{Force: force})
}

func (s *IndexService) IndexFileWithOptions(ctx context.Context, path, title string, opts IndexRunOptions) (IndexStats, domain.Plan, bool, error) {
	start := time.Now()
	debugf(opts.Debug, "index file start path=%s force=%t", path, opts.Force)

	if err := s.repo.Init(ctx); err != nil {
		return IndexStats{}, domain.Plan{}, false, fmt.Errorf("init: %w", err)
	}

	p, skipped, err := s.indexPlanPath(ctx, path, title, opts.Force, domain.VendorClaude)
	if err != nil {
		return IndexStats{}, domain.Plan{}, false, err
	}

	stats := IndexStats{Elapsed: time.Since(start)}
	if skipped {
		stats.Skipped = 1
		debugf(opts.Debug, "skip file id=%s path=%s elapsed=%s", p.ID, path, stats.Elapsed.Round(time.Millisecond))
		return stats, p, true, nil
	}

	stats = IndexStats{
		Plans:    1,
		Messages: 1,
		Elapsed:  time.Since(start),
	}
	debugf(opts.Debug, "indexed file id=%s path=%s elapsed=%s", p.ID, path, stats.Elapsed.Round(time.Millisecond))
	return stats, p, false, nil
}

func (s *IndexService) shouldSkipTranscript(ctx context.Context, sessionID string, preferred domain.TranscriptSource, opts IndexRunOptions, hashes map[string]string) (bool, domain.TranscriptFingerprint, domain.TranscriptSource) {
	var matchedSource domain.TranscriptSource
	var matchedFP domain.TranscriptFingerprint
	for _, src := range orderedTranscriptSources(s.transcripts, preferred) {
		fpSource, ok := src.(domain.TranscriptFingerprintSource)
		if !ok {
			continue
		}
		fp, err := fpSource.TranscriptFingerprint(ctx, sessionID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				debugf(opts.Debug, "fingerprint transcript failed id=%s source=%T err=%v", sessionID, src, err)
			}
			continue
		}
		if fp.Path == "" || fp.Hash == "" {
			continue
		}
		matchedSource = src
		matchedFP = fp
		break
	}

	if opts.Force || matchedFP.Path == "" {
		return false, matchedFP, matchedSource
	}
	stored, ok := hashes[matchedFP.Path]
	return ok && stored == matchedFP.Hash, matchedFP, matchedSource
}

func (s *IndexService) transcriptFingerprint(ctx context.Context, src domain.TranscriptSource, sessionID string, debug io.Writer) domain.TranscriptFingerprint {
	if src == nil {
		return domain.TranscriptFingerprint{}
	}
	fpSource, ok := src.(domain.TranscriptFingerprintSource)
	if !ok {
		return domain.TranscriptFingerprint{}
	}
	fp, err := fpSource.TranscriptFingerprint(ctx, sessionID)
	if err != nil {
		debugf(debug, "fingerprint transcript failed id=%s source=%T err=%v", sessionID, src, err)
		return domain.TranscriptFingerprint{}
	}
	return fp
}

func orderedTranscriptSources(sources []domain.TranscriptSource, first domain.TranscriptSource) []domain.TranscriptSource {
	if first == nil {
		return sources
	}
	ordered := make([]domain.TranscriptSource, 0, len(sources))
	ordered = append(ordered, first)
	for _, src := range sources {
		if src != first {
			ordered = append(ordered, src)
		}
	}
	return ordered
}

func mergeSessionMetadata(primary, fallback domain.Session) domain.Session {
	if primary.Title == "" {
		primary.Title = fallback.Title
	}
	if primary.ProjectPath == "" {
		primary.ProjectPath = fallback.ProjectPath
	}
	if primary.GitBranch == "" {
		primary.GitBranch = fallback.GitBranch
	}
	if primary.StartedAt.IsZero() {
		primary.StartedAt = fallback.StartedAt
	}
	if primary.EndedAt.IsZero() {
		primary.EndedAt = fallback.EndedAt
	}
	if primary.MessageCount == 0 {
		primary.MessageCount = fallback.MessageCount
	}
	if primary.FilePath == "" {
		primary.FilePath = fallback.FilePath
	}
	if !primary.HasTranscript {
		primary.HasTranscript = fallback.HasTranscript
	}
	if primary.RecordKind == "" {
		primary.RecordKind = fallback.RecordKind
	}
	if primary.Vendor == "" {
		primary.Vendor = fallback.Vendor
	}
	return primary
}

func debugf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "[debug] "+format+"\n", args...)
}

func synthesizeSession(id string, prompts []domain.Prompt) domain.Session {
	minTS := prompts[0].Timestamp
	maxTS := prompts[0].Timestamp
	project := prompts[0].Project
	title := domain.DeriveTitle(prompts[0].Text, 60)
	if title == "" {
		title = prompts[0].Text
		if runes := []rune(title); len(runes) > 60 {
			title = string(runes[:60]) + "…"
		}
	}
	for _, p := range prompts[1:] {
		if p.Timestamp.Before(minTS) {
			minTS = p.Timestamp
		}
		if p.Timestamp.After(maxTS) {
			maxTS = p.Timestamp
		}
	}
	return domain.Session{
		ID:            id,
		Title:         title,
		ProjectPath:   project,
		StartedAt:     minTS,
		EndedAt:       maxTS,
		MessageCount:  len(prompts),
		HasTranscript: false,
		RecordKind:    domain.RecordChat,
		Vendor:        domain.VendorClaude,
	}
}

func promptsToMessages(sessionID string, prompts []domain.Prompt) []domain.Message {
	msgs := make([]domain.Message, len(prompts))
	for i, p := range prompts {
		msgs[i] = domain.Message{
			UUID:      fmt.Sprintf("%s-prompt-%d", sessionID, i),
			SessionID: sessionID,
			Role:      domain.RoleUser,
			Kind:      domain.KindText,
			Text:      p.Text,
			Source:    domain.SourceHistory,
			Timestamp: p.Timestamp,
			Sequence:  p.Seq,
		}
	}
	return msgs
}

func planToSession(p domain.Plan) domain.Session {
	return domain.Session{
		ID:            p.ID,
		Title:         p.Title,
		ProjectPath:   p.ProjectPath,
		StartedAt:     p.ModTime,
		EndedAt:       p.ModTime,
		MessageCount:  1,
		FilePath:      p.FilePath,
		HasTranscript: false,
		RecordKind:    domain.RecordPlan,
		Vendor:        p.Vendor,
	}
}

func planToMessages(p domain.Plan) []domain.Message {
	return []domain.Message{{
		UUID:      p.ID + "-content",
		SessionID: p.ID,
		Role:      domain.RoleAssistant,
		Kind:      domain.KindText,
		Text:      p.Content,
		Source:    domain.SourcePlan,
		Timestamp: p.ModTime,
		Sequence:  0,
	}}
}
