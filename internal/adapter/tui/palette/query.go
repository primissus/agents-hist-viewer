package palette

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const queryTimeout = 200 * time.Millisecond

var (
	ErrUnsupported = errors.New("palette query unsupported")
	ErrTimeout     = errors.New("palette query timeout")
)

var osc4Response = regexp.MustCompile(`\x1b\]4;(\d+);([^\x07\x1b]+)`)

// QueryPalette reads live RGB values for palette indices via OSC 4.
// Writes queries to out and reads responses from in (typically stdout/stdin).
func QueryPalette(out io.Writer, in io.Reader, indices []int) (map[int]string, error) {
	if len(indices) == 0 {
		return nil, ErrUnsupported
	}
	if !queryEnabled() {
		return nil, ErrUnsupported
	}

	query := buildQuery(indices)
	if _, err := out.Write([]byte(query)); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(queryTimeout)
	want := make(map[int]struct{}, len(indices))
	for _, i := range indices {
		want[i] = struct{}{}
	}
	got := make(map[int]string, len(indices))

	buf := make([]byte, 0, 512)
	tmp := make([]byte, 256)
	for len(got) < len(want) && time.Now().Before(deadline) {
		if rd, ok := in.(interface{ SetReadDeadline(time.Time) error }); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			if err := rd.SetReadDeadline(time.Now().Add(min(remaining, queryTimeout))); err != nil {
				return nil, err
			}
		}
		n, err := in.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for len(buf) > 0 {
				match := osc4Response.FindSubmatch(buf)
				if match == nil {
					break
				}
				idx, err := strconv.Atoi(string(match[1]))
				if err != nil {
					buf = buf[1:]
					continue
				}
				if hex, err := parseColorSpec(strings.TrimSpace(string(match[2]))); err == nil {
					got[idx] = hex
				}
				end := len(match[0])
				if end < len(buf) && buf[end] == '\x07' {
					end++
				} else if end+1 < len(buf) && buf[end] == '\x1b' && buf[end+1] == '\\' {
					end += 2
				}
				buf = buf[end:]
			}
		}
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, io.EOF) {
				break
			}
			if len(got) == 0 {
				return nil, err
			}
			break
		}
	}

	if len(got) < len(want) {
		if len(got) == 0 {
			return nil, ErrTimeout
		}
		return got, ErrTimeout
	}
	return got, nil
}

func buildQuery(indices []int) string {
	var b strings.Builder
	b.WriteString("\x1b]4;")
	for i, idx := range indices {
		if i > 0 {
			b.WriteByte(';')
		}
		fmt.Fprintf(&b, "%d;?", idx)
	}
	b.WriteString("\x1b\\")
	return b.String()
}

func parseColorSpec(spec string) (string, error) {
	spec = strings.TrimSuffix(spec, "\x1b\\")
	spec = strings.TrimSuffix(spec, "\x07")
	spec = strings.TrimSpace(spec)

	if strings.HasPrefix(spec, "#") {
		if len(spec) == 7 {
			return strings.ToLower(spec), nil
		}
		return "", fmt.Errorf("invalid hex color %q", spec)
	}

	const prefix = "rgb:"
	if strings.HasPrefix(spec, prefix) {
		parts := strings.Split(strings.TrimPrefix(spec, prefix), "/")
		if len(parts) != 3 {
			return "", fmt.Errorf("invalid rgb spec %q", spec)
		}
		ch := func(s string) (string, error) {
			if len(s) >= 2 {
				return s[:2], nil
			}
			return "", fmt.Errorf("invalid channel %q", s)
		}
		r, err := ch(parts[0])
		if err != nil {
			return "", err
		}
		g, err := ch(parts[1])
		if err != nil {
			return "", err
		}
		b, err := ch(parts[2])
		if err != nil {
			return "", err
		}
		return "#" + r + g + b, nil
	}

	return "", fmt.Errorf("unknown color spec %q", spec)
}

func queryEnabled() bool {
	if os.Getenv("CHV_NO_COLOR_QUERY") != "" {
		return false
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "vscode", "Cursor":
		return false
	}
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" ||
		strings.HasPrefix(term, "screen") ||
		strings.HasPrefix(term, "tmux") {
		return false
	}
	return true
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
