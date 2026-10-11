package remote

import "testing"

func TestPlatformOf(t *testing.T) {
	for system, want := range map[string]string{
		"Linux x86_64":    "linux-amd64",
		"Linux amd64":     "linux-amd64",
		"Linux aarch64":   "linux-arm64",
		"Linux arm64":     "linux-arm64",
		"Darwin x86_64":   "darwin-amd64",
		"Darwin arm64":    "darwin-arm64",
		" Darwin  arm64 ": "darwin-arm64",
		"Linux armv7l":    "",
		"FreeBSD amd64":   "",
		"Darwin":          "",
		"":                "",
	} {
		p, ok := platformOf(system)
		if ok != (want != "") || p.name != want {
			t.Errorf("platformOf(%q) = %q, %v; want %q", system, p.name, ok, want)
		}
	}
}
