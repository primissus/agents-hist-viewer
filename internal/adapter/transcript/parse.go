package transcript

import (
	"encoding/json"
	"fmt"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

type envelope struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	Timestamp   string          `json:"timestamp"`
	CWD         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	Message     *rawMessage     `json:"message"`
	AiTitle     string          `json:"aiTitle"`
	CustomTitle string          `json:"customTitle"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    *imageSource    `json:"source"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// normalizeContent converts string-or-array content to []rawBlock.
func normalizeContent(raw json.RawMessage) ([]rawBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []rawBlock{{Type: "text", Text: s}}, nil
	}
	var blocks []rawBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// blockToRaw converts a rawBlock into a (kind, rawText) pair for Shrink.
func blockToRaw(b rawBlock) (domain.BlockKind, string, error) {
	switch b.Type {
	case "text":
		return domain.KindText, b.Text, nil
	case "thinking":
		return domain.KindThinking, b.Thinking, nil
	case "image":
		return domain.KindImage, "", nil
	case "tool_use":
		input, err := json.Marshal(b.Input)
		if err != nil {
			return domain.KindToolUse, "", err
		}
		return domain.KindToolUse, fmt.Sprintf("%s %s", b.Name, string(input)), nil
	case "tool_result":
		text, err := flattenContent(b.Content)
		if err != nil {
			return domain.KindToolResult, "", err
		}
		return domain.KindToolResult, text, nil
	default:
		return domain.KindText, "", nil
	}
}

// flattenContent handles tool_result content which may be string or array of text blocks.
func flattenContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	var blocks []rawBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw), nil
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n"
		}
		result += p
	}
	return result, nil
}

// parseMessages walks an envelope's blocks, applies Shrink, and calls yield per kept block.
func parseMessages(env envelope, seq *int, opts domain.IndexOptions, yield func(domain.Message) error) error {
	if env.Message == nil {
		return nil
	}
	blocks, err := normalizeContent(env.Message.Content)
	if err != nil {
		return err
	}
	ts := parseTime(env.Timestamp)
	role := domain.Role(env.Message.Role)

	for _, b := range blocks {
		kind, raw, err := blockToRaw(b)
		if err != nil {
			continue
		}
		if !opts.ShouldIndex(role, kind) {
			continue
		}
		text, ok := domain.Shrink(kind, raw, opts.Shrink)
		if !ok {
			continue
		}
		m := domain.Message{
			UUID:        env.UUID,
			ParentUUID:  env.ParentUUID,
			SessionID:   env.SessionID,
			Role:        role,
			Kind:        kind,
			Text:        text,
			Timestamp:   ts,
			Sequence:    *seq,
			IsSidechain: env.IsSidechain,
			Source:      domain.SourceTranscript,
			ProjectPath: env.CWD,
			GitBranch:   env.GitBranch,
		}
		if kind == domain.KindToolUse {
			m.ToolName = b.Name
		}
		if err := yield(m); err != nil {
			return err
		}
		*seq++
	}
	return nil
}
