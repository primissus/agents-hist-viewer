package tui

import (
	"testing"

	"claude-code-hist-viewer/internal/domain"
)

func TestBuildFilterOptsPath(t *testing.T) {
	m := Model{
		filterCat: filterPath,
		hits: []domain.SearchHit{
			{ProjectPath: "/b"},
			{ProjectPath: "/a"},
		},
	}
	opts := buildFilterOpts(m)
	if len(opts) != 3 || opts[0] != "" || opts[1] != "/a" {
		t.Fatalf("path opts: %v", opts)
	}
}

func TestClearFilterCategoryVendor(t *testing.T) {
	m := Model{
		filterCat:    filterVendor,
		filterVendor: domain.VendorCursor,
		filterOpts:   []string{"", string(domain.VendorClaude), string(domain.VendorClaudeDesktop), string(domain.VendorCursor), string(domain.VendorOpencode), string(domain.VendorCodex)},
	}
	clearFilterCategory(&m)
	if m.filterVendor != "" {
		t.Errorf("expected cleared vendor, got %q", m.filterVendor)
	}
}

func TestFilterOptLabel(t *testing.T) {
	if got := filterOptLabel(filterVendor, ""); got != "All vendors" {
		t.Errorf("got %q", got)
	}
	if got := filterOptLabel(filterType, string(domain.RecordPlan)); got != "Plan" {
		t.Errorf("got %q", got)
	}
	if got := filterOptLabel(filterVendor, string(domain.VendorClaude)); got != "Claude Code" {
		t.Errorf("got %q", got)
	}
	if got := filterOptLabel(filterVendor, string(domain.VendorClaudeDesktop)); got != "Claude Desktop" {
		t.Errorf("got %q", got)
	}
	if got := filterOptLabel(filterVendor, string(domain.VendorOpencode)); got != "OpenCode" {
		t.Errorf("got %q", got)
	}
	if got := filterOptLabel(filterVendor, string(domain.VendorCodex)); got != "Codex" {
		t.Errorf("got %q", got)
	}
}

func TestBuildFilterOptsVendorIncludesNewVendors(t *testing.T) {
	m := Model{filterCat: filterVendor}
	opts := buildFilterOpts(m)
	for _, want := range []string{
		string(domain.VendorOpencode),
		string(domain.VendorCodex),
	} {
		found := false
		for _, o := range opts {
			if o == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("vendor option %q missing from %v", want, opts)
		}
	}
}

func TestHitTagsIncludeSourceLabel(t *testing.T) {
	got := hitTags(domain.SearchHit{Vendor: domain.VendorClaudeDesktop, RecordKind: domain.RecordChat, HasTranscript: true})
	if got != " [Claude Desktop]" {
		t.Fatalf("tag = %q", got)
	}
	got = hitTags(domain.SearchHit{Vendor: domain.VendorCursor, RecordKind: domain.RecordPlan})
	if got != " [Cursor plan]" {
		t.Fatalf("tag = %q", got)
	}
}
