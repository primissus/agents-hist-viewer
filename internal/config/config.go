package config

import (
	"os"
	"path/filepath"
)

// DBPath returns the default SQLite DB path, respecting XDG_DATA_HOME.
func DBPath() string {
	return xdgChv("chv.db")
}

// SearchHistoryPath returns the path to the persistent search history file.
func SearchHistoryPath() string {
	return xdgChv("search_history")
}

// ManualFilesDir returns the directory for archived manually-indexed files.
func ManualFilesDir() string {
	return xdgChv("manual")
}

// OpenCodeDBPath returns the path to the OpenCode SQLite database (XDG-aware).
func OpenCodeDBPath() string {
	return filepath.Join(xdgDataHome(), "opencode", "opencode.db")
}

// CodexSessionsDir returns the Codex sessions directory ($CODEX_HOME or ~/.codex).
func CodexSessionsDir() string {
	base := os.Getenv("CODEX_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			base = "."
		} else {
			base = filepath.Join(home, ".codex")
		}
	}
	return filepath.Join(base, "sessions")
}

func xdgChv(name string) string {
	return filepath.Join(xdgDataHome(), "chv", name)
}

func xdgDataHome() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "."
		}
		return filepath.Join(home, ".local", "share")
	}
	return base
}
