package tui

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const maxHistoryEntries = 100

// loadHistory reads newline-separated queries from path.
// Returns nil without error when the file doesn't exist yet.
func loadHistory(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// appendHistory deduplicates query, appends it, trims to maxHistoryEntries,
// writes the result back to path, and returns the updated slice.
func appendHistory(path string, query string, existing []string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return existing
	}

	filtered := make([]string, 0, len(existing)+1)
	for _, e := range existing {
		if e != query {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, query)

	if len(filtered) > maxHistoryEntries {
		filtered = filtered[len(filtered)-maxHistoryEntries:]
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err == nil {
		if f, err := os.Create(path); err == nil {
			w := bufio.NewWriter(f)
			for _, line := range filtered {
				w.WriteString(line)
				w.WriteByte('\n')
			}
			w.Flush()
			f.Close()
		}
	}

	return filtered
}
