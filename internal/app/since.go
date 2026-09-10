package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSinceDuration extends time.ParseDuration with a "Nd" (days) suffix,
// for CLI flags like --since 30d.
func ParseSinceDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// SinceTime resolves a --since flag value to an absolute time relative to
// now. An empty string returns the zero time.Time (no lower bound).
func SinceTime(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	d, err := ParseSinceDuration(s)
	if err != nil {
		return time.Time{}, err
	}
	return now.Add(-d), nil
}
