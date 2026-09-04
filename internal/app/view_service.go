package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-code-hist-viewer/internal/domain"
)

type ViewFormat string

const (
	ViewFormatAuto   ViewFormat = "auto"
	ViewFormatClaude ViewFormat = "claude"
	ViewFormatCursor ViewFormat = "cursor"
)

type TranscriptFileLoader func(context.Context, string, domain.IndexOptions) (domain.SessionDetail, error)

type ViewService struct {
	claudeLoader TranscriptFileLoader
	cursorLoader TranscriptFileLoader
	opts         domain.IndexOptions
}

func NewViewService(claudeLoader, cursorLoader TranscriptFileLoader, opts domain.IndexOptions) *ViewService {
	return &ViewService{
		claudeLoader: claudeLoader,
		cursorLoader: cursorLoader,
		opts:         opts,
	}
}

func ParseViewFormat(s string) (ViewFormat, error) {
	switch ViewFormat(strings.ToLower(strings.TrimSpace(s))) {
	case "", ViewFormatAuto:
		return ViewFormatAuto, nil
	case ViewFormatClaude:
		return ViewFormatClaude, nil
	case ViewFormatCursor:
		return ViewFormatCursor, nil
	default:
		return "", fmt.Errorf("unsupported format %q (want auto, claude, or cursor)", s)
	}
}

func (s *ViewService) LoadJSONL(ctx context.Context, filePath string, format ViewFormat) (domain.SessionDetail, error) {
	if err := ctx.Err(); err != nil {
		return domain.SessionDetail{}, err
	}
	if filepath.Ext(filePath) != ".jsonl" {
		return domain.SessionDetail{}, fmt.Errorf("view expects a .jsonl file: %s", filePath)
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return domain.SessionDetail{}, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return domain.SessionDetail{}, err
	}
	if info.IsDir() {
		return domain.SessionDetail{}, fmt.Errorf("%s is a directory", absPath)
	}

	if format == ViewFormatAuto {
		format, err = detectJSONLFormat(absPath)
		if err != nil {
			return domain.SessionDetail{}, err
		}
	}

	switch format {
	case ViewFormatClaude:
		if s.claudeLoader == nil {
			return domain.SessionDetail{}, fmt.Errorf("claude loader is not configured")
		}
		return s.claudeLoader(ctx, absPath, s.opts)
	case ViewFormatCursor:
		if s.cursorLoader == nil {
			return domain.SessionDetail{}, fmt.Errorf("cursor loader is not configured")
		}
		return s.cursorLoader(ctx, absPath, s.opts)
	default:
		return domain.SessionDetail{}, fmt.Errorf("unsupported format %q", format)
	}
}

func detectJSONLFormat(filePath string) (ViewFormat, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var probe struct {
			Type      json.RawMessage `json:"type"`
			SessionID string          `json:"sessionId"`
			Role      string          `json:"role"`
			Message   json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		if probe.SessionID != "" || len(probe.Type) > 0 {
			return ViewFormatClaude, nil
		}
		if probe.Role != "" && len(probe.Message) > 0 {
			return ViewFormatCursor, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("could not detect transcript format for %s", filePath)
}
