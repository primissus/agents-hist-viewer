//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package palette

import (
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

// fdRW is a raw-fd reader/writer with a software read deadline. It never
// wraps os.File, so Fd() is never called and SetReadDeadline keeps working.
type fdRW struct {
	fd       int
	deadline time.Time
}

func (f *fdRW) Write(p []byte) (int, error) {
	return unix.Write(f.fd, p)
}

func (f *fdRW) Read(p []byte) (int, error) {
	for {
		var timeout *unix.Timeval
		if !f.deadline.IsZero() {
			remaining := time.Until(f.deadline)
			if remaining <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			tv := unix.NsecToTimeval(int64(remaining))
			timeout = &tv
		}

		var fds unix.FdSet
		fds.Set(f.fd)
		n, err := unix.Select(f.fd+1, &fds, nil, nil, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, os.ErrDeadlineExceeded
		}

		n, err = unix.Read(f.fd, p)
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			continue
		}
		if err != nil {
			return 0, err
		}
		return n, nil
	}
}

func (f *fdRW) SetReadDeadline(t time.Time) error {
	f.deadline = t
	return nil
}

// QueryTerminal queries the palette via the controlling TTY so OSC responses
// do not pollute stdin/stdout used by the Bubble Tea program.
func QueryTerminal(indices []int) (map[int]string, error) {
	if !queryEnabled() {
		return nil, ErrUnsupported
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return nil, ErrUnsupported
	}

	fd, err := unix.Open("/dev/tty", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, ErrUnsupported
	}

	// Raw fd, not os.File: Fd() is never called, so the runtime poller is
	// untouched and SetReadDeadline on fdRW works for the whole query.
	state, err := term.MakeRaw(uintptr(fd))
	if err != nil {
		unix.Close(fd)
		// Never send a query we cannot shield from echo.
		return nil, ErrUnsupported
	}

	defer unix.Close(fd)
	defer term.Restore(uintptr(fd), state) //nolint: errcheck

	drainFD(uintptr(fd), 10*time.Millisecond)
	got, err := QueryPalette(&fdRW{fd: fd}, &fdRW{fd: fd}, indices)
	drainFD(uintptr(fd), drainWindow)
	return got, err
}
