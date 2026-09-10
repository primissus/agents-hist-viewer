package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

// CondenseOptions controls how a session's messages are reduced to a
// compact, LLM-friendly transcript by Condense.
type CondenseOptions struct {
	MaxChars       int // overall budget for the condensed text, in runes
	ToolPayloadCap int // per-block rune cap for tool_use/tool_result text
	AssistantCap   int // per-block rune cap for assistant text
}

// DefaultCondenseOptions are sane defaults for Condense.
var DefaultCondenseOptions = CondenseOptions{
	MaxChars:       24000,
	ToolPayloadCap: 400,
	AssistantCap:   1500,
}

func withCondenseDefaults(opts CondenseOptions) CondenseOptions {
	d := DefaultCondenseOptions
	if opts.MaxChars > 0 {
		d.MaxChars = opts.MaxChars
	}
	if opts.ToolPayloadCap > 0 {
		d.ToolPayloadCap = opts.ToolPayloadCap
	}
	if opts.AssistantCap > 0 {
		d.AssistantCap = opts.AssistantCap
	}
	return d
}

// Condensed is the result of condensing a session's transcript.
type Condensed struct {
	Text      string
	Truncated bool
	Kept      int
	Omitted   int
}

type condensedBlock struct {
	text      string
	mandatory bool // user turns; never dropped for budget
}

const omittedMarkerFmt = "\n[... %d blocks omitted ...]\n"

// Condense builds a compact, deterministic Markdown-ish rendering of a
// session's transcript, suitable as LLM input or as a human-readable recap.
// It drops thinking blocks, images, and sidechain messages, applies
// domain.CleanText to user text, and caps tool-payload/assistant text at
// opts.ToolPayloadCap/opts.AssistantCap runes. If the result would exceed
// opts.MaxChars, it keeps 60% of the remaining budget (after the header)
// from the start and 40% from the end, with a single
// "[... N blocks omitted ...]" marker between them — except user turns,
// which are always kept regardless of budget.
func Condense(detail domain.SessionDetail, opts CondenseOptions) Condensed {
	opts = withCondenseDefaults(opts)

	msgs := make([]domain.Message, len(detail.Messages))
	copy(msgs, detail.Messages)
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Sequence < msgs[j].Sequence })

	header := condenseHeader(detail.Session)

	var blocks []condensedBlock
	for _, m := range msgs {
		if m.Kind == domain.KindThinking || m.Kind == domain.KindImage || m.IsSidechain {
			continue
		}
		blocks = append(blocks, condensedBlock{
			text:      formatCondensedBlock(m, opts),
			mandatory: m.Role == domain.RoleUser,
		})
	}

	full := header + joinBlocks(blocks)
	if runeLen(full) <= opts.MaxChars {
		return Condensed{Text: full, Truncated: false, Kept: len(blocks), Omitted: 0}
	}
	return condenseWithBudget(header, blocks, opts)
}

func condenseHeader(s domain.Session) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", s.Title)
	fmt.Fprintf(&b, "vendor: %s\n", s.Vendor)
	fmt.Fprintf(&b, "project: %s\n", s.ProjectPath)
	if s.GitBranch != "" {
		fmt.Fprintf(&b, "branch: %s\n", s.GitBranch)
	}
	fmt.Fprintf(&b, "started: %s\n", s.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "ended: %s\n", s.EndedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "messages: %d\n\n", s.MessageCount)
	return b.String()
}

func formatCondensedBlock(m domain.Message, opts CondenseOptions) string {
	label := condenseLabel(m)
	text := m.Text
	switch {
	case m.Kind == domain.KindToolUse || m.Kind == domain.KindToolResult:
		text = truncateWithMarker(text, opts.ToolPayloadCap)
	case m.Role == domain.RoleUser:
		text = domain.CleanText(text)
	case m.Role == domain.RoleAssistant:
		text = truncateWithMarker(text, opts.AssistantCap)
	}
	return fmt.Sprintf("### %s\n%s\n\n", label, text)
}

func condenseLabel(m domain.Message) string {
	switch m.Kind {
	case domain.KindToolUse:
		if m.ToolName != "" {
			return "tool_use: " + m.ToolName
		}
		return "tool_use"
	case domain.KindToolResult:
		return "tool_result"
	default:
		return string(m.Role)
	}
}

func truncateWithMarker(text string, cap int) string {
	if cap <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= cap {
		return text
	}
	return string(runes[:cap]) + " …[truncated]"
}

func joinBlocks(blocks []condensedBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.text)
	}
	return b.String()
}

func runeLen(s string) int { return len([]rune(s)) }

// condenseWithBudget applies the 60/40 start/end budget split to blocks,
// always keeping mandatory (user) blocks regardless of budget, and emits
// exactly one omitted-blocks marker in the output.
func condenseWithBudget(header string, blocks []condensedBlock, opts CondenseOptions) Condensed {
	n := len(blocks)
	selected := make([]bool, n)

	budget := opts.MaxChars - runeLen(header)
	if budget < 0 {
		budget = 0
	}
	startBudget := budget * 60 / 100
	endBudget := budget - startBudget

	frontLen := 0
	frontEnd := 0 // exclusive upper bound of the contiguous front run
	for i := 0; i < n; i++ {
		bl := runeLen(blocks[i].text)
		if blocks[i].mandatory || frontLen+bl <= startBudget {
			selected[i] = true
			frontLen += bl
			frontEnd = i + 1
			continue
		}
		break
	}

	backLen := 0
	backStart := n // inclusive lower bound of the contiguous back run
	for j := n - 1; j >= frontEnd; j-- {
		bl := runeLen(blocks[j].text)
		if blocks[j].mandatory || backLen+bl <= endBudget {
			selected[j] = true
			backLen += bl
			backStart = j
			continue
		}
		break
	}

	// Rescue mandatory (user) blocks left over in the untouched middle —
	// user turns are never dropped for budget.
	for i := frontEnd; i < backStart; i++ {
		if blocks[i].mandatory {
			selected[i] = true
		}
	}

	kept, omitted := 0, 0
	for _, s := range selected {
		if s {
			kept++
		} else {
			omitted++
		}
	}

	if omitted == 0 {
		// Nothing actually got dropped (mandatory rescue covered everything).
		return Condensed{Text: header + joinBlocks(blocks), Truncated: false, Kept: n, Omitted: 0}
	}

	var b strings.Builder
	b.WriteString(header)
	markerEmitted := false
	for i := 0; i < n; i++ {
		if selected[i] {
			b.WriteString(blocks[i].text)
			continue
		}
		if !markerEmitted {
			fmt.Fprintf(&b, omittedMarkerFmt, omitted)
			markerEmitted = true
		}
	}

	return Condensed{Text: b.String(), Truncated: true, Kept: kept, Omitted: omitted}
}

// SummarizeOptions controls SummarizeService behavior.
type SummarizeOptions struct {
	Refresh  bool // bypass cache, always re-summarize
	NoLLM    bool // condense only, no chat call, no cache read/write
	MaxChars int  // overrides DefaultCondenseOptions.MaxChars when > 0
}

// SummarizeResult is the outcome of summarizing a session.
type SummarizeResult struct {
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	Model     string    `json:"model,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Condensed string    `json:"condensed"`
	Truncated bool      `json:"truncated"`
	Cached    bool      `json:"cached"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// SummarizeService condenses a session's transcript and, unless NoLLM is
// set, asks a chat model to produce a structured recap, caching the result
// keyed by (session, chat model) so unchanged sessions aren't re-summarized.
type SummarizeService struct {
	chat      domain.ChatModel
	chatModel string
	search    domain.SearchRepository
	store     domain.SummaryRepository
}

func NewSummarizeService(chat domain.ChatModel, chatModel string, search domain.SearchRepository, store domain.SummaryRepository) *SummarizeService {
	return &SummarizeService{chat: chat, chatModel: chatModel, search: search, store: store}
}

// Summarize loads the session by ID and summarizes it.
func (s *SummarizeService) Summarize(ctx context.Context, sessionID string, opts SummarizeOptions) (SummarizeResult, error) {
	detail, err := s.search.SessionByID(ctx, sessionID)
	if err != nil {
		return SummarizeResult{}, fmt.Errorf("session %s: %w", sessionID, err)
	}
	return s.SummarizeDetail(ctx, detail, opts)
}

// SummarizeDetail condenses detail and, unless opts.NoLLM, produces (or
// retrieves a cached) chat-model summary of it.
func (s *SummarizeService) SummarizeDetail(ctx context.Context, detail domain.SessionDetail, opts SummarizeOptions) (SummarizeResult, error) {
	condOpts := DefaultCondenseOptions
	if opts.MaxChars > 0 {
		condOpts.MaxChars = opts.MaxChars
	}
	condensed := Condense(detail, condOpts)
	hash := summaryHash(condensed.Text)

	result := SummarizeResult{
		SessionID: detail.Session.ID,
		Title:     detail.Session.Title,
	}

	if opts.NoLLM {
		result.Condensed = condensed.Text
		result.Truncated = condensed.Truncated
		return result, nil
	}

	if s.store != nil {
		if err := s.store.InitSummaries(ctx); err != nil {
			return SummarizeResult{}, fmt.Errorf("init summaries: %w", err)
		}
		if !opts.Refresh {
			if existing, ok, err := s.store.GetSummary(ctx, detail.Session.ID, s.chatModel); err == nil && ok && existing.SourceHash == hash {
				result.Model = existing.Model
				result.Summary = existing.Text
				result.Condensed = condensed.Text
				result.Truncated = condensed.Truncated
				result.Cached = true
				result.CreatedAt = existing.CreatedAt
				return result, nil
			}
		}
	}

	if s.chat == nil {
		return SummarizeResult{}, fmt.Errorf("no chat model configured (use --no-llm or start Ollama)")
	}

	answer, err := s.chat.Chat(ctx, summarySystemPrompt, condensed.Text)
	if err != nil {
		return SummarizeResult{}, fmt.Errorf("chat: %w", err)
	}

	now := time.Now().UTC()
	result.Model = s.chatModel
	result.Summary = answer
	result.Condensed = condensed.Text
	result.Truncated = condensed.Truncated
	result.Cached = false
	result.CreatedAt = now

	if s.store != nil {
		if err := s.store.PutSummary(ctx, domain.Summary{
			SessionID:  detail.Session.ID,
			Model:      s.chatModel,
			SourceHash: hash,
			Text:       answer,
			CreatedAt:  now,
		}); err != nil {
			return result, fmt.Errorf("cache summary: %w", err)
		}
	}

	return result, nil
}

func summaryHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

const summarySystemPrompt = `You summarize a coding-agent conversation transcript into a concise recap.

Output exactly these five Markdown sections, in this order, and nothing else:

## Goal
## What was done
## Key decisions
## Files / areas touched
## Outcome & open items

Rules:
- Use bullet points. Be concrete — name files, commands, decisions, and outcomes when the transcript mentions them.
- Never invent facts that aren't in the transcript.
- If a section is genuinely empty, write "None noted." under its heading instead of omitting the heading.`
