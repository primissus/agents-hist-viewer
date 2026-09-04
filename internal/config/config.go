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

func xdgChv(name string) string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			base = "."
		} else {
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, "chv", name)
}
