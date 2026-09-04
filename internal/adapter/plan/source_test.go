package plan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/config"
	"claude-code-hist-viewer/internal/domain"
)

func TestSourcePlanPaths(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	plansDir := filepath.Join(claude, "plans")
	sub := filepath.Join(plansDir, "myapp")
	for _, d := range []string{plansDir, sub} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(plansDir, "PROGRESS.md"), []byte("# Root Plan\nhello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "PLAN.md"), []byte("# Nested\nworld"), 0644); err != nil {
		t.Fatal(err)
	}

	src := NewSource(home, config.IndexConfig{})
	paths, err := src.PlanPaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 plan paths, got %d: %v", len(paths), paths)
	}
}

func TestPlanIDStable(t *testing.T) {
	id1 := PlanID("/tmp/a.md")
	id2 := PlanID("/tmp/a.md")
	if id1 != id2 || id1[:5] != "plan:" {
		t.Errorf("unexpected id %q", id1)
	}
}

func TestPlanTitle(t *testing.T) {
	if got := planTitle("# Hello World\nbody", "x.md"); got != "Hello World" {
		t.Errorf("got %q", got)
	}
	if got := planTitle("no heading", "fallback.md"); got != "fallback" {
		t.Errorf("got %q", got)
	}
}

func TestManualTitle(t *testing.T) {
	if got := manualTitle("# From Heading\nbody", ""); got != "From Heading" {
		t.Errorf("got %q", got)
	}
	if got := manualTitle("# Ignored\nbody", "Override"); got != "Override" {
		t.Errorf("got %q", got)
	}
	if got := manualTitle("no heading here", ""); got == "" {
		t.Error("expected petname fallback")
	}
}

func TestArchiveFilename(t *testing.T) {
	got := archiveFilename("plan:abc123")
	if got != "plan-abc123.md" {
		t.Errorf("got %q", got)
	}
}

func TestFromFile(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()
	src := filepath.Join(srcDir, "PROGRESS.md")
	if err := os.WriteFile(src, []byte("# Sprint 3\nunique manual phrase"), 0644); err != nil {
		t.Fatal(err)
	}

	p1, err := FromFile(src, "", archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Title != "Sprint 3" {
		t.Errorf("title: got %q", p1.Title)
	}
	if p1.ID != PlanID(src) {
		t.Errorf("id: got %q want %q", p1.ID, PlanID(src))
	}
	if !strings.HasPrefix(p1.FilePath, archiveDir) {
		t.Errorf("FilePath %q not under archive dir", p1.FilePath)
	}
	if p1.ProjectPath != srcDir {
		t.Errorf("ProjectPath: got %q", p1.ProjectPath)
	}
	if _, err := os.Stat(p1.FilePath); err != nil {
		t.Fatalf("archive missing: %v", err)
	}

	p2, err := FromFile(src, "Custom Title", archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Title != "Custom Title" {
		t.Errorf("override title: got %q", p2.Title)
	}
	if p2.ID != p1.ID {
		t.Error("expected stable id on re-index")
	}

	if err := os.WriteFile(src, []byte("updated content without heading"), 0644); err != nil {
		t.Fatal(err)
	}
	p3, err := FromFile(src, "", archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p3.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "updated content without heading" {
		t.Errorf("archive not updated: %q", data)
	}
}

func TestContentHash(t *testing.T) {
	h1 := ContentHash([]byte("abc"))
	h2 := ContentHash([]byte("abc"))
	h3 := ContentHash([]byte("abcd"))
	if h1 != h2 || h1 == h3 {
		t.Fatalf("hash mismatch: %q %q %q", h1, h2, h3)
	}
}

var _ domain.PlanSource = (*Source)(nil)

func TestPlanSourceImplementsPort(_ *testing.T) {
	_ = time.Now()
}
