package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	desktoptranscript "claude-code-hist-viewer/internal/adapter/claudedesktop/transcript"
	codextranscript "claude-code-hist-viewer/internal/adapter/codex/transcript"
	cursortranscript "claude-code-hist-viewer/internal/adapter/cursor/transcript"
	opencodetranscript "claude-code-hist-viewer/internal/adapter/opencode/transcript"
	"claude-code-hist-viewer/internal/adapter/history"
	"claude-code-hist-viewer/internal/adapter/ollama"
	"claude-code-hist-viewer/internal/adapter/plan"
	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/adapter/transcript"
	"claude-code-hist-viewer/internal/adapter/tui"
	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/config"
	"claude-code-hist-viewer/internal/domain"

	tea "github.com/charmbracelet/bubbletea"
)

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
	default:
		launchTUI()
	}
}

// parseSinceDuration extends time.ParseDuration with a "Nd" (days) suffix,
// for CLI flags like --since 30d.
func parseSinceDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func loadIndexConfigOrExit() config.IndexConfig {
	indexCfg, err := config.LoadIndexConfig(config.IndexConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	return indexCfg
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
	modelFlag := fs.String("model", "", "embedding model (default: config or mxbai-embed-large)")
	ollamaFlag := fs.String("ollama", "", "Ollama base URL (default: config or http://localhost:11434)")
	batchFlag := fs.Int("batch", 32, "embed batch size")
	vendorFlag := fs.String("vendor", "", "filter by vendor")
	projectFlag := fs.String("project", "", "filter by project path")
	forceFlag := fs.Bool("force", false, "re-embed even if already embedded")
	limitFlag := fs.Int("limit", 0, "max units to embed (0 = no limit)")
	fs.Parse(args)

	indexCfg := loadIndexConfigOrExit()
	repo := openDBOrExit(*dbFlag, true)
	defer repo.Close()

	model := *modelFlag
	if model == "" {
		model = indexCfg.EmbedModelOrDefault()
	}
	ollamaURL := *ollamaFlag
	if ollamaURL == "" {
		ollamaURL = indexCfg.OllamaURLOrDefault()
	}
	client := ollama.New(ollamaURL, model, indexCfg.ChatModelOrDefault())

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
	modelFlag := fs.String("model", "", "embedding model")
	chatFlag := fs.String("chat", "", "chat model")
	ollamaFlag := fs.String("ollama", "", "Ollama base URL")
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

	model := *modelFlag
	if model == "" {
		model = indexCfg.EmbedModelOrDefault()
	}
	chatModel := *chatFlag
	if chatModel == "" {
		chatModel = indexCfg.ChatModelOrDefault()
	}
	ollamaURL := *ollamaFlag
	if ollamaURL == "" {
		ollamaURL = indexCfg.OllamaURLOrDefault()
	}
	client := ollama.New(ollamaURL, model, chatModel)

	var since time.Time
	if *sinceFlag != "" {
		d, err := parseSinceDuration(*sinceFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ask: %v\n", err)
			os.Exit(1)
		}
		since = time.Now().Add(-d)
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

func cmdPatterns(args []string) {
	fs := flag.NewFlagSet("patterns", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file")
	modelFlag := fs.String("model", "", "embedding model")
	chatFlag := fs.String("chat", "", "chat model")
	ollamaFlag := fs.String("ollama", "", "Ollama base URL")
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

	model := *modelFlag
	if model == "" {
		model = indexCfg.EmbedModelOrDefault()
	}
	var chatSvc domain.ChatModel
	if !*noLLMFlag {
		ollamaURL := *ollamaFlag
		if ollamaURL == "" {
			ollamaURL = indexCfg.OllamaURLOrDefault()
		}
		chatModel := *chatFlag
		if chatModel == "" {
			chatModel = indexCfg.ChatModelOrDefault()
		}
		chatSvc = ollama.New(ollamaURL, model, chatModel)
	}

	var since time.Time
	if *sinceFlag != "" {
		d, err := parseSinceDuration(*sinceFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "patterns: %v\n", err)
			os.Exit(1)
		}
		since = time.Now().Add(-d)
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

func cmdSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	dbFlag := fs.String("db", "", "path to DB file")
	limitFlag := fs.Int("limit", 20, "max results")
	jsonFlag := fs.Bool("json", false, "output JSON")
	fuzzyFlag := fs.Bool("fuzzy", false, "fuzzy match terms (prefix + edit-distance-1)")
	fs.Parse(args)

	query := strings.Join(fs.Args(), " ")
	if query == "" {
		fmt.Fprintln(os.Stderr, "usage: chv search <query>")
		os.Exit(1)
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
