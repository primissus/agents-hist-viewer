package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/domain"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// handlers holds the tool implementations as plain methods, so they're
// directly unit-testable without going through the SDK transport.
type handlers struct{ d Deps }

func registerTools(s *sdk.Server, h *handlers) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "search_history",
		Description: "Full-text search over indexed agent history (Claude Code, Cursor, OpenCode, Codex).",
	}, h.searchHistory)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "semantic_search",
		Description: "Embedding-based nearest-neighbor search over indexed agent history. Requires `chv embed` to have run.",
	}, h.semanticSearch)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "list_sessions",
		Description: "List recent sessions, optionally filtered by vendor, project, or kind (chat vs plan).",
	}, h.listSessions)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_session",
		Description: "Fetch a session's messages, paginated and filterable by kind, with per-message text truncation.",
	}, h.getSession)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "summarize_session",
		Description: "Condense a session's transcript, optionally with an LLM-generated structured recap (cached).",
	}, h.summarizeSession)
}

// --- DTOs -------------------------------------------------------------

type hitDTO struct {
	SessionID     string  `json:"session_id"`
	Title         string  `json:"title,omitempty"`
	Vendor        string  `json:"vendor,omitempty"`
	RecordKind    string  `json:"record_kind,omitempty"`
	ProjectPath   string  `json:"project_path,omitempty"`
	FilePath      string  `json:"file_path,omitempty"`
	StartedAt     string  `json:"started_at,omitempty"`
	Timestamp     string  `json:"timestamp,omitempty"`
	Snippet       string  `json:"snippet,omitempty"`
	Score         float64 `json:"score,omitempty"`
	HasTranscript bool    `json:"has_transcript,omitempty"`
}

func hitFromSearchHit(h domain.SearchHit) hitDTO {
	return hitDTO{
		SessionID:     h.SessionID,
		Title:         h.SessionTitle,
		Vendor:        string(h.Vendor),
		RecordKind:    string(h.RecordKind),
		ProjectPath:   h.ProjectPath,
		FilePath:      h.FilePath,
		StartedAt:     formatTime(h.StartedAt),
		Timestamp:     formatTime(h.Timestamp),
		Snippet:       h.Snippet,
		Score:         h.Score,
		HasTranscript: h.HasTranscript,
	}
}

type sessionDTO struct {
	ID            string `json:"id"`
	Title         string `json:"title,omitempty"`
	Vendor        string `json:"vendor,omitempty"`
	RecordKind    string `json:"record_kind,omitempty"`
	ProjectPath   string `json:"project_path,omitempty"`
	GitBranch     string `json:"git_branch,omitempty"`
	FilePath      string `json:"file_path,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	EndedAt       string `json:"ended_at,omitempty"`
	MessageCount  int    `json:"message_count,omitempty"`
	HasTranscript bool   `json:"has_transcript,omitempty"`
}

func sessionFromDomain(s domain.Session) sessionDTO {
	return sessionDTO{
		ID:            s.ID,
		Title:         s.Title,
		Vendor:        string(s.Vendor),
		RecordKind:    string(s.RecordKind),
		ProjectPath:   s.ProjectPath,
		GitBranch:     s.GitBranch,
		FilePath:      s.FilePath,
		StartedAt:     formatTime(s.StartedAt),
		EndedAt:       formatTime(s.EndedAt),
		MessageCount:  s.MessageCount,
		HasTranscript: s.HasTranscript,
	}
}

type messageDTO struct {
	Seq       int    `json:"seq"`
	Role      string `json:"role,omitempty"`
	Kind      string `json:"kind,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Text      string `json:"text,omitempty"`
	Sidechain bool   `json:"sidechain,omitempty"`
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// clamp returns v if it's within (0, max]; def if v <= 0; max if v > max.
func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// --- search_history -----------------------------------------------------

type searchHistoryInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
	Fuzzy bool   `json:"fuzzy,omitempty"`
}

type searchHistoryOutput struct {
	Hits []hitDTO `json:"hits"`
}

func (h *handlers) searchHistory(ctx context.Context, _ *sdk.CallToolRequest, in searchHistoryInput) (*sdk.CallToolResult, searchHistoryOutput, error) {
	if h.d.Search == nil {
		return nil, searchHistoryOutput{}, errors.New("search unavailable (no search service configured)")
	}
	limit := clamp(in.Limit, 20, 100)
	hits, err := h.d.Search.Search(ctx, in.Query, limit, domain.SearchOpts{Fuzzy: in.Fuzzy})
	if err != nil {
		return nil, searchHistoryOutput{}, fmt.Errorf("search: %w", err)
	}
	out := searchHistoryOutput{Hits: make([]hitDTO, 0, len(hits))}
	for _, hit := range hits {
		out.Hits = append(out.Hits, hitFromSearchHit(hit))
	}
	return nil, out, nil
}

// --- semantic_search -----------------------------------------------------

type semanticSearchInput struct {
	Query   string `json:"query"`
	K       int    `json:"k,omitempty"`
	Vendor  string `json:"vendor,omitempty"`
	Project string `json:"project,omitempty"`
	Since   string `json:"since,omitempty"`
}

type semanticSearchOutput struct {
	Hits []hitDTO `json:"hits"`
}

func (h *handlers) semanticSearch(ctx context.Context, _ *sdk.CallToolRequest, in semanticSearchInput) (*sdk.CallToolResult, semanticSearchOutput, error) {
	if h.d.Semantic == nil {
		return nil, semanticSearchOutput{}, errors.New("semantic search unavailable (is Ollama running?)")
	}

	since, err := app.SinceTime(in.Since, time.Now())
	if err != nil {
		return nil, semanticSearchOutput{}, fmt.Errorf("since: %w", err)
	}

	k := clamp(in.K, 12, 50)
	hits, err := h.d.Semantic.Search(ctx, in.Query, app.SemanticSearchOpts{
		Limit:       k,
		Vendor:      domain.Vendor(in.Vendor),
		ProjectPath: in.Project,
		Since:       since,
	})
	if err != nil {
		if errors.Is(err, app.ErrNoEmbeddings) {
			return nil, semanticSearchOutput{}, err
		}
		if h.d.OllamaURL != "" {
			return nil, semanticSearchOutput{}, fmt.Errorf("semantic search unavailable (is Ollama running at %s?): %w", h.d.OllamaURL, err)
		}
		return nil, semanticSearchOutput{}, fmt.Errorf("semantic search unavailable (is Ollama running?): %w", err)
	}

	out := semanticSearchOutput{Hits: make([]hitDTO, 0, len(hits))}
	for _, hit := range hits {
		out.Hits = append(out.Hits, hitFromSearchHit(hit))
	}
	return nil, out, nil
}

// --- list_sessions -----------------------------------------------------

type listSessionsInput struct {
	Limit   int    `json:"limit,omitempty"`
	Vendor  string `json:"vendor,omitempty"`
	Project string `json:"project,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

type listSessionsOutput struct {
	Sessions []hitDTO `json:"sessions"`
}

func (h *handlers) listSessions(ctx context.Context, _ *sdk.CallToolRequest, in listSessionsInput) (*sdk.CallToolResult, listSessionsOutput, error) {
	if h.d.Search == nil {
		return nil, listSessionsOutput{}, errors.New("list_sessions unavailable (no search service configured)")
	}
	limit := clamp(in.Limit, 20, 200)
	hits, err := h.d.Search.RecentSessions(ctx, domain.RecentQuery{
		Limit:       limit,
		RecordKind:  domain.RecordKind(in.Kind),
		ProjectPath: in.Project,
		Vendor:      domain.Vendor(in.Vendor),
	})
	if err != nil {
		return nil, listSessionsOutput{}, fmt.Errorf("list_sessions: %w", err)
	}
	out := listSessionsOutput{Sessions: make([]hitDTO, 0, len(hits))}
	for _, hit := range hits {
		out.Sessions = append(out.Sessions, hitFromSearchHit(hit))
	}
	return nil, out, nil
}

// --- get_session -----------------------------------------------------

var defaultGetSessionKinds = []string{"text", "tool_use", "tool_result"}

type getSessionInput struct {
	SessionID        string   `json:"session_id"`
	Offset           int      `json:"offset,omitempty"`
	Limit            int      `json:"limit,omitempty"`
	Kinds            []string `json:"kinds,omitempty"`
	IncludeSidechain bool     `json:"include_sidechain,omitempty"`
	MaxChars         int      `json:"max_chars,omitempty"`
}

type getSessionOutput struct {
	Session    sessionDTO   `json:"session"`
	Total      int          `json:"total"`
	Offset     int          `json:"offset"`
	Returned   int          `json:"returned"`
	HasMore    bool         `json:"has_more"`
	NextOffset int          `json:"next_offset,omitempty"`
	Messages   []messageDTO `json:"messages"`
}

func (h *handlers) getSession(ctx context.Context, _ *sdk.CallToolRequest, in getSessionInput) (*sdk.CallToolResult, getSessionOutput, error) {
	if h.d.Search == nil {
		return nil, getSessionOutput{}, errors.New("get_session unavailable (no search service configured)")
	}

	detail, err := h.d.Search.Session(ctx, in.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, getSessionOutput{}, fmt.Errorf("session %q not found", in.SessionID)
		}
		return nil, getSessionOutput{}, fmt.Errorf("get_session: %w", err)
	}

	kinds := in.Kinds
	if len(kinds) == 0 {
		kinds = defaultGetSessionKinds
	}
	kindSet := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		kindSet[k] = true
	}

	var filtered []domain.Message
	for _, m := range detail.Messages {
		if !kindSet[string(m.Kind)] {
			continue
		}
		if m.IsSidechain && !in.IncludeSidechain {
			continue
		}
		filtered = append(filtered, m)
	}

	total := len(filtered)
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	limit := clamp(in.Limit, 50, 200)
	maxChars := clamp(in.MaxChars, 2000, 20000)

	var page []domain.Message
	if offset < total {
		end := offset + limit
		if end > total {
			end = total
		}
		page = filtered[offset:end]
	}

	out := getSessionOutput{
		Session:  sessionFromDomain(detail.Session),
		Total:    total,
		Offset:   offset,
		Returned: len(page),
		Messages: make([]messageDTO, 0, len(page)),
	}
	if offset+len(page) < total {
		out.HasMore = true
		out.NextOffset = offset + len(page)
	}

	for _, m := range page {
		out.Messages = append(out.Messages, messageDTO{
			Seq:       m.Sequence,
			Role:      string(m.Role),
			Kind:      string(m.Kind),
			ToolName:  m.ToolName,
			Timestamp: formatTime(m.Timestamp),
			Text:      truncateRunes(m.Text, maxChars),
			Sidechain: m.IsSidechain,
		})
	}

	return nil, out, nil
}

// truncateRunes caps text at maxChars runes, appending a truncation marker
// if it was cut.
func truncateRunes(text string, maxChars int) string {
	if maxChars <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + " …[truncated]"
}

// --- summarize_session -----------------------------------------------------

type summarizeSessionInput struct {
	SessionID     string `json:"session_id"`
	Refresh       bool   `json:"refresh,omitempty"`
	CondensedOnly bool   `json:"condensed_only,omitempty"`
	MaxChars      int    `json:"max_chars,omitempty"`
}

type summarizeSessionOutput struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title,omitempty"`
	Model     string `json:"model,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Cached    bool   `json:"cached,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Condensed string `json:"condensed,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (h *handlers) summarizeSession(ctx context.Context, _ *sdk.CallToolRequest, in summarizeSessionInput) (*sdk.CallToolResult, summarizeSessionOutput, error) {
	if h.d.Summarize == nil {
		if !in.CondensedOnly {
			return nil, summarizeSessionOutput{}, errors.New("summarize_session unavailable (no chat model configured; pass condensed_only for condense-only mode)")
		}
		// No SummarizeService is configured at all, but condensing a
		// transcript doesn't need a chat model — fall back to Condense
		// directly via the search service.
		if h.d.Search == nil {
			return nil, summarizeSessionOutput{}, errors.New("summarize_session unavailable (no search service configured)")
		}
		detail, err := h.d.Search.Session(ctx, in.SessionID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, summarizeSessionOutput{}, fmt.Errorf("session %q not found", in.SessionID)
			}
			return nil, summarizeSessionOutput{}, fmt.Errorf("summarize_session: %w", err)
		}
		condensed := app.Condense(detail, app.CondenseOptions{MaxChars: in.MaxChars})
		return nil, summarizeSessionOutput{
			SessionID: detail.Session.ID,
			Title:     detail.Session.Title,
			Condensed: condensed.Text,
			Truncated: condensed.Truncated,
		}, nil
	}

	result, err := h.d.Summarize.Summarize(ctx, in.SessionID, app.SummarizeOptions{
		Refresh:  in.Refresh,
		NoLLM:    in.CondensedOnly,
		MaxChars: in.MaxChars,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, summarizeSessionOutput{}, fmt.Errorf("session %q not found", in.SessionID)
		}
		return nil, summarizeSessionOutput{}, fmt.Errorf("summarize_session: %w", err)
	}

	out := summarizeSessionOutput{
		SessionID: result.SessionID,
		Title:     result.Title,
		Model:     result.Model,
		Summary:   result.Summary,
		Cached:    result.Cached,
		Condensed: result.Condensed,
		Truncated: result.Truncated,
	}
	if !result.CreatedAt.IsZero() {
		out.CreatedAt = result.CreatedAt.Format(time.RFC3339)
	}
	return nil, out, nil
}
