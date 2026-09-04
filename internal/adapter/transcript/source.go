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

// Source implements domain.TranscriptSource over ~/.claude/projects.
type Source struct {
	root string
	opts domain.IndexOptions
}

func NewSource(root string, opts domain.IndexOptions) *Source {
	return &Source{root: root, opts: opts}
}

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

	fallbackID := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	sessionID := scanSessionID(filePath, fallbackID)
	meta, err := scanSessionMeta(filePath, sessionID)
	if err != nil {
		return domain.SessionDetail{}, err
	}

	seq := 0
	var messages []domain.Message
	appendEnv := func(env envelope) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if env.IsMeta {
			return nil
		}
		if env.Type != "user" && env.Type != "assistant" {
			return nil
		}
		return parseMessages(env, &seq, opts, func(m domain.Message) error {
			if m.SessionID == "" {
				m.SessionID = sessionID
			}
			messages = append(messages, m)
			return nil
		})
	}
	if err := streamFile(filePath, appendEnv); err != nil {
		return domain.SessionDetail{}, err
	}

	scDir := filepath.Join(filepath.Dir(filePath), sessionID, "subagents")
	scEntries, _ := os.ReadDir(scDir)
	for _, e := range scEntries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "agent-") || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		scPath := filepath.Join(scDir, e.Name())
		if err := streamFile(scPath, func(env envelope) error {
			env.IsSidechain = true
			return appendEnv(env)
		}); err != nil {
			continue
		}
	}

	sess := meta.session
	sess.ID = sessionID
	sess.FilePath = filePath
	sess.HasTranscript = true
	sess.RecordKind = domain.RecordChat
	sess.Vendor = domain.VendorClaude
	sess.MessageCount = len(messages)
	sess.StartedAt = meta.minTS
	sess.EndedAt = meta.maxTS
	if sess.StartedAt.IsZero() {
		sess.StartedAt = info.ModTime().UTC()
	}
	if sess.EndedAt.IsZero() {
		sess.EndedAt = sess.StartedAt
	}
	if meta.customTitle != "" {
		sess.Title = meta.customTitle
	} else if meta.aiTitle != "" {
		sess.Title = meta.aiTitle
	} else {
		sess.Title = firstUserTitle(messages)
	}
	if sess.ProjectPath == "" {
		sess.ProjectPath = filepath.Dir(filePath)
	}

	return domain.SessionDetail{Session: sess, Messages: messages}, nil
}

// sessionMeta holds title resolution and time-range state during scan.
type sessionMeta struct {
	session     domain.Session
	aiTitle     string
	customTitle string
	minTS       time.Time
	maxTS       time.Time
	msgCount    int
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
		projDir := filepath.Join(s.root, proj.Name())
		files, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(f.Name(), ".jsonl")
			filePath := filepath.Join(projDir, f.Name())
			meta, err := scanSessionMeta(filePath, sessionID)
			if err != nil {
				continue
			}
			sess := meta.session
			sess.FilePath = filePath
			sess.HasTranscript = true
			sess.ID = sessionID
			sess.Vendor = domain.VendorClaude
			sess.MessageCount = meta.msgCount
			sess.StartedAt = meta.minTS
			sess.EndedAt = meta.maxTS
			if meta.customTitle != "" {
				sess.Title = meta.customTitle
			} else if meta.aiTitle != "" {
				sess.Title = meta.aiTitle
			}
			sessions = append(sessions, sess)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (s *Source) Messages(ctx context.Context, sessionID string, yield func(domain.Message) error) error {
	filePath, err := s.findFile(sessionID)
	if err != nil {
		return err
	}

	seq := 0
	if err := streamFile(filePath, func(env envelope) error {
		if env.IsMeta {
			return nil
		}
		if env.Type != "user" && env.Type != "assistant" {
			return nil
		}
		return parseMessages(env, &seq, s.opts, yield)
	}); err != nil {
		return err
	}

	// Also stream sidechain files.
	projDir := filepath.Dir(filePath)
	scDir := filepath.Join(projDir, sessionID, "subagents")
	scEntries, _ := os.ReadDir(scDir)
	for _, e := range scEntries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "agent-") || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		scPath := filepath.Join(scDir, e.Name())
		if err := streamFile(scPath, func(env envelope) error {
			if env.IsMeta {
				return nil
			}
			if env.Type != "user" && env.Type != "assistant" {
				return nil
			}
			env.IsSidechain = true
			return parseMessages(env, &seq, s.opts, yield)
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
	filePath, err := s.findFile(sessionID)
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	paths := append([]string{filePath}, claudeSidechainPaths(filePath, sessionID)...)
	return transcriptFingerprintForFiles(paths, s.opts)
}

func (s *Source) findFile(sessionID string) (string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return "", err
	}
	for _, proj := range entries {
		if !proj.IsDir() {
			continue
		}
		p := filepath.Join(s.root, proj.Name(), sessionID+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func claudeSidechainPaths(filePath, sessionID string) []string {
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

func scanSessionID(filePath, fallback string) string {
	sessionID := fallback
	_ = streamFile(filePath, func(env envelope) error {
		if env.SessionID != "" {
			sessionID = env.SessionID
		}
		return nil
	})
	return sessionID
}

func firstUserTitle(messages []domain.Message) string {
	for _, m := range messages {
		if m.Role == domain.RoleUser && m.Kind == domain.KindText {
			if title := domain.DeriveTitle(m.Text, 60); title != "" {
				return title
			}
		}
	}
	return ""
}

func scanSessionMeta(filePath, sessionID string) (*sessionMeta, error) {
	meta := &sessionMeta{}
	meta.session.ID = sessionID

	err := streamFile(filePath, func(env envelope) error {
		switch env.Type {
		case "ai-title":
			meta.aiTitle = env.AiTitle
		case "custom-title":
			meta.customTitle = env.CustomTitle
		case "user", "assistant":
			if env.IsMeta {
				return nil
			}
			ts := parseTime(env.Timestamp)
			if meta.minTS.IsZero() || ts.Before(meta.minTS) {
				meta.minTS = ts
			}
			if ts.After(meta.maxTS) {
				meta.maxTS = ts
			}
			if env.CWD != "" {
				meta.session.ProjectPath = env.CWD
			}
			if env.GitBranch != "" {
				meta.session.GitBranch = env.GitBranch
			}
			meta.msgCount++
		}
		return nil
	})
	return meta, err
}

func streamFile(path string, fn func(envelope) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var env envelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		if err := fn(env); err != nil {
			return err
		}
	}
	return sc.Err()
}
