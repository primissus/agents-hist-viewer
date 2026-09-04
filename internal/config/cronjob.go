package config

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// CronManagedMarker identifies crontab lines installed by chv.
	CronManagedMarker = "# chv-managed"
	defaultCronSchedule = "0 */4 * * *"
)

// IndexLogPath returns the default log file for scheduled index runs.
func IndexLogPath() string {
	return xdgChv("index.log")
}

// CronEntry builds a crontab line for chv index.
func CronEntry(chvPath, schedule, logPath string) string {
	return fmt.Sprintf("%s %s index >> %s 2>&1 %s",
		schedule, shellQuote(chvPath), shellQuote(logPath), CronManagedMarker)
}

// MergeCronLines removes existing chv-managed lines and appends entry.
func MergeCronLines(existing []string, entry string) []string {
	var out []string
	for _, line := range existing {
		if strings.Contains(line, CronManagedMarker) {
			continue
		}
		out = append(out, line)
	}
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
		out = append(out, "")
	}
	out = append(out, entry)
	return out
}

// StripManagedCronLines removes chv-managed crontab lines.
func StripManagedCronLines(lines []string) []string {
	var out []string
	for _, line := range lines {
		if strings.Contains(line, CronManagedMarker) {
			continue
		}
		out = append(out, line)
	}
	return trimTrailingBlankLines(out)
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

// InstallCronJob adds or replaces the chv index cron entry.
func InstallCronJob(chvPath, schedule string) error {
	if schedule == "" {
		schedule = defaultCronSchedule
	}
	chvPath, err := filepath.Abs(chvPath)
	if err != nil {
		return fmt.Errorf("chv path: %w", err)
	}
	if _, err := os.Stat(chvPath); err != nil {
		return fmt.Errorf("chv binary: %w", err)
	}

	logPath := IndexLogPath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return fmt.Errorf("log dir: %w", err)
	}

	existing, err := readCrontab()
	if err != nil {
		return err
	}
	entry := CronEntry(chvPath, schedule, logPath)
	merged := MergeCronLines(existing, entry)
	return writeCrontab(merged)
}

// UninstallCronJob removes the chv index cron entry.
func UninstallCronJob() error {
	existing, err := readCrontab()
	if err != nil {
		return err
	}
	stripped := StripManagedCronLines(existing)
	if len(stripped) == 0 {
		return removeCrontab()
	}
	return writeCrontab(stripped)
}

func readCrontab() ([]string, error) {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		if execErr, ok := err.(*exec.ExitError); ok && execErr.ExitCode() != 0 {
			msg := strings.ToLower(string(execErr.Stderr))
			if strings.Contains(msg, "no crontab") || len(bytes.TrimSpace(out)) == 0 {
				return nil, nil
			}
		}
		if len(bytes.TrimSpace(out)) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("read crontab: %w", err)
	}
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func writeCrontab(lines []string) error {
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("write crontab: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeCrontab() error {
	cmd := exec.Command("crontab", "-r")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "no crontab") {
			return nil
		}
		return fmt.Errorf("remove crontab: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"'$\\") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
