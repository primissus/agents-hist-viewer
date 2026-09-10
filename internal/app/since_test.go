package app

import (
	"testing"
	"time"
)

func TestParseSinceDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"30d", 30 * 24 * time.Hour, false},
		{"2h", 2 * time.Hour, false},
		{"", 0, true},
		{"not-a-duration", 0, true},
	}
	for _, c := range cases {
		got, err := ParseSinceDuration(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSinceDuration(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSinceDuration(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSinceDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSinceTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	got, err := SinceTime("", now)
	if err != nil {
		t.Fatalf("SinceTime(\"\"): unexpected error: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("SinceTime(\"\") = %v, want zero time.Time", got)
	}

	got, err = SinceTime("2h", now)
	if err != nil {
		t.Fatalf("SinceTime(\"2h\"): unexpected error: %v", err)
	}
	want := now.Add(-2 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("SinceTime(\"2h\") = %v, want %v", got, want)
	}

	if _, err := SinceTime("bogus", now); err == nil {
		t.Errorf("SinceTime(\"bogus\"): expected error, got nil")
	}
}
