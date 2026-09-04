package plan_test

import (
	"os"
	"path/filepath"
	"testing"

	"claude-code-hist-viewer/internal/adapter/cursor/plan"
	"claude-code-hist-viewer/internal/domain"
)

func TestTitleForContentFrontmatter(t *testing.T) {
	content := "---\nname: My Cursor Plan\noverview: test\n---\n\n# Ignored\nbody"
	got := plan.TitleForContent(content, "", "file.plan.md")
	if got != "My Cursor Plan" {
		t.Errorf("got %q", got)
	}
}

func TestPlanIDStable(t *testing.T) {
	p := filepath.Join("/tmp", "a.plan.md")
	id1 := plan.PlanID(p)
	id2 := plan.PlanID(p)
	if id1 != id2 || id1[:12] != "cursor-plan:" {
		t.Errorf("got %q", id1)
	}
}

func TestFromFile(t *testing.T) {
	archive := t.TempDir()
	src := filepath.Join(t.TempDir(), "demo.plan.md")
	content := "---\nname: Cursor Demo\n---\n\ncursor plan unique phrase"
	if err := os.WriteFile(src, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := plan.FromFile(src, "", archive)
	if err != nil {
		t.Fatal(err)
	}
	if p.Vendor != domain.VendorCursor {
		t.Errorf("vendor: %q", p.Vendor)
	}
	if p.Title != "Cursor Demo" {
		t.Errorf("title: %q", p.Title)
	}
	if _, err := os.Stat(p.FilePath); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
}

func TestPlanPaths(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".cursor", "plans")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.plan.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}

	src := plan.NewSource(home)
	paths, err := src.PlanPaths(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d paths", len(paths))
	}
}
