package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const defaultPlansDirName = "plans"

// PlanDir is an absolute directory containing plan markdown files.
type PlanDir struct {
	Path        string
	ProjectPath string // empty when global / unknown
}

// DiscoverPlanDirs returns unique plan directories from Claude Code settings
// and the default ~/.claude/plans location.
func DiscoverPlanDirs(home string) ([]PlanDir, error) {
	claudeDir := filepath.Join(home, ".claude")
	projects := CollectProjectRoots(home, claudeDir)

	seen := map[string]string{} // abs dir -> project path
	add := func(abs, project string) {
		if abs == "" {
			return
		}
		abs = filepath.Clean(abs)
		if prev, ok := seen[abs]; ok {
			if prev == "" && project != "" {
				seen[abs] = project
			}
			return
		}
		seen[abs] = project
	}

	defaultDir := filepath.Join(claudeDir, defaultPlansDirName)
	add(defaultDir, "")

	if raw, ok := readPlansDirectory(filepath.Join(claudeDir, "settings.json")); ok {
		if resolved, err := resolvePlansPath(raw, claudeDir, ""); err == nil {
			add(resolved, "")
		}
	}

	for _, project := range projects {
		for _, name := range []string{"settings.json", "settings.local.json"} {
			path := filepath.Join(project, ".claude", name)
			raw, ok := readPlansDirectory(path)
			if !ok {
				continue
			}
			resolved, err := resolvePlansPath(raw, project, project)
			if err != nil {
				continue
			}
			add(resolved, project)
		}
	}

	out := make([]PlanDir, 0, len(seen))
	for dir, project := range seen {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, PlanDir{Path: dir, ProjectPath: project})
	}
	return out, nil
}

// CollectProjectRoots returns known Claude Code project directories.
func CollectProjectRoots(home, claudeDir string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		if p == "" || p == "." || seen[p] {
			return
		}
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	for _, p := range projectRootsFromJSON(filepath.Join(home, ".claude.json")) {
		add(p)
	}

	entries, err := os.ReadDir(filepath.Join(claudeDir, "projects"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if p := decodeProjectDir(e.Name()); p != "" {
			add(p)
		}
	}
	return out
}

func projectRootsFromJSON(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Projects map[string]json.RawMessage `json:"projects"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Projects))
	for p := range doc.Projects {
		out = append(out, p)
	}
	return out
}

func readPlansDirectory(settingsPath string) (string, bool) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", false
	}
	var doc struct {
		PlansDirectory string `json:"plansDirectory"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", false
	}
	raw := strings.TrimSpace(doc.PlansDirectory)
	if raw == "" {
		return "", false
	}
	return raw, true
}

// resolvePlansPath expands plansDirectory from Claude Code settings.
// Project-scoped paths are relative to projectRoot; user-level relative paths
// are relative to claudeDir (~/.claude).
func resolvePlansPath(raw, baseDir, projectRoot string) (string, error) {
	raw = expandHome(raw)
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), nil
	}
	root := baseDir
	if projectRoot != "" {
		root = projectRoot
	}
	return filepath.Clean(filepath.Join(root, raw)), nil
}

func expandHome(path string) string {
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// decodeProjectDir reverses Claude Code's project folder encoding.
func decodeProjectDir(encoded string) string {
	if !strings.HasPrefix(encoded, "-") {
		return ""
	}
	return "/" + strings.ReplaceAll(encoded[1:], "-", "/")
}
