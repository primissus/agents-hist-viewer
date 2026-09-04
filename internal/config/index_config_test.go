package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIndexConfigMissing(t *testing.T) {
	cfg, err := LoadIndexConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Directories) != 0 {
		t.Fatalf("expected empty directories, got %v", cfg.Directories)
	}
	if got := cfg.PatternsOrDefault(); len(got) != 2 || got[0] != "PLAN.md" {
		t.Fatalf("patterns: %v", got)
	}
}

func TestLoadIndexConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	data := `{"directories":["~/proj"],"patterns":["NOTES.md"]}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadIndexConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Directories) != 1 || cfg.Directories[0] != "~/proj" {
		t.Fatalf("directories: %v", cfg.Directories)
	}
	if got := cfg.PatternsOrDefault(); len(got) != 1 || got[0] != "NOTES.md" {
		t.Fatalf("patterns: %v", got)
	}
}

func TestExpandDirectories(t *testing.T) {
	home := t.TempDir()
	cfg := IndexConfig{Directories: []string{"~/work", home}}
	got := cfg.ExpandDirectories(home)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	want := filepath.Join(home, "work")
	if got[0] != want {
		t.Errorf("first dir: got %q want %q", got[0], want)
	}
}
