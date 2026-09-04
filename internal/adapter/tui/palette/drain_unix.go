//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package palette

import (
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

const drainWindow = 50 * time.Millisecond

// DrainStdin discards pending terminal responses before the TUI takes over stdin.
func DrainStdin() {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return
	}
	drainFD(os.Stdin.Fd(), drainWindow)
}

func drainFD(fd uintptr, maxWait time.Duration) {
	if err := setNonblock(fd, true); err != nil {
		return
	}
	defer setNonblock(fd, false)

	deadline := time.Now().Add(maxWait)
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, err := unix.Read(int(fd), buf)
		if n > 0 {
			deadline = time.Now().Add(10 * time.Millisecond)
			continue
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		break
	}
}

func setNonblock(fd uintptr, nonblock bool) error {
	flags, err := unix.FcntlInt(fd, unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	if nonblock {
		flags |= unix.O_NONBLOCK
	} else {
		flags &^= unix.O_NONBLOCK
	}
	_, err = unix.FcntlInt(fd, unix.F_SETFL, flags)
	return err
}
