package plan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"claude-code-hist-viewer/internal/config"
	"claude-code-hist-viewer/internal/domain"
)

// Source discovers Cursor plan markdown files under ~/.cursor/plans.
type Source struct{ home string }

func NewSource(home string) *Source { return &Source{home: home} }

func (s *Source) PlanPaths(_ context.Context) ([]string, error) {
	dir := filepath.Join(s.home, ".cursor", "plans")
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".plan.md") {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		paths = append(paths, abs)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return paths, err
}

// PlanID returns a stable session id for a Cursor plan file.
func PlanID(absPath string) string {
	sum := sha256.Sum256([]byte(absPath))
	return "cursor-plan:" + hex.EncodeToString(sum[:8])
}

// FromFile reads a Cursor plan, archives it, and returns a domain.Plan.
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
		Title:       titleForCursorPlan(string(data), titleOverride, filepath.Base(absPath)),
		ProjectPath: filepath.Dir(absPath),
		FilePath:    archivePath,
		Content:     string(data),
		ModTime:     info.ModTime().UTC(),
		Vendor:      domain.VendorCursor,
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

func archiveFilename(id string) string {
	return strings.Replace(id, ":", "-", 1) + ".md"
}

// TitleForContent resolves a display title for a Cursor plan file.
func TitleForContent(content, titleOverride, filename string) string {
	return titleForCursorPlan(content, titleOverride, filename)
}

func titleForCursorPlan(content, titleOverride, filename string) string {
	if titleOverride != "" {
		return titleOverride
	}
	if name := frontmatterName(content); name != "" {
		return name
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func frontmatterName(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return ""
	}
	fm := content[4 : 4+end]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
	}
	return ""
}

// DefaultArchiveDir returns the archive directory for Cursor plans.
func DefaultArchiveDir() string { return config.ManualFilesDir() }
