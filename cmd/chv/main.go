package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	desktoptranscript "claude-code-hist-viewer/internal/adapter/claudedesktop/transcript"
	codextranscript "claude-code-hist-viewer/internal/adapter/codex/transcript"
	cursortranscript "claude-code-hist-viewer/internal/adapter/cursor/transcript"
	"claude-code-hist-viewer/internal/adapter/history"
	mcpadapter "claude-code-hist-viewer/internal/adapter/mcp"
	"claude-code-hist-viewer/internal/adapter/ollama"
	opencodetranscript "claude-code-hist-viewer/internal/adapter/opencode/transcript"
	"claude-code-hist-viewer/internal/adapter/plan"
	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/adapter/transcript"
	"claude-code-hist-viewer/internal/adapter/tui"
	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/config"
	"claude-code-hist-viewer/internal/domain"

	tea "github.com/charmbracelet/bubbletea"
)

// version is the chv build version reported to MCP clients. It is overridden
// at release time via -ldflags "-X main.version=<tag>" (see .goreleaser.yml);
// local builds report the default below.
var version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		launchTUI()
		return
	}
	switch os.Args[1] {
	case "index", "i":
		cmdIndex(os.Args[2:])
	case "search":
		cmdSearch(os.Args[2:])
	case "view", "v":
		cmdView(os.Args[2:])
	case "embed":
		cmdEmbed(os.Args[2:])
	case "ask":
		cmdAsk(os.Args[2:])
	case "patterns":
		cmdPatterns(os.Args[2:])
	case "summarize":
		cmdSummarize(os.Args[2:])
	case "mcp":
		cmdMCP(os.Args[2:])
	default:
		launchTUI()
	}
}

func loadIndexConfigOrExit() config.IndexConfig {
	indexCfg, err := config.LoadIndexConfig(config.IndexConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	return indexCfg
}

// ollamaFlags bundles the --model, --chat, and --ollama flags shared by the
// embed, ask, and patterns subcommands.
type ollamaFlags struct{ model, chat, url *string }

// addOllamaFlags registers --model, --chat, and --ollama on fs.
func addOllamaFlags(fs *flag.FlagSet) ollamaFlags {
	return ollamaFlags{
		model: fs.String("model", "", "embedding model (default: config or mxbai-embed-large)"),
		chat:  fs.String("chat", "", "chat model (default: config)"),
		url:   fs.String("ollama", "", "Ollama base URL (default: config or http://localhost:11434)"),
	}
}

// resolve resolves the flags against cfg defaults and constructs an Ollama
// client, returning the resolved embedding and chat model names alongside it.
func (f ollamaFlags) resolve(cfg config.IndexConfig) (client *ollama.Client, embedModel, chatModel string) {
	embedModel = *f.model
	if embedModel == "" {
		embedModel = cfg.EmbedModelOrDefault()
	}
	chatModel = *f.chat
	if chatModel == "" {
		chatModel = cfg.ChatModelOrDefault()
	}
	ollamaURL := *f.url
	if ollamaURL == "" {
		ollamaURL = cfg.OllamaURLOrDefault()
	}
	client = ollama.New(ollamaURL, embedModel, chatModel)
	return client, embedModel, chatModel
}

// resolveOllamaURL mirrors the URL-resolution half of ollamaFlags.resolve,
// for callers that need the URL string without constructing a client.
func resolveOllamaURL(f ollamaFlags, cfg config.IndexConfig) string {
	if *f.url != "" {
		return *f.url
	}
	return cfg.OllamaURLOrDefault()
}

func openDBOrExit(dbFlag string, requireExisting bool) *sqlite.Repo {
	dbPath := dbFlag
	if dbPath == "" {
		dbPath = config.DBPath()
	}
	if requireExisting {
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "DB not found — run `chv index` first")
			os.Exit(1)
		}
	}
	repo, err := sqlite.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	return repo
}

func cmdEmbed(args []string) {
	fs := flag.NewFlagSet("embed", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file (default: XDG)")
	ollamaFlags := addOllamaFlags(fs)
	batchFlag := fs.Int("batch", 32, "embed batch size")
	vendorFlag := fs.String("vendor", "", "filter by vendor")
	projectFlag := fs.String("project", "", "filter by project path")
	forceFlag := fs.Bool("force", false, "re-embed even if already embedded")
	limitFlag := fs.Int("limit", 0, "max units to embed (0 = no limit)")
	fs.Parse(args)

	indexCfg := loadIndexConfigOrExit()
	repo := openDBOrExit(*dbFlag, true)
	defer repo.Close()

	client, _, _ := ollamaFlags.resolve(indexCfg)

	svc := app.NewEmbedService(client, repo)
	stats, err := svc.Run(context.Background(), app.EmbedOptions{
		Vendor:      domain.Vendor(*vendorFlag),
		ProjectPath: *projectFlag,
		Force:       *forceFlag,
		Limit:       *limitFlag,
		BatchSize:   *batchFlag,
	}, func(processed int) {
		fmt.Fprintf(os.Stderr, "\rEmbedded %d...", processed)
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "embed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Embedded: %d  Skipped: %d\n", stats.Embedded, stats.Skipped)
}

func cmdAsk(args []string) {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file")
	ollamaFlags := addOllamaFlags(fs)
	kFlag := fs.Int("k", 12, "number of hits")
	vendorFlag := fs.String("vendor", "", "filter by vendor")
	projectFlag := fs.String("project", "", "filter by project path")
	sinceFlag := fs.String("since", "", "only messages since duration ago, e.g. 30d")
	noLLMFlag := fs.Bool("no-llm", false, "semantic search only, no chat model")
	jsonFlag := fs.Bool("json", false, "output JSON")
	fs.Parse(args)

	question := strings.Join(fs.Args(), " ")
	if question == "" {
		fmt.Fprintln(os.Stderr, "usage: chv ask <question>")
		os.Exit(1)
	}

	indexCfg := loadIndexConfigOrExit()
	repo := openDBOrExit(*dbFlag, true)
	defer repo.Close()

	client, _, _ := ollamaFlags.resolve(indexCfg)

	since, err := app.SinceTime(*sinceFlag, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ask: %v\n", err)
		os.Exit(1)
	}

	var chatSvc domain.ChatModel = client
	if *noLLMFlag {
		chatSvc = nil
	}
	svc := app.NewAskService(client, chatSvc, repo, repo)
	result, err := svc.Ask(context.Background(), question, app.AskOptions{
		K: *kFlag, Vendor: domain.Vendor(*vendorFlag), ProjectPath: *projectFlag, Since: since, NoLLM: *noLLMFlag,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ask: %v\n", err)
		os.Exit(1)
	}

	if *jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
		return
	}

	if result.Answer != "" {
		fmt.Println(result.Answer)
		fmt.Println()
	}
	for _, h := range result.Hits {
		fmt.Printf("[%d] %s  %s (%s)  score=%.3f\n  %s\n\n",
			h.Num, h.SessionID, h.SessionTitle, domain.VendorLabel(h.Vendor), h.Score, h.Snippet)
	}
}

// cmdSummarize condenses a session's transcript and, unless --no-llm, asks
// the chat model for a structured recap. The positional arg is either a
// session ID (looked up in the DB) or a path ending in .jsonl (loaded
// directly, no indexing). Summaries print to stdout; status lines go to
// stderr so a downstream consumer parsing stdout gets clean output.
func cmdSummarize(args []string) {
	fs := flag.NewFlagSet("summarize", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file (default: XDG)")
	ollamaFlags := addOllamaFlags(fs)
	refreshFlag := fs.Bool("refresh", false, "bypass the cached summary and regenerate")
	noLLMFlag := fs.Bool("no-llm", false, "condense only, no chat model call")
	jsonFlag := fs.Bool("json", false, "output JSON")
	maxCharsFlag := fs.Int("max-chars", 0, "condensed-text budget in runes (0 = default)")
	formatFlag := fs.String("format", "auto", "transcript format for a .jsonl arg: auto, claude, cursor, or codex")
	fs.Parse(args)

	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: chv summarize [flags] <session-id | path.jsonl>")
		os.Exit(1)
	}
	arg := fs.Arg(0)

	indexCfg := loadIndexConfigOrExit()
	client, _, chatModel := ollamaFlags.resolve(indexCfg)
	var chatSvc domain.ChatModel = client
	if *noLLMFlag {
		chatSvc = nil
	}

	opts := app.SummarizeOptions{Refresh: *refreshFlag, NoLLM: *noLLMFlag, MaxChars: *maxCharsFlag}

	var result app.SummarizeResult
	var err error

	if strings.HasSuffix(arg, ".jsonl") {
		format, ferr := app.ParseViewFormat(*formatFlag)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "summarize: %v\n", ferr)
			os.Exit(1)
		}
		viewOpts := domain.IndexOptions{
			Shrink: domain.ShrinkConfig{Enabled: true, ToolPayloadCap: 2000, DropImages: true},
			Depth:  domain.IndexDepthDeep,
		}
		viewSvc := app.NewViewServiceWithCodex(transcript.LoadFile, cursortranscript.LoadFile, codextranscript.LoadFile, viewOpts)
		detail, verr := viewSvc.LoadJSONL(context.Background(), arg, format)
		if verr != nil {
			fmt.Fprintf(os.Stderr, "summarize: %v\n", verr)
			os.Exit(1)
		}

		// Only use the DB-backed cache if a DB already exists on disk —
		// never create one as a side effect of summarizing a raw file.
		dbPath := *dbFlag
		if dbPath == "" {
			dbPath = config.DBPath()
		}
		var store domain.SummaryRepository
		if _, statErr := os.Stat(dbPath); statErr == nil {
			repo, openErr := sqlite.Open(dbPath)
			if openErr != nil {
				fmt.Fprintf(os.Stderr, "open db: %v\n", openErr)
				os.Exit(1)
			}
			defer repo.Close()
			store = repo
		}

		svc := app.NewSummarizeService(chatSvc, chatModel, nil, store)
		result, err = svc.SummarizeDetail(context.Background(), detail, opts)
	} else {
		repo := openDBOrExit(*dbFlag, true)
		defer repo.Close()
		svc := app.NewSummarizeService(chatSvc, chatModel, repo, repo)
		result, err = svc.Summarize(context.Background(), arg, opts)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "summarize: %v\n", err)
		os.Exit(1)
	}

	if *jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
		return
	}

	if *noLLMFlag {
		fmt.Println(result.Condensed)
		fmt.Fprintf(os.Stderr, "(condensed only, no chat model)\n")
		return
	}

	fmt.Println(result.Summary)
	status := "model " + result.Model
	if result.Cached {
		status = "cached · model " + result.Model
	}
	if !result.CreatedAt.IsZero() {
		status += " · " + result.CreatedAt.Format("2006-01-02")
	}
	fmt.Fprintf(os.Stderr, "(%s)\n", status)
}

// cmdMCP runs chv as an MCP stdio server, exposing search/summarize tools
// to MCP clients. stdout is the protocol channel: nothing but the SDK
// transport may write to it, so all diagnostics go to stderr.
func cmdMCP(args []string) {
	// Defensive: any stdlib `log` call reachable from this path must go to
	// stderr, never stdout, since stdout is the MCP protocol channel.
	log.SetOutput(os.Stderr)

	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file (default: XDG)")
	ollamaFlags := addOllamaFlags(fs)
	noLLMFlag := fs.Bool("no-llm", false, "don't construct a chat-capable summarizer; only condensed_only summarize calls work")
	debugFlag := fs.Bool("debug", false, "log server activity to stderr")
	fs.Parse(args)

	indexCfg := loadIndexConfigOrExit()
	repo := openDBOrExit(*dbFlag, true)
	defer repo.Close()

	client, _, chatModel := ollamaFlags.resolve(indexCfg)

	var chatSvc domain.ChatModel = client
	if *noLLMFlag {
		chatSvc = nil
	}

	deps := mcpadapter.Deps{
		Search:    app.NewSearchService(repo),
		Semantic:  app.NewSemanticSearchService(client, repo, repo),
		Summarize: app.NewSummarizeService(chatSvc, chatModel, repo, repo),
		Version:   version,
		OllamaURL: resolveOllamaURL(ollamaFlags, indexCfg),
	}
	if *debugFlag {
		deps.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcpadapter.Run(ctx, deps); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "mcp: %v\n", err)
		os.Exit(1)
	}
}

func cmdPatterns(args []string) {
	fs := flag.NewFlagSet("patterns", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file")
	ollamaFlags := addOllamaFlags(fs)
	kindFlag := fs.String("kind", "all", "skills|scripts|all")
	kFlag := fs.Int("k", 0, "cluster count (0 = auto)")
	topFlag := fs.Int("top", 15, "max candidates per kind")
	vendorFlag := fs.String("vendor", "", "filter by vendor")
	projectFlag := fs.String("project", "", "filter by project path")
	sinceFlag := fs.String("since", "", "only messages since duration ago, e.g. 30d")
	outFlag := fs.String("out", "", "write candidate files to this directory")
	jsonFlag := fs.Bool("json", false, "output JSON")
	noLLMFlag := fs.Bool("no-llm", false, "skip chat-model labeling")
	fs.Parse(args)

	indexCfg := loadIndexConfigOrExit()
	repo := openDBOrExit(*dbFlag, true)
	defer repo.Close()

	client, model, _ := ollamaFlags.resolve(indexCfg)
	var chatSvc domain.ChatModel
	if !*noLLMFlag {
		chatSvc = client
	}

	since, err := app.SinceTime(*sinceFlag, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "patterns: %v\n", err)
		os.Exit(1)
	}

	svc := app.NewPatternService(repo, chatSvc)
	report, err := svc.Run(context.Background(), model, app.PatternOptions{
		Kind:        app.PatternKind(*kindFlag),
		K:           *kFlag,
		Top:         *topFlag,
		Vendor:      domain.Vendor(*vendorFlag),
		ProjectPath: *projectFlag,
		Since:       since,
		NoLLM:       *noLLMFlag,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "patterns: %v\n", err)
		os.Exit(1)
	}

	if *outFlag != "" {
		if err := app.WritePatternReportFiles(*outFlag, report); err != nil {
			fmt.Fprintf(os.Stderr, "patterns: write files: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote %d skill and %d script candidates to %s\n", len(report.Skills), len(report.Scripts), *outFlag)
		return
	}

	if *jsonFlag {
		if err := app.RenderPatternReportJSON(os.Stdout, report); err != nil {
			fmt.Fprintf(os.Stderr, "patterns: %v\n", err)
			os.Exit(1)
		}
		return
	}

	app.RenderPatternReportMarkdown(os.Stdout, report)
}

func cmdIndex(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file (default: XDG)")
	configFlag := fs.String("config", "", "path to index config JSON (default: XDG)")
	shrinkCap := fs.Int("shrink-cap", 2000, "rune cap for tool payloads")
	noShrink := fs.Bool("no-shrink", false, "disable shrink transform")
	quickFlag := fs.Bool("quick", false, "index user text, tool results, and plans only")
	deepFlag := fs.Bool("deep", false, "index all conversation content (default)")
	forceFlag := fs.Bool("force", false, "re-index even if content unchanged")
	debugFlag := fs.Bool("debug", false, "print indexing diagnostics to stderr")
	fs.Parse(args)

	if *quickFlag && *deepFlag {
		fmt.Fprintln(os.Stderr, "index: --quick and --deep are mutually exclusive")
		os.Exit(1)
	}

	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = config.DBPath()
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}

	configPath := *configFlag
	if configPath == "" {
		configPath = config.IndexConfigPath()
	}
	indexCfg, err := config.LoadIndexConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	repo, err := sqlite.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer repo.Close()
	var debugWriter *os.File
	if *debugFlag {
		debugWriter = os.Stderr
	}

	pos := fs.Args()
	if len(pos) > 0 {
		filePath := pos[0]
		title := strings.Join(pos[1:], " ")
		svc := app.NewIndexService(nil, nil, nil, repo, domain.ShrinkConfig{})
		stats, p, skipped, err := svc.IndexFileWithOptions(context.Background(), filePath, title, app.IndexRunOptions{
			Force: *forceFlag,
			Debug: debugWriter,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "index: %v\n", err)
			os.Exit(1)
		}
		if skipped {
			fmt.Printf("Skipped unchanged %s  %q\n", p.ID, p.Title)
			return
		}
		fmt.Printf("Indexed %s  %q  (%d message)  Elapsed: %s\n",
			p.ID, p.Title, stats.Messages, stats.Elapsed.Round(time.Millisecond))
		return
	}

	depth := domain.IndexDepthDeep
	if *quickFlag {
		depth = domain.IndexDepthQuick
	}
	opts := domain.IndexOptions{
		Shrink: domain.ShrinkConfig{
			Enabled:        !*noShrink,
			ToolPayloadCap: *shrinkCap,
			DropImages:     true,
		},
		Depth: depth,
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "homedir: %v\n", err)
		os.Exit(1)
	}

	src := transcript.NewSource(filepath.Join(home, ".claude", "projects"), opts)
	desktopSrc := desktoptranscript.NewSource(
		filepath.Join(home, "Library", "Application Support", "Claude"),
		filepath.Join(home, ".claude", "projects"),
		opts,
	)
	cursorSrc := cursortranscript.NewSource(filepath.Join(home, ".cursor", "projects"), opts)
	opencodeSrc := opencodetranscript.NewSource(config.OpenCodeDBPath(), opts)
	codexSrc := codextranscript.NewSource(config.CodexSessionsDir(), opts)
	log := history.NewLog(filepath.Join(home, ".claude", "history.jsonl"))
	plans := plan.NewSource(home, indexCfg)

	svc := app.NewIndexServiceMulti(
		[]domain.TranscriptSource{src, desktopSrc, cursorSrc, opencodeSrc, codexSrc},
		log,
		[]domain.PlanSource{plans},
		repo,
		opts.Shrink,
	)
	stats, err := svc.RunWithOptions(context.Background(), indexCfg, home, func(done, total int) {
		fmt.Fprintf(os.Stderr, "\rIndexing %d/%d...", done, total)
	}, app.IndexRunOptions{
		Force: *forceFlag,
		Debug: debugWriter,
	})
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Sessions: %d  Orphaned: %d  Plans: %d  Skipped: %d  Messages: %d  Elapsed: %s\n",
		stats.Sessions, stats.Orphaned, stats.Plans, stats.Skipped, stats.Messages, stats.Elapsed.Round(time.Millisecond))
}

// cmdSearch runs full-text (FTS) search by default. With --semantic it runs
// embedding-based nearest-neighbor search instead. FTS Score is BM25 (lower
// is better); semantic Score is cosine similarity (higher is better). FTS
// search has no vendor/project/since filters — those apply to --semantic
// only, since it queries the embeddings index rather than the FTS index.
func cmdSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file")
	limitFlag := fs.Int("limit", 20, "max results")
	jsonFlag := fs.Bool("json", false, "output JSON")
	fuzzyFlag := fs.Bool("fuzzy", false, "fuzzy match terms (prefix + edit-distance-1)")
	semanticFlag := fs.Bool("semantic", false, "semantic (embedding) search instead of full-text search")
	vendorFlag := fs.String("vendor", "", "filter by vendor (--semantic only)")
	projectFlag := fs.String("project", "", "filter by project path (--semantic only)")
	sinceFlag := fs.String("since", "", "only messages since duration ago, e.g. 30d (--semantic only)")
	ollamaFlags := addOllamaFlags(fs)
	fs.Parse(args)

	query := strings.Join(fs.Args(), " ")
	if query == "" {
		fmt.Fprintln(os.Stderr, "usage: chv search <query>")
		os.Exit(1)
	}

	if *fuzzyFlag && *semanticFlag {
		fmt.Fprintln(os.Stderr, "usage: --fuzzy cannot be combined with --semantic")
		os.Exit(1)
	}
	if !*semanticFlag && (*vendorFlag != "" || *projectFlag != "" || *sinceFlag != "") {
		fmt.Fprintln(os.Stderr, "usage: --vendor/--project/--since require --semantic — FTS search has no filters")
		os.Exit(1)
	}

	if *semanticFlag {
		cmdSearchSemantic(query, *limitFlag, *jsonFlag, *vendorFlag, *projectFlag, *sinceFlag, *dbFlag, ollamaFlags)
		return
	}

	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = config.DBPath()
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "DB not found — run `chv index` first")
		os.Exit(1)
	}

	repo, err := sqlite.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer repo.Close()

	svc := app.NewSearchService(repo)
	hits, err := svc.Search(context.Background(), query, *limitFlag, domain.SearchOpts{Fuzzy: *fuzzyFlag})
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		os.Exit(1)
	}

	if *jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(hits)
		return
	}

	for _, h := range hits {
		tag := vendorTag(h)
		fmt.Printf("%s  %s%s\n  %s\n\n", h.SessionID, h.SessionTitle, tag, h.Snippet)
	}
}

// cmdSearchSemantic runs the --semantic branch of cmdSearch.
func cmdSearchSemantic(query string, limit int, jsonOut bool, vendorFlag, projectFlag, sinceFlag, dbFlag string, ollamaFlags ollamaFlags) {
	indexCfg := loadIndexConfigOrExit()
	repo := openDBOrExit(dbFlag, true)
	defer repo.Close()

	client, _, _ := ollamaFlags.resolve(indexCfg)

	since, err := app.SinceTime(sinceFlag, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		os.Exit(1)
	}

	svc := app.NewSemanticSearchService(client, repo, repo)
	hits, err := svc.Search(context.Background(), query, app.SemanticSearchOpts{
		Limit: limit, Vendor: domain.Vendor(vendorFlag), ProjectPath: projectFlag, Since: since,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(hits)
		return
	}

	for _, h := range hits {
		tag := vendorTag(h)
		fmt.Printf("%s  %s%s  score=%.3f\n  %s\n\n", h.SessionID, h.SessionTitle, tag, h.Score, h.Snippet)
	}
}

func cmdView(args []string) {
	fs := flag.NewFlagSet("view", flag.ExitOnError)
	formatFlag := fs.String("format", "auto", "transcript format: auto, claude, cursor, or codex")
	shrinkCap := fs.Int("shrink-cap", 2000, "rune cap for tool payloads")
	noShrink := fs.Bool("no-shrink", false, "disable shrink transform")
	fs.Parse(args)

	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: chv view [--format auto|claude|cursor|codex] <path.jsonl>")
		os.Exit(1)
	}
	format, err := app.ParseViewFormat(*formatFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "view: %v\n", err)
		os.Exit(1)
	}

	opts := domain.IndexOptions{
		Shrink: domain.ShrinkConfig{
			Enabled:        !*noShrink,
			ToolPayloadCap: *shrinkCap,
			DropImages:     true,
		},
		Depth: domain.IndexDepthDeep,
	}
	svc := app.NewViewServiceWithCodex(transcript.LoadFile, cursortranscript.LoadFile, codextranscript.LoadFile, opts)
	detail, err := svc.LoadJSONL(context.Background(), fs.Arg(0), format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "view: %v\n", err)
		os.Exit(1)
	}

	tui.InitTheme(os.Stdout)
	p := tea.NewProgram(tui.NewDetailApp(detail), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}

func vendorTag(h domain.SearchHit) string {
	parts := []string{domain.VendorLabel(h.Vendor)}
	switch h.RecordKind {
	case domain.RecordPlan:
		parts = append(parts, "plan")
	case domain.RecordChat:
		if !h.HasTranscript {
			parts = append(parts, "prompt-only")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, " ") + "]"
}

func launchTUI() {
	dbPath := config.DBPath()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "DB not found — run `chv index` first")
		os.Exit(1)
	}
	repo, err := sqlite.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer repo.Close()

	svc := app.NewSearchService(repo)
	tui.InitTheme(os.Stdout)
	p := tea.NewProgram(tui.NewApp(svc, config.SearchHistoryPath()), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
