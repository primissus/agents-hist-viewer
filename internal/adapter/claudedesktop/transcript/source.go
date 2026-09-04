package transcript

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	claudetranscript "claude-code-hist-viewer/internal/adapter/transcript"
	"claude-code-hist-viewer/internal/domain"
)

// Source implements domain.TranscriptSource over Claude Desktop agent sessions.
type Source struct {
	root       string
	claudeRoot string
	opts       domain.IndexOptions
}

type localSession struct {
	SessionID      string `json:"sessionId"`
	CLISessionID   string `json:"cliSessionId"`
	CWD            string `json:"cwd"`
	OriginCWD      string `json:"originCwd"`
	Title          string `json:"title"`
	CreatedAt      int64  `json:"createdAt"`
	LastActivityAt int64  `json:"lastActivityAt"`
}

type desktopRecord struct {
	metaPath       string
	transcriptPath string
	session        domain.Session
}

func NewSource(root, claudeRoot string, opts domain.IndexOptions) *Source {
	return &Source{root: root, claudeRoot: claudeRoot, opts: opts}
}

func (s *Source) Sessions(ctx context.Context) ([]domain.Session, error) {
	records, err := s.records(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]domain.Session, 0, len(records))
	for _, rec := range records {
		sessions = append(sessions, rec.session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (s *Source) Messages(ctx context.Context, sessionID string, yield func(domain.Message) error) error {
	rec, err := s.record(ctx, sessionID)
	if err != nil {
		return err
	}
	detail, err := claudetranscript.LoadFile(ctx, rec.transcriptPath, s.opts)
	if err != nil {
		return err
	}
	for _, m := range detail.Messages {
		if err := yield(m); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) TranscriptFingerprint(ctx context.Context, sessionID string) (domain.TranscriptFingerprint, error) {
	if err := ctx.Err(); err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	rec, err := s.record(ctx, sessionID)
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	paths := append([]string{rec.metaPath, rec.transcriptPath}, sidechainPaths(rec.transcriptPath, sessionID)...)
	return fingerprintForFiles(paths, s.opts)
}

func (s *Source) record(ctx context.Context, sessionID string) (desktopRecord, error) {
	records, err := s.records(ctx)
	if err != nil {
		return desktopRecord{}, err
	}
	rec, ok := records[sessionID]
	if !ok {
		return desktopRecord{}, os.ErrNotExist
	}
	return rec, nil
}

func (s *Source) records(ctx context.Context) (map[string]desktopRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transcripts, err := transcriptPaths(s.claudeRoot)
	if err != nil {
		return nil, err
	}

	sessionRoot := filepath.Join(s.root, "claude-code-sessions")
	out := make(map[string]desktopRecord)
	err = filepath.WalkDir(sessionRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "local_") || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		rec, ok := s.readRecord(path, transcripts)
		if !ok {
			return nil
		}
		if prev, exists := out[rec.session.ID]; exists && !rec.session.EndedAt.After(prev.session.EndedAt) {
			return nil
		}
		out[rec.session.ID] = rec
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	return out, nil
}

func (s *Source) readRecord(path string, transcripts map[string]string) (desktopRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return desktopRecord{}, false
	}
	var local localSession
	if err := json.Unmarshal(data, &local); err != nil {
		return desktopRecord{}, false
	}
	if local.CLISessionID == "" {
		return desktopRecord{}, false
	}
	transcriptPath, ok := transcripts[local.CLISessionID]
	if !ok {
		return desktopRecord{}, false
	}

	started := parseDesktopTime(local.CreatedAt)
	ended := parseDesktopTime(local.LastActivityAt)
	if started.IsZero() {
		if info, err := os.Stat(transcriptPath); err == nil {
			started = info.ModTime().UTC()
		}
	}
	if ended.IsZero() {
		ended = started
	}
	projectPath := local.CWD
	if projectPath == "" {
		projectPath = local.OriginCWD
	}

	return desktopRecord{
		metaPath:       path,
		transcriptPath: transcriptPath,
		session: domain.Session{
			ID:            local.CLISessionID,
			Title:         local.Title,
			ProjectPath:   projectPath,
			StartedAt:     started,
			EndedAt:       ended,
			FilePath:      transcriptPath,
			HasTranscript: true,
			RecordKind:    domain.RecordChat,
			Vendor:        domain.VendorClaudeDesktop,
		},
	}, true
}

func transcriptPaths(root string) (map[string]string, error) {
	out := make(map[string]string)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, proj := range entries {
		if !proj.IsDir() {
			continue
		}
		projDir := filepath.Join(root, proj.Name())
		files, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			id := strings.TrimSuffix(f.Name(), ".jsonl")
			out[id] = filepath.Join(projDir, f.Name())
		}
	}
	return out, nil
}

func parseDesktopTime(ts int64) time.Time {
	if ts <= 0 {
		return time.Time{}
	}
	if ts > 1_000_000_000_000 {
		return time.UnixMilli(ts).UTC()
	}
	return time.Unix(ts, 0).UTC()
}

func sidechainPaths(filePath, sessionID string) []string {
	scDir := filepath.Join(filepath.Dir(filePath), sessionID, "subagents")
	scEntries, _ := os.ReadDir(scDir)
	var paths []string
	for _, e := range scEntries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "agent-") || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		paths = append(paths, filepath.Join(scDir, e.Name()))
	}
	sort.Strings(paths)
	return paths
}

func fingerprintForFiles(paths []string, opts domain.IndexOptions) (domain.TranscriptFingerprint, error) {
	if len(paths) == 0 {
		return domain.TranscriptFingerprint{}, os.ErrNotExist
	}
	mainPath, err := filepath.Abs(paths[0])
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}

	var manifest strings.Builder
	fmt.Fprintf(&manifest, "claude-desktop-transcript-meta-v1\n")
	fmt.Fprintf(&manifest, "depth=%d\n", opts.Depth)
	fmt.Fprintf(&manifest, "shrink_enabled=%t\n", opts.Shrink.Enabled)
	fmt.Fprintf(&manifest, "shrink_cap=%d\n", opts.Shrink.ToolPayloadCap)
	fmt.Fprintf(&manifest, "drop_images=%t\n", opts.Shrink.DropImages)
	for _, path := range paths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return domain.TranscriptFingerprint{}, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return domain.TranscriptFingerprint{}, err
		}
		fmt.Fprintf(&manifest, "%s\t%d\t%d\n", absPath, info.Size(), info.ModTime().UTC().UnixNano())
	}
	sum := sha256.Sum256([]byte(manifest.String()))
	return domain.TranscriptFingerprint{
		Path: mainPath,
		Hash: "claude-desktop-transcript-meta-v1:" + hex.EncodeToString(sum[:]),
	}, nil
}
