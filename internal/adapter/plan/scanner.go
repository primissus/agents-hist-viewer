package plan

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// FindFiles discovers plan markdown files under roots matching basename patterns.
// Uses fd when available; falls back to filepath.WalkDir.
func FindFiles(roots, patterns []string) ([]string, error) {
	roots = existingRoots(roots)
	if len(roots) == 0 {
		return nil, nil
	}
	if len(patterns) == 0 {
		patterns = []string{"PLAN.md", "PROGRESS.md"}
	}

	paths, err := findWithFD(roots, patterns)
	if err != nil {
		paths, err = findWithWalk(roots, patterns)
	}
	if err != nil {
		return nil, err
	}
	return dedupePaths(paths), nil
}

func existingRoots(roots []string) []string {
	var out []string
	for _, root := range roots {
		root = filepath.Clean(root)
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, root)
	}
	return out
}

func findWithFD(roots, patterns []string) ([]string, error) {
	if _, err := exec.LookPath("fd"); err != nil {
		return nil, fmt.Errorf("fd not found")
	}

	var paths []string
	for _, pat := range patterns {
		args := []string{"-t", "f", "-H", "-i", "-a", "-g", pat}
		args = append(args, roots...)
		cmd := exec.Command("fd", args...)
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("fd: %w", err)
		}
		for line := range strings.SplitSeq(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || shouldSkipPath(line) {
				continue
			}
			paths = append(paths, line)
		}
	}
	return paths, nil
}

func findWithWalk(roots, patterns []string) ([]string, error) {
	patternSet := make([]string, len(patterns))
	for i, p := range patterns {
		patternSet[i] = strings.ToLower(p)
	}

	var paths []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if shouldSkipPath(path) {
				return nil
			}
			base := strings.ToLower(filepath.Base(path))
			for _, pat := range patternSet {
				if base == pat {
					abs, err := filepath.Abs(path)
					if err != nil {
						return nil
					}
					paths = append(paths, abs)
					break
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func shouldSkipPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".original.md")
}

func dedupePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
