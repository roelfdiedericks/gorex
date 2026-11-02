//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// cpACP is Windows' code page of the system (CP_ACP).
const cpACP = 0

// importSupported tells that this system has PuTTY's and KiTTY's sessions
// to import.
func importSupported() bool { return true }

// Where PuTTY and KiTTY keep their sessions in the registry, a key a
// session, its name escaped as %XX.
const (
	puttyRegistry = `Software\SimonTatham\PuTTY\Sessions`
	kittyRegistry = `Software\9bis.com\KiTTY\Sessions`
)

// importSources reads the saved sessions of PuTTY (its registry key), of
// KiTTY (its key, and the folders of a KiTTY that keeps its sessions in
// one: kittyFolders), and of the folders chosen, each a KiTTY's folder or
// its Sessions. problems are what could not be read.
func importSources(chosen []string) (sessions []puttySession, problems []string) {
	sessions = append(sessions, registrySessions(puttyRegistry, "PuTTY")...)
	sessions = append(sessions, registrySessions(kittyRegistry, "KiTTY")...)
	seen := map[string]bool{}
	folders := kittyFolders()
	for _, dir := range chosen {
		folders = append(folders, sessionsDir(dir))
	}
	for _, dir := range folders {
		key := strings.ToLower(filepath.Clean(dir))
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := os.Stat(dir); err != nil {
			problems = append(problems, "Can't read "+dir+": "+err.Error())
			continue
		}
		sessions = append(sessions, kittyFolder(dir, dir, ansiString)...)
	}
	return sessions, problems
}

// registrySessions reads the sessions under the key at path of the
// current user's registry: strings, and numbers as decimal text.
func registrySessions(path, source string) []puttySession {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, _ := k.ReadSubKeyNames(-1)
	var sessions []puttySession
	for _, n := range names {
		sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		values := map[string]string{}
		vnames, _ := sk.ReadValueNames(-1)
		for _, vn := range vnames {
			if s, _, err := sk.GetStringValue(vn); err == nil {
				values[vn] = s
			} else if i, _, err := sk.GetIntegerValue(vn); err == nil {
				values[vn] = strconv.FormatUint(i, 10)
			}
		}
		sk.Close()
		sessions = append(sessions, puttySession{Name: ansiString(unescapeBytes(n)), Source: source, Values: values})
	}
	return sessions
}

// kittyExe matches the names KiTTY goes by, and PuTTY's, which a KiTTY
// may be renamed to.
var kittyExe = regexp.MustCompile(`(?i)^(kitty|putty)[^\\/]*\.exe$`)

// kittyFolders finds the Sessions folders of the KiTTYs that keep their
// sessions in a folder: of each KiTTY running or on PATH, by its kitty.ini
// (savemode=dir, and configdir, its folder else), as KiTTY reads it: the
// file KITTY_INI_FILE names, kitty.ini or putty.ini beside it, or
// %APPDATA%\KiTTY\kitty.ini. A portable KiTTY keeps them so by default.
func kittyFolders() []string {
	var exes []string
	exes = append(exes, runningExes()...)
	for _, name := range []string{"kitty", "kitty_portable", "putty"} {
		if p, err := exec.LookPath(name); err == nil {
			exes = append(exes, p)
		}
	}
	var folders []string
	seen := map[string]bool{}
	for _, exe := range exes {
		// The same KiTTY may run many times, and be on PATH too.
		if seen[strings.ToLower(exe)] {
			continue
		}
		seen[strings.ToLower(exe)] = true
		dir := filepath.Dir(exe)
		var ini []byte
		for _, p := range []string{os.Getenv("KITTY_INI_FILE"), filepath.Join(dir, "kitty.ini"), filepath.Join(dir, "putty.ini"),
			filepath.Join(os.Getenv("APPDATA"), "KiTTY", "kitty.ini")} {
			if b, err := os.ReadFile(p); p != "" && err == nil {
				ini = b
				break
			}
		}
		mode, configdir := kittyIni(ini)
		portable := strings.Contains(strings.ToLower(filepath.Base(exe)), "portable")
		if mode != "dir" && !(mode == "" && portable) {
			continue
		}
		base := dir
		if configdir != "" {
			base = configdir
			if !filepath.IsAbs(base) {
				base = filepath.Join(dir, base)
			}
		}
		if st, err := os.Stat(filepath.Join(base, "Sessions")); err == nil && st.IsDir() {
			folders = append(folders, filepath.Join(base, "Sessions"))
		}
	}
	return folders
}

// runningExes are the paths of the programs running now named as KiTTY
// or PuTTY.
func runningExes() []string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var exes []string
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !kittyExe.MatchString(windows.UTF16ToString(e.ExeFile[:])) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		n := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
			exes = append(exes, windows.UTF16ToString(buf[:n]))
		}
		windows.CloseHandle(h)
	}
	return exes
}

// ansiString is text of Windows' code page as Go's: PuTTY's names and
// KiTTY's values are the bytes of the code page, %XX escaped.
func ansiString(b []byte) string {
	ascii := true
	for _, c := range b {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii || len(b) == 0 {
		return string(b)
	}
	n, err := windows.MultiByteToWideChar(cpACP, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n == 0 {
		return string(b)
	}
	w := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(cpACP, 0, &b[0], int32(len(b)), &w[0], n); err != nil {
		return string(b)
	}
	return windows.UTF16ToString(w)
}
