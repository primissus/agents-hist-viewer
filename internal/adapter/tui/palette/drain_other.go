//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package palette

// DrainStdin is a no-op on unsupported platforms.
func DrainStdin() {}
