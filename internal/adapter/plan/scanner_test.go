package plan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindFilesWalk(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "proj")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"PLAN.md":           "# Plan\nplan content",
		"progress.md":       "# Progress\nprogress content",
		"PLAN.original.md":  "# Original\nskip",
		"README.md":         "# Readme\nskip",
	} {
		dir := root
		if name == "PLAN.md" {
			dir = sub
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := findWithWalk([]string{root}, []string{"PLAN.md", "PROGRESS.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
	for _, p := range paths {
		if filepath.Base(p) == "PLAN.original.md" {
			t.Fatalf("should skip original: %s", p)
		}
	}
}

func TestDedupePaths(t *testing.T) {
	got := dedupePaths([]string{"/a/PLAN.md", "/a/PLAN.md", "/b/PLAN.md"})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}
