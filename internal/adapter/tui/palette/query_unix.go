//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package palette

import (
	"os"

	"github.com/charmbracelet/x/term"
)

// QueryTerminal queries the palette via the controlling TTY so OSC responses
// do not pollute stdin/stdout used by the Bubble Tea program.
func QueryTerminal(indices []int) (map[int]string, error) {
	if !queryEnabled() {
		return nil, ErrUnsupported
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return nil, ErrUnsupported
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, ErrUnsupported
	}
	defer tty.Close()

	// Disable ECHO while querying so the tty line discipline does not echo the
	// terminal's OSC 4 responses back onto the screen before we read them.
	if state, err := term.MakeRaw(tty.Fd()); err == nil {
		defer term.Restore(tty.Fd(), state) //nolint: errcheck
	}

	got, err := QueryPalette(tty, tty, indices)
	drainFD(tty.Fd(), drainWindow)
	return got, err
}
