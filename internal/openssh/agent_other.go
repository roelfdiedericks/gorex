//go:build !windows

package openssh

// PageantPipe returns Pageant's pipe, and whether Pageant runs: none but on
// Windows.
func PageantPipe() (string, bool) { return "", false }
