package history

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

// Log implements domain.PromptLog over ~/.claude/history.jsonl.
type Log struct{ path string }

func NewLog(path string) *Log { return &Log{path: path} }

type historyLine struct {
	Display       string                    `json:"display"`
	PastedContents map[string]pastedEntry   `json:"pastedContents"`
	Timestamp     int64                     `json:"timestamp"`
	Project       string                    `json:"project"`
	SessionID     string                    `json:"sessionId"`
}

type pastedEntry struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (l *Log) Prompts(ctx context.Context) ([]domain.Prompt, error) {
	f, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type raw struct {
		sessionID string
		text      string
		timestamp time.Time
		project   string
	}

	var raws []raw
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1*1024*1024), 1*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var hl historyLine
		if err := json.Unmarshal(line, &hl); err != nil {
			continue
		}
		text := hl.Display
		if isPastedPlaceholder(text) && len(hl.PastedContents) > 0 {
			text = mergePasted(hl.PastedContents)
		}
		raws = append(raws, raw{
			sessionID: hl.SessionID,
			text:      text,
			timestamp: time.UnixMilli(hl.Timestamp).UTC(),
			project:   hl.Project,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	// Group by sessionID, sort each group by timestamp, assign Seq.
	type group struct {
		items []raw
	}
	groups := map[string]*group{}
	order := []string{}
	for _, r := range raws {
		if _, ok := groups[r.sessionID]; !ok {
			groups[r.sessionID] = &group{}
			order = append(order, r.sessionID)
		}
		groups[r.sessionID].items = append(groups[r.sessionID].items, r)
	}

	var prompts []domain.Prompt
	for _, sid := range order {
		g := groups[sid]
		sort.Slice(g.items, func(i, j int) bool {
			return g.items[i].timestamp.Before(g.items[j].timestamp)
		})
		for seq, item := range g.items {
			prompts = append(prompts, domain.Prompt{
				SessionID: item.sessionID,
				Project:   item.project,
				Text:      item.text,
				Timestamp: item.timestamp,
				Seq:       seq,
			})
		}
	}
	return prompts, nil
}

func isPastedPlaceholder(s string) bool {
	return strings.HasPrefix(s, "[Pasted text ")
}

func mergePasted(entries map[string]pastedEntry) string {
	// Collect keys in a consistent order.
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		if entries[k].Content != "" {
			parts = append(parts, entries[k].Content)
		}
	}
	return strings.Join(parts, "\n")
}
