package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-code-hist-viewer/internal/config"
	"claude-code-hist-viewer/internal/domain"

	petname "github.com/dustinkirkland/golang-petname"
)

// Source implements domain.PlanSource via fd/walk plan file discovery.
type Source struct {
	home string
	cfg  config.IndexConfig
}

func NewSource(home string, cfg config.IndexConfig) *Source {
	return &Source{home: home, cfg: cfg}
}

func (s *Source) PlanPaths(_ context.Context) ([]string, error) {
	roots := []string{filepath.Join(s.home, ".claude")}
	roots = append(roots, s.cfg.ExpandDirectories(s.home)...)
	return FindFiles(roots, s.cfg.PatternsOrDefault())
}

// FromFile reads a file for manual indexing, copies it into archiveDir, and returns a Plan.
func FromFile(path, titleOverride, archiveDir string) (domain.Plan, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("abs path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return domain.Plan{}, fmt.Errorf("not a regular file: %s", abs)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("read: %w", err)
	}

	id := PlanID(abs)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return domain.Plan{}, fmt.Errorf("mkdir archive: %w", err)
	}
	archivePath := filepath.Join(archiveDir, archiveFilename(id))
	if err := os.WriteFile(archivePath, data, 0644); err != nil {
		return domain.Plan{}, fmt.Errorf("write archive: %w", err)
	}

	archived, err := os.ReadFile(archivePath)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("read archive: %w", err)
	}

	return domain.Plan{
		ID:          id,
		Title:       manualTitle(string(archived), titleOverride),
		ProjectPath: filepath.Dir(abs),
		FilePath:    archivePath,
		Content:     string(archived),
		ModTime:     info.ModTime().UTC(),
		Vendor:      domain.VendorClaude,
	}, nil
}

// PlanID returns a stable session id for a plan file path.
func PlanID(absPath string) string {
	sum := sha256.Sum256([]byte(absPath))
	return "plan:" + hex.EncodeToString(sum[:8])
}

// ContentHash returns a SHA-256 hex digest of file content.
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func archiveFilename(id string) string {
	return strings.Replace(id, ":", "-", 1) + ".md"
}

func titleFromHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func planTitle(content, filename string) string {
	if t := titleFromHeading(content); t != "" {
		return t
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func manualTitle(content, titleOverride string) string {
	if titleOverride != "" {
		return titleOverride
	}
	if t := titleFromHeading(content); t != "" {
		return t
	}
	return petname.Generate(2, "-")
}

// TitleForContent resolves a display title from file content and optional override.
func TitleForContent(content, titleOverride, filename string) string {
	if titleOverride != "" {
		return titleOverride
	}
	if t := titleFromHeading(content); t != "" {
		return t
	}
	if filename != "" {
		return strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	return petname.Generate(2, "-")
}
