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
	"sync"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

// Source implements domain.TranscriptSource over Codex rollout JSONL files.
type Source struct {
	root string
	opts domain.IndexOptions

	mu    sync.Mutex
	index map[string]string // session id -> file path
}

func NewSource(root string, opts domain.IndexOptions) *Source {
	return &Source{root: root, opts: opts}
}

func SessionID(id string) string { return "codex:" + id }

// LoadFile builds a SessionDetail directly from a single rollout JSONL file.
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

	meta, err := scanSessionMeta(filePath)
	if err != nil {
		return domain.SessionDetail{}, err
	}
	sessionID := SessionID(meta.ID)
	sess := sessionFromMeta(meta, filePath, info.ModTime().UTC())
	sess.Title = scanTitle(filePath)

	seq := 0
	toolNames := make(map[string]string)
	var messages []domain.Message
	if err := streamFile(filePath, func(rec envelope, line int) error {
		if rec.Type == "session_meta" {
			return nil
		}
		return parseRecord(rec, line, info.ModTime().UTC(), sessionID, toolNames, &seq, opts, func(m domain.Message) error {
			m.ProjectPath = meta.CWD
			messages = append(messages, m)
			return nil
		})
	}); err != nil {
		return domain.SessionDetail{}, err
	}

	sess.MessageCount = len(messages)
	return domain.SessionDetail{Session: sess, Messages: messages}, nil
}

func (s *Source) Sessions(ctx context.Context) ([]domain.Session, error) {
	files, err := s.rolloutFiles()
	if err != nil {
		return nil, err
	}

	var sessions []domain.Session
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		meta, err := scanSessionMeta(path)
		if err != nil {
			continue
		}
		if meta.ID == "" {
			continue
		}
		sess := sessionFromMeta(meta, path, info.ModTime().UTC())
		sess.Title = scanTitle(path)
		sessions = append(sessions, sess)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})
	return sessions, nil
}

func (s *Source) Messages(ctx context.Context, sessionID string, yield func(domain.Message) error) error {
	id := strings.TrimPrefix(sessionID, "codex:")
	filePath, err := s.findFile(id)
	if err != nil {
		return err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	meta, err := scanSessionMeta(filePath)
	if err != nil {
		return err
	}

	seq := 0
	toolNames := make(map[string]string)
	return streamFile(filePath, func(rec envelope, line int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rec.Type == "session_meta" {
			return nil
		}
		return parseRecord(rec, line, info.ModTime().UTC(), sessionID, toolNames, &seq, s.opts, func(m domain.Message) error {
			m.ProjectPath = meta.CWD
			return yield(m)
		})
	})
}

func (s *Source) TranscriptFingerprint(ctx context.Context, sessionID string) (domain.TranscriptFingerprint, error) {
	if err := ctx.Err(); err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	id := strings.TrimPrefix(sessionID, "codex:")
	filePath, err := s.findFile(id)
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	return fingerprintForFile(filePath, s.opts)
}

func (s *Source) rolloutFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	sort.Strings(files)
	return files, err
}

func (s *Source) findFile(id string) (string, error) {
	s.mu.Lock()
	if s.index == nil {
		s.index = make(map[string]string)
		files, err := s.rolloutFiles()
		if err == nil {
			for _, path := range files {
				meta, err := scanSessionMeta(path)
				if err != nil || meta.ID == "" {
					continue
				}
				if _, ok := s.index[meta.ID]; !ok {
					s.index[meta.ID] = path
				}
			}
		}
	}
	path, ok := s.index[id]
	s.mu.Unlock()

	if !ok {
		return "", os.ErrNotExist
	}
	return path, nil
}

func sessionFromMeta(meta sessionMeta, filePath string, mod time.Time) domain.Session {
	projectPath := meta.CWD
	branch := meta.Git.Branch
	return domain.Session{
		ID:            SessionID(meta.ID),
		ProjectPath:   projectPath,
		GitBranch:     branch,
		FilePath:      filePath,
		HasTranscript: true,
		Vendor:        domain.VendorCodex,
		RecordKind:    domain.RecordChat,
		StartedAt:     mod,
		EndedAt:       mod,
	}
}

// scanSessionMeta reads only the first non-empty line of a rollout file and
// returns its session_meta payload.
func scanSessionMeta(filePath string) (sessionMeta, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return sessionMeta{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var rec envelope
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		if rec.Type == "session_meta" {
			return parseSessionMeta(rec.Payload)
		}
	}
	if err := sc.Err(); err != nil {
		return sessionMeta{}, err
	}
	return sessionMeta{}, os.ErrNotExist
}

// scanTitle derives a display title from the first user text message.
func scanTitle(filePath string) string {
	title := ""
	_ = streamFile(filePath, func(rec envelope, line int) error {
		if title != "" {
			return errStop
		}
		if rec.Type != "response_item" {
			return nil
		}
		var item responseItem
		if err := json.Unmarshal(rec.Payload, &item); err != nil {
			return nil
		}
		if item.Type != "message" || item.Role != string(domain.RoleUser) {
			return nil
		}
		items, err := normalizeContent(item.Content)
		if err != nil {
			return nil
		}
		for _, c := range items {
			if c.Text != "" {
				title = domain.DeriveTitle(c.Text, 60)
				return errStop
			}
		}
		return nil
	})
	return title
}

var errStop = fmt.Errorf("stop")

func fingerprintForFile(path string, opts domain.IndexOptions) (domain.TranscriptFingerprint, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return domain.TranscriptFingerprint{}, err
	}

	var manifest strings.Builder
	fmt.Fprintf(&manifest, "codex-transcript-meta-v1\n")
	fmt.Fprintf(&manifest, "depth=%d\n", opts.Depth)
	fmt.Fprintf(&manifest, "shrink_enabled=%t\n", opts.Shrink.Enabled)
	fmt.Fprintf(&manifest, "shrink_cap=%d\n", opts.Shrink.ToolPayloadCap)
	fmt.Fprintf(&manifest, "drop_images=%t\n", opts.Shrink.DropImages)
	fmt.Fprintf(&manifest, "%s\t%d\t%d\n", absPath, info.Size(), info.ModTime().UTC().UnixNano())
	sum := sha256.Sum256([]byte(manifest.String()))
	return domain.TranscriptFingerprint{
		Path: absPath,
		Hash: "codex-transcript-meta-v1:" + hex.EncodeToString(sum[:]),
	}, nil
}

func streamFile(path string, fn func(envelope, int) error) error {
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
		var rec envelope
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		if err := fn(rec, line); err != nil {
			if err == errStop {
				return nil
			}
			return err
		}
		line++
	}
	return sc.Err()
}
