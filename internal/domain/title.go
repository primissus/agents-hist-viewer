package domain

import (
	"strings"
	"unicode/utf8"
)

const defaultTitleMaxRunes = 60

// DeriveTitle picks a single-line display title from multiline prompt text.
// It skips standalone XML tags, slash commands, and bracket metadata lines.
func DeriveTitle(text string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = defaultTitleMaxRunes
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isStandaloneXMLTag(line) || isSlashCommand(line) || isBracketMetadata(line) {
			continue
		}
		line = strings.TrimSpace(stripLeadingXMLTags(line))
		if line == "" {
			continue
		}
		return truncateRunes(line, maxRunes)
	}
	return ""
}

func isStandaloneXMLTag(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "<") || !strings.HasSuffix(line, ">") {
		return false
	}
	inner := strings.TrimSpace(line[1 : len(line)-1])
	if inner == "" {
		return false
	}
	if strings.HasPrefix(inner, "/") {
		return isXMLName(strings.TrimSpace(inner[1:]))
	}
	name := inner
	if i := strings.IndexAny(inner, " \t"); i >= 0 {
		name = inner[:i]
	}
	return isXMLName(name)
}

func isXMLName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r != '_' && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				return false
			}
			continue
		}
		if r != '_' && r != '-' && r != '.' && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func isSlashCommand(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return false
	}
	rest := strings.TrimSpace(line[1:])
	if rest == "" {
		return true
	}
	i := 0
	for i < len(rest) {
		r, size := utf8.DecodeRuneInString(rest[i:])
		if r == ' ' || r == '\t' {
			break
		}
		if r != '-' && r != '_' && !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
		i += size
	}
	return i > 0
}

func isBracketMetadata(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func stripLeadingXMLTags(line string) string {
	for {
		line = strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(line, "<") {
			return line
		}
		end := strings.IndexRune(line, '>')
		if end < 0 {
			return line
		}
		tag := line[:end+1]
		if strings.HasPrefix(tag, "</") || !strings.HasSuffix(tag, ">") {
			return line
		}
		inner := strings.TrimSpace(tag[1 : len(tag)-1])
		if space := strings.IndexAny(inner, " \t"); space >= 0 {
			inner = inner[:space]
		}
		if !isXMLName(inner) {
			return line
		}
		line = line[end+1:]
	}
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
