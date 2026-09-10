// Package mcp is a driving adapter that exposes chv's search/summarize
// capabilities as an MCP (Model Context Protocol) stdio server. It imports
// internal/app; internal/app never imports this package.
package mcp

import (
	"context"
	"log/slog"

	"claude-code-hist-viewer/internal/app"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Deps bundles the application services the MCP tools call into. Semantic
// and Summarize may be nil (e.g. Ollama not configured); tools that need
// them return a clear tool error instead of panicking.
type Deps struct {
	Search    *app.SearchService
	Semantic  *app.SemanticSearchService
	Summarize *app.SummarizeService
	Version   string

	// OllamaURL, if set, is included in semantic-search error messages to
	// help the caller diagnose a down/misconfigured Ollama server.
	OllamaURL string

	// Logger, if non-nil, enables logging of server activity (must write to
	// stderr, never stdout — stdout is the MCP protocol channel).
	Logger *slog.Logger
}

const serverInstructions = `chv exposes search over indexed Claude Code, Cursor, OpenCode, and Codex
agent history.

Tools:
  - search_history: full-text (FTS5) search over indexed sessions.
  - semantic_search: embedding-based nearest-neighbor search (requires chv embed to have run).
  - list_sessions: browse recent sessions, optionally filtered by vendor/project/kind.
  - get_session: fetch a session's messages, paginated and filterable by kind.
  - summarize_session: condense a session's transcript, optionally with an LLM-generated recap.`

// NewServer builds an MCP server with all chv tools registered.
func NewServer(d Deps) *sdk.Server {
	version := d.Version
	if version == "" {
		version = "dev"
	}
	s := sdk.NewServer(&sdk.Implementation{
		Name:    "chv",
		Version: version,
	}, &sdk.ServerOptions{
		Instructions: serverInstructions,
		Logger:       d.Logger,
	})

	h := &handlers{d: d}
	registerTools(s, h)
	return s
}

// Run starts an MCP server over stdio and blocks until ctx is cancelled or
// the client disconnects. stdout is the protocol channel: nothing but the
// SDK transport may write to it on this code path.
func Run(ctx context.Context, d Deps) error {
	return NewServer(d).Run(ctx, &sdk.StdioTransport{})
}
