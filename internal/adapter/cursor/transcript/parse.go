package transcript

import (
	"encoding/json"
	"fmt"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

type record struct {
	Role    string      `json:"role"`
	Message *rawMessage `json:"message"`
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
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

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

func parseRecord(rec record, line int, baseTS time.Time, sessionID string, isSidechain bool, seq *int, opts domain.IndexOptions, yield func(domain.Message) error) error {
	if rec.Message == nil {
		return nil
	}
	blocks, err := normalizeContent(rec.Message.Content)
	if err != nil {
		return err
	}
	role := domain.Role(rec.Role)
	if role == "" {
		role = domain.Role(rec.Message.Role)
	}
	ts := baseTS.Add(time.Duration(line) * time.Second)

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
			UUID:        fmt.Sprintf("%s-%d-%d", sessionID, line, *seq),
			SessionID:   sessionID,
			Role:        role,
			Kind:        kind,
			Text:        text,
			Timestamp:   ts,
			Sequence:    *seq,
			IsSidechain: isSidechain,
			Source:      domain.SourceTranscript,
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

func firstUserTitle(rec record) string {
	if rec.Role != string(domain.RoleUser) || rec.Message == nil {
		return ""
	}
	blocks, err := normalizeContent(rec.Message.Content)
	if err != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return domain.DeriveTitle(b.Text, 60)
		}
	}
	return ""
}
