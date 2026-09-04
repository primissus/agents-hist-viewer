package transcript

import (
	"bufio"
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

	"claude-code-hist-viewer/internal/domain"
)

// Source implements domain.TranscriptSource over ~/.cursor/projects agent transcripts.
type Source struct {
	root string
	opts domain.IndexOptions
}

func NewSource(root string, opts domain.IndexOptions) *Source {
	return &Source{root: root, opts: opts}
}

func SessionID(chatID string) string { return "cursor:" + chatID }

func LoadFile(ctx context.Context, filePath string, opts domain.IndexOptions) (domain.SessionDetail, error) {
	if err := ctx.Err(); err != nil {
		return domain.SessionDetail{}, err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return domain.SessionDetail{}, err
	}
	if info.IsDir() {
		return domain.SessionDetail{}, fmt.Errorf("%s is a directory", filePath)
	}

	chatID := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	sessionID := SessionID(chatID)
	projectPath := projectPathForFile(filePath)
	baseTS := info.ModTime().UTC()
	sess := scanMeta(filePath, chatID, projectPath, baseTS)

	seq := 0
	var messages []domain.Message
	appendRecord := func(rec record, line int, isSidechain bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return parseRecord(rec, line, baseTS, sessionID, isSidechain, &seq, opts, func(m domain.Message) error {
			m.ProjectPath = projectPath
			messages = append(messages, m)
			return nil
		})
	}
	if err := streamFile(filePath, func(rec record, line int) error {
		return appendRecord(rec, line, false)
	}); err != nil {
		return domain.SessionDetail{}, err
	}

	scDir := filepath.Join(filepath.Dir(filePath), "subagents")
	scEntries, _ := os.ReadDir(scDir)
	for _, e := range scEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		scPath := filepath.Join(scDir, e.Name())
		if err := streamFile(scPath, func(rec record, line int) error {
			return appendRecord(rec, line, true)
		}); err != nil {
			continue
		}
	}

	sess.MessageCount = len(messages)
	if sess.ProjectPath == "" {
		sess.ProjectPath = projectPath
	}
	return domain.SessionDetail{Session: sess, Messages: messages}, nil
}

func (s *Source) Sessions(ctx context.Context) ([]domain.Session, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []domain.Session
	for _, proj := range entries {
		if !proj.IsDir() {
			continue
		}
		projectPath := decodeProjectPath(proj.Name())
		transcriptRoot := filepath.Join(s.root, proj.Name(), "agent-transcripts")
		chats, err := os.ReadDir(transcriptRoot)
		if err != nil {
			continue
		}
		for _, chat := range chats {
			if !chat.IsDir() {
				continue
			}
			chatID := chat.Name()
			filePath := filepath.Join(transcriptRoot, chatID, chatID+".jsonl")
			info, err := os.Stat(filePath)
			if err != nil {
				continue
			}
			meta := scanMeta(filePath, chatID, projectPath, info.ModTime().UTC())
			sessions = append(sessions, meta)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (s *Source) Messages(ctx context.Context, sessionID string, yield func(domain.Message) error) error {
	chatID := strings.TrimPrefix(sessionID, "cursor:")
	filePath, err := s.findFile(chatID)
	if err != nil {
		return err
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	baseTS := info.ModTime().UTC()

	seq := 0
	if err := streamFile(filePath, func(rec record, line int) error {
		return parseRecord(rec, line, baseTS, sessionID, false, &seq, s.opts, yield)
	}); err != nil {
		return err
	}

	scDir := filepath.Join(filepath.Dir(filePath), "subagents")
	scEntries, _ := os.ReadDir(scDir)
	for _, e := range scEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		scPath := filepath.Join(scDir, e.Name())
		if err := streamFile(scPath, func(rec record, line int) error {
			return parseRecord(rec, line, baseTS, sessionID, true, &seq, s.opts, yield)
		}); err != nil {
			continue
		}
	}
	return nil
}

func (s *Source) TranscriptFingerprint(ctx context.Context, sessionID string) (domain.TranscriptFingerprint, error) {
	if err := ctx.Err(); err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	chatID := strings.TrimPrefix(sessionID, "cursor:")
	filePath, err := s.findFile(chatID)
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	paths := append([]string{filePath}, cursorSidechainPaths(filePath)...)
	return transcriptFingerprintForFiles(paths, s.opts)
}

func (s *Source) findFile(chatID string) (string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return "", err
	}
	for _, proj := range entries {
		if !proj.IsDir() {
			continue
		}
		p := filepath.Join(s.root, proj.Name(), "agent-transcripts", chatID, chatID+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func cursorSidechainPaths(filePath string) []string {
	scDir := filepath.Join(filepath.Dir(filePath), "subagents")
	scEntries, _ := os.ReadDir(scDir)
	var paths []string
	for _, e := range scEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		paths = append(paths, filepath.Join(scDir, e.Name()))
	}
	sort.Strings(paths)
	return paths
}

func transcriptFingerprintForFiles(paths []string, opts domain.IndexOptions) (domain.TranscriptFingerprint, error) {
	if len(paths) == 0 {
		return domain.TranscriptFingerprint{}, os.ErrNotExist
	}
	mainPath, err := filepath.Abs(paths[0])
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}

	var manifest strings.Builder
	fmt.Fprintf(&manifest, "transcript-meta-v1\n")
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
		Hash: "transcript-meta-v1:" + hex.EncodeToString(sum[:]),
	}, nil
}

func scanMeta(filePath, chatID, projectPath string, mod time.Time) domain.Session {
	sess := domain.Session{
		ID:            SessionID(chatID),
		ProjectPath:   projectPath,
		FilePath:      filePath,
		HasTranscript: true,
		Vendor:        domain.VendorCursor,
		RecordKind:    domain.RecordChat,
		StartedAt:     mod,
		EndedAt:       mod,
	}
	_ = streamFile(filePath, func(rec record, line int) error {
		if title := firstUserTitle(rec); title != "" && sess.Title == "" {
			sess.Title = title
		}
		sess.MessageCount++
		return nil
	})
	return sess
}

func decodeProjectPath(folder string) string {
	if strings.Contains(folder, "-") && !isNumeric(folder) {
		p := "/" + strings.ReplaceAll(folder, "-", "/")
		return filepath.Clean(p)
	}
	return folder
}

func projectPathForFile(filePath string) string {
	transcriptRoot := filepath.Dir(filepath.Dir(filePath))
	if filepath.Base(transcriptRoot) == "agent-transcripts" {
		projectFolder := filepath.Base(filepath.Dir(transcriptRoot))
		return decodeProjectPath(projectFolder)
	}
	return filepath.Dir(filePath)
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func streamFile(path string, fn func(record, int) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var rec record
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		if err := fn(rec, line); err != nil {
			return err
		}
		line++
	}
	return sc.Err()
}
