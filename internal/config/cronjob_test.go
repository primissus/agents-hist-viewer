package config

import (
	"strings"
	"testing"
)

func TestCronEntry(t *testing.T) {
	got := CronEntry("/usr/local/bin/chv", "0 */4 * * *", "/tmp/index.log")
	if !strings.Contains(got, CronManagedMarker) {
		t.Fatalf("missing marker: %q", got)
	}
	if !strings.Contains(got, "index >> /tmp/index.log") {
		t.Fatalf("unexpected entry: %q", got)
	}
}

func TestMergeCronLines(t *testing.T) {
	existing := []string{
		"0 * * * * /other/tool",
		"0 */4 * * * /old/chv index >> /old.log 2>&1 # chv-managed",
	}
	entry := CronEntry("/new/chv", "0 */4 * * *", "/new.log")
	merged := MergeCronLines(existing, entry)
	text := strings.Join(merged, "\n")
	if strings.Contains(text, "/old/chv") {
		t.Fatalf("old entry not removed: %q", text)
	}
	if !strings.Contains(text, "/new/chv") {
		t.Fatalf("new entry missing: %q", text)
	}
	if !strings.Contains(text, "/other/tool") {
		t.Fatalf("unrelated entry removed: %q", text)
	}
}

func TestStripManagedCronLines(t *testing.T) {
	lines := []string{
		"0 * * * * /other/tool",
		"0 */4 * * * /chv index >> /log 2>&1 # chv-managed",
		"",
	}
	got := StripManagedCronLines(lines)
	if len(got) != 1 || got[0] != "0 * * * * /other/tool" {
		t.Fatalf("got %v", got)
	}
}

func TestShellQuote(t *testing.T) {
	if shellQuote("/simple/path") != "/simple/path" {
		t.Fatal("simple path should not be quoted")
	}
	if shellQuote("/path with spaces/chv") != `"/path with spaces/chv"` {
		t.Fatalf("got %q", shellQuote("/path with spaces/chv"))
	}
}
