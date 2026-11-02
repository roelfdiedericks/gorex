//go:build !windows

package main

// importSupported tells that this system has PuTTY's and KiTTY's sessions
// to import: Windows only.
func importSupported() bool { return false }

// importSources reads the sessions of the KiTTY folders chosen; PuTTY and
// KiTTY keep theirs on Windows.
func importSources(chosen []string) (sessions []puttySession, problems []string) {
	for _, dir := range chosen {
		dir = sessionsDir(dir)
		sessions = append(sessions, kittyFolder(dir, dir, func(b []byte) string { return string(b) })...)
	}
	return sessions, nil
}
