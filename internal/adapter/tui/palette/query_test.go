package palette

import (
	"io"
	"strings"
	"testing"
)

func TestParseColorSpec(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"rgb:aaaa/bbbb/cccc", "#aabbcc"},
		{"rgb:ff00/aa00/5500", "#ffaa55"},
		{"#1a2b3c", "#1a2b3c"},
	}
	for _, tc := range tests {
		got, err := parseColorSpec(tc.in)
		if err != nil {
			t.Fatalf("parseColorSpec(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseColorSpec(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildQuery(t *testing.T) {
	got := buildQuery([]int{0, 2, 4})
	want := "\x1b]4;0;?;2;?;4;?\x1b\\"
	if got != want {
		t.Fatalf("buildQuery() = %q, want %q", got, want)
	}
}

type scriptedTTY struct {
	query strings.Builder
	resp  string
	off   int
}

func (s *scriptedTTY) Write(p []byte) (int, error) {
	return s.query.Write(p)
}

func (s *scriptedTTY) Read(p []byte) (int, error) {
	if s.off >= len(s.resp) {
		return 0, io.EOF
	}
	n := copy(p, s.resp[s.off:])
	s.off += n
	return n, nil
}

func TestQueryPalette(t *testing.T) {
	t.Setenv("CHV_NO_COLOR_QUERY", "")
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("TERM_PROGRAM", "")

	tty := &scriptedTTY{
		resp: "\x1b]4;2;rgb:9898/c3c3/7979\x1b\\" +
			"\x1b]4;4;rgb:4585/8585/8888\x1b\\",
	}
	got, err := QueryPalette(tty, tty, []int{2, 4})
	if err != nil {
		t.Fatalf("QueryPalette: %v", err)
	}
	if got[2] != "#98c379" {
		t.Fatalf("slot 2 = %q", got[2])
	}
	if got[4] != "#458588" {
		t.Fatalf("slot 4 = %q", got[4])
	}
	if tty.query.String() != buildQuery([]int{2, 4}) {
		t.Fatalf("unexpected query: %q", tty.query.String())
	}
}

func TestQueryPaletteSkipsWhenDisabled(t *testing.T) {
	t.Setenv("CHV_NO_COLOR_QUERY", "1")
	_, err := QueryPalette(io.Discard, strings.NewReader(""), []int{0})
	if err != ErrUnsupported {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}

func TestQueryPaletteTimeoutPartial(t *testing.T) {
	t.Setenv("CHV_NO_COLOR_QUERY", "")
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("TERM_PROGRAM", "")

	_, err := QueryPalette(io.Discard, strings.NewReader(""), []int{0})
	if err != ErrTimeout {
		t.Fatalf("got %v, want ErrTimeout", err)
	}
}

func TestQueryPaletteSkipsIntegratedTerminal(t *testing.T) {
	t.Setenv("CHV_NO_COLOR_QUERY", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "vscode")

	_, err := QueryPalette(io.Discard, strings.NewReader(""), []int{0})
	if err != ErrUnsupported {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}
