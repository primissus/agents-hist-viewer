//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package palette

// QueryTerminal is unsupported on this platform.
func QueryTerminal(indices []int) (map[int]string, error) {
	return nil, ErrUnsupported
}
