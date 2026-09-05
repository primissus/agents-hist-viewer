package transcript

import (
	"encoding/json"
	"fmt"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

// envelope is the top-level Codex rollout JSONL record.
type envelope struct {
	Timestamp json.RawMessage `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// sessionMeta is the payload of a `session_meta` envelope.
type sessionMeta struct {
	ID      string      `json:"id"`
	CWD     string      `json:"cwd"`
	Git     gitInfo     `json:"git"`
	Lineage lineageInfo `json:"lineage"`
}

type gitInfo struct {
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

type lineageInfo struct {
	ParentThreadID string `json:"parent_thread_id"`
	ForkedFromID   string `json:"forked_from_id"`
}

// responseItem is the payload of a `response_item` envelope.
type responseItem struct {
	Type string `json:"type"`
	// message
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	// reasoning
	Summary json.RawMessage `json:"summary"`
	Text    string          `json:"text"`
	// function_call / custom_tool_call / function_call_output / local_shell_call
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Command   string          `json:"command"`
}

// contentItem is one element of a message content array or reasoning summary.
type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// parseSessionMeta extracts session metadata from a session_meta payload.
func parseSessionMeta(raw json.RawMessage) (sessionMeta, error) {
	var m sessionMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return sessionMeta{}, err
	}
	return m, nil
}

// parseRecord maps a single Codex envelope line to zero or more messages.
func parseRecord(rec envelope, line int, baseTS time.Time, sessionID string, toolNames map[string]string, seq *int, opts domain.IndexOptions, yield func(domain.Message) error) error {
	switch rec.Type {
	case "session_meta", "event_msg", "turn_context", "compacted":
		return nil
	case "response_item":
		return parseResponseItem(rec, line, baseTS, sessionID, toolNames, seq, opts, yield)
	default:
		return nil
	}
}

func parseResponseItem(rec envelope, line int, baseTS time.Time, sessionID string, toolNames map[string]string, seq *int, opts domain.IndexOptions, yield func(domain.Message) error) error {
	var item responseItem
	if err := json.Unmarshal(rec.Payload, &item); err != nil {
		return nil // skip bad lines
	}
	ts := envelopeTime(rec.Timestamp, line, baseTS)

	switch item.Type {
	case "message":
		return parseMessage(item, ts, sessionID, seq, opts, yield)
	case "reasoning":
		text := reasoningText(item)
		if text == "" {
			return nil
		}
		return emit(ts, sessionID, domain.RoleAssistant, domain.KindThinking, text, "", seq, opts, yield)
	case "function_call", "custom_tool_call", "local_shell_call":
		name := toolName(item)
		toolNames[item.CallID] = name
		return emit(ts, sessionID, domain.RoleAssistant, domain.KindToolUse, toolCallText(item), name, seq, opts, yield)
	case "function_call_output":
		return emit(ts, sessionID, domain.RoleAssistant, domain.KindToolResult, flattenOutput(item.Output), toolNames[item.CallID], seq, opts, yield)
	default:
		return nil
	}
}

func parseMessage(item responseItem, ts time.Time, sessionID string, seq *int, opts domain.IndexOptions, yield func(domain.Message) error) error {
	role := domain.Role(item.Role)
	if role == "" || role == "system" || role == "developer" {
		return nil
	}
	items, err := normalizeContent(item.Content)
	if err != nil {
		return err
	}
	for _, c := range items {
		if c.Text == "" {
			continue
		}
		if err := emit(ts, sessionID, role, domain.KindText, c.Text, "", seq, opts, yield); err != nil {
			return err
		}
	}
	return nil
}

// emit builds a domain.Message, applies ShouldIndex + Shrink, and yields it.
func emit(ts time.Time, sessionID string, role domain.Role, kind domain.BlockKind, text, toolName string, seq *int, opts domain.IndexOptions, yield func(domain.Message) error) error {
	if !opts.ShouldIndex(role, kind) {
		return nil
	}
	shrunk, ok := domain.Shrink(kind, text, opts.Shrink)
	if !ok {
		return nil
	}
	m := domain.Message{
		UUID:      fmt.Sprintf("%s-%d", sessionID, *seq),
		SessionID: sessionID,
		Role:      role,
		Kind:      kind,
		Text:      shrunk,
		Timestamp: ts,
		Sequence:  *seq,
		Source:    domain.SourceTranscript,
	}
	if kind == domain.KindToolUse || kind == domain.KindToolResult {
		m.ToolName = toolName
	}
	if err := yield(m); err != nil {
		return err
	}
	*seq++
	return nil
}

func normalizeContent(raw json.RawMessage) ([]contentItem, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []contentItem{{Type: "text", Text: s}}, nil
	}
	var items []contentItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func reasoningText(item responseItem) string {
	if item.Text != "" {
		return item.Text
	}
	if len(item.Summary) == 0 {
		return ""
	}
	items, err := normalizeContent(item.Summary)
	if err != nil {
		return string(item.Summary)
	}
	var out string
	for i, c := range items {
		if c.Text == "" {
			continue
		}
		if i > 0 {
			out += "\n"
		}
		out += c.Text
	}
	return out
}

func toolName(item responseItem) string {
	switch item.Type {
	case "local_shell_call":
		if item.Name != "" {
			return item.Name
		}
		return "shell"
	default:
		return item.Name
	}
}

func toolCallText(item responseItem) string {
	switch item.Type {
	case "function_call":
		return item.Arguments
	case "local_shell_call":
		if item.Command != "" {
			return item.Command
		}
		return string(item.Input)
	default:
		if len(item.Input) > 0 {
			return string(item.Input)
		}
		return item.Arguments
	}
}

func flattenOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return string(raw)
		}
		return s
	}
	return string(raw)
}

// envelopeTime resolves a message timestamp from the envelope, falling back to
// a synthesized timestamp derived from the line offset.
func envelopeTime(raw json.RawMessage, line int, baseTS time.Time) time.Time {
	if len(raw) > 0 {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && s != "" {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return t
			}
		} else {
			var n int64
			if err := json.Unmarshal(raw, &n); err == nil && n > 0 {
				if n > 1_000_000_000_000 {
					return time.UnixMilli(n).UTC()
				}
				return time.Unix(n, 0).UTC()
			}
		}
	}
	return baseTS.Add(time.Duration(line) * time.Second)
}
