package claudesettings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePlansPath(t *testing.T) {
	claude := filepath.Join("/Users/me", ".claude")
	project := "/Users/me/src/app"

	tests := []struct {
		name    string
		raw     string
		base    string
		project string
		want    string
	}{
		{"absolute", "/tmp/plans", claude, "", "/tmp/plans"},
		{"project relative", "./.claude/plans", claude, project, filepath.Join(project, ".claude", "plans")},
		{"user relative", "plans", claude, "", filepath.Join(claude, "plans")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvePlansPath(tc.raw, tc.base, tc.project)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if got := expandHome("~/plans"); got != filepath.Join(home, "plans") {
		t.Errorf("got %q want %q", got, filepath.Join(home, "plans"))
	}
}

func TestDiscoverPlanDirs(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	defaultPlans := filepath.Join(claude, "plans")
	project := filepath.Join(home, "src", "app")
	custom := filepath.Join(project, "my-plans")

	for _, d := range []string{defaultPlans, custom} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	settings := `{"plansDirectory":"./my-plans"}`
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".claude", "settings.json"), []byte(settings), 0644); err != nil {
		t.Fatal(err)
	}

	claudeJSON := `{"projects":{"` + project + `":{}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(claudeJSON), 0644); err != nil {
		t.Fatal(err)
	}

	dirs, err := DiscoverPlanDirs(home)
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, d := range dirs {
		found[d.Path] = true
		if d.Path == custom && d.ProjectPath != project {
			t.Errorf("custom dir project = %q want %q", d.ProjectPath, project)
		}
	}
	if !found[defaultPlans] {
		t.Errorf("missing default plans dir %s", defaultPlans)
	}
	if !found[custom] {
		t.Errorf("missing project plans dir %s", custom)
	}
}

func TestDecodeProjectDir(t *testing.T) {
	got := decodeProjectDir("-Users-me-src-app")
	want := "/Users/me/src/app"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
