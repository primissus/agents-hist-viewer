package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"bytes"
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
	data, err := os.ReadFile(abs)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("read: %w", err)
	}
	return FromData(abs, data, titleOverride, archiveDir)
}

// FromData builds a Plan from already-read content, archiving it without
// re-reading the archive. Callers that hash the file first avoid a second read.
func FromData(absPath string, data []byte, titleOverride, archiveDir string) (domain.Plan, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return domain.Plan{}, fmt.Errorf("not a regular file: %s", absPath)
	}

	id := PlanID(absPath)
	if err := writeArchive(archiveDir, id, data); err != nil {
		return domain.Plan{}, err
	}
	archivePath := filepath.Join(archiveDir, archiveFilename(id))

	return domain.Plan{
		ID:          id,
		Title:       manualTitle(string(data), titleOverride),
		ProjectPath: filepath.Dir(absPath),
		FilePath:    archivePath,
		Content:     string(data),
		ModTime:     info.ModTime().UTC(),
		Vendor:      domain.VendorClaude,
	}, nil
}

// writeArchive stores data under archiveDir, skipping the write when the
// existing archive already has identical bytes.
func writeArchive(archiveDir, id string, data []byte) error {
	archivePath := filepath.Join(archiveDir, archiveFilename(id))
	if existing, err := os.ReadFile(archivePath); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return fmt.Errorf("mkdir archive: %w", err)
	}
	if err := os.WriteFile(archivePath, data, 0644); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
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
