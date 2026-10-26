package rex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The file operations the server does for the app's file browser and
// editor: on the host, as the user the server runs as. Paths are the
// host's; the server cleans them and expands nothing.

func fileInfo(fi fs.FileInfo) FileInfo {
	return FileInfo{Name: fi.Name(), Size: fi.Size(), Mode: uint32(fi.Mode()), ModTime: fi.ModTime(), Dir: fi.IsDir()}
}

func filePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("no path")
	}
	return filepath.Clean(path), nil
}

// statFile answers stat: the file's info, following symbolic links.
func statFile(path string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return fileInfo(fi), nil
}

// readDir answers readDir: the entries of a directory by name, each
// described as stat does (through symbolic links; one that is broken is
// listed as it is).
func readDir(path string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	out := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		fi, err := os.Stat(filepath.Join(path, e.Name()))
		if err != nil {
			if fi, err = e.Info(); err != nil {
				continue
			}
		}
		info := fileInfo(fi)
		info.Name = e.Name()
		out = append(out, info)
	}
	return out, nil
}

// readFile answers readFile: the file's bytes and modification time, or
// ErrTooLarge when it is over max bytes (0 for no cap) without reading it.
func readFile(path string, max int64) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if max > 0 && fi.Size() > max {
		return nil, fmt.Errorf("%s: %d bytes", ErrTooLarge, fi.Size())
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return FileData{Data: data, ModTime: fi.ModTime()}, nil
}

// writeFile answers writeFile: it writes data to a file beside the target
// and renames it over, keeping the target's mode when it existed, and
// refuses with ErrChanged when ifModTime is set and the target was
// modified since. It answers the time written.
func writeFile(path string, data []byte, ifModTime time.Time) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		if fi.IsDir() {
			return nil, fmt.Errorf("%s is a directory", path)
		}
		if !ifModTime.IsZero() && !fi.ModTime().Equal(ifModTime) {
			return nil, errors.New(ErrChanged)
		}
		mode = fi.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	tmp := path + ".gorex-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return FileData{ModTime: fi.ModTime()}, nil
}

// maxGitFiles caps what gitStatus lists.
const maxGitFiles = 20000

// gitStatus answers gitStatus: the repository the directory is in and
// the files git sees as changed, by the user's own git. No git, or no
// repository: an empty answer.
func gitStatus(path string) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return GitStatus{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	top := exec.CommandContext(ctx, git, "-C", path, "rev-parse", "--show-toplevel", "--show-prefix")
	hideWindow(top)
	out, err := top.Output()
	if err != nil {
		return GitStatus{}, nil
	}
	lines := strings.SplitN(string(out), "\n", 3)
	if len(lines) < 2 || strings.TrimSpace(lines[0]) == "" {
		return GitStatus{}, nil
	}
	// Git names the toplevel with links resolved (macOS's /tmp is
	// /private/tmp), under which the window finds none of its paths: the
	// root is the asked path itself, up as many directories as git's
	// prefix of it has. A link within the repository to a directory at
	// another depth miscounts them.
	root := path
	if prefix := strings.Trim(strings.TrimSuffix(lines[1], "\r"), "/"); prefix != "" {
		for range strings.Count(prefix, "/") + 1 {
			root = filepath.Dir(root)
		}
	}
	st := exec.CommandContext(ctx, git, "-C", root, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignored=matching")
	hideWindow(st)
	out, err = st.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %v", err)
	}
	files, truncated := parsePorcelain(out, maxGitFiles)
	return GitStatus{Root: root, Files: files, Truncated: truncated}, nil
}

// parsePorcelain reads git status --porcelain=v2 -z: records separated
// by NUL, a renamed or copied file's record followed by its old path as a
// record of its own.
func parsePorcelain(out []byte, max int) (files []GitChange, truncated bool) {
	recs := strings.Split(string(out), "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if rec == "" {
			continue
		}
		var status, path string
		switch rec[0] {
		case '1', '2', 'u':
			// "1 XY sub mH mI mW hH hI path", "2 XY sub mH mI mW hH hI Xscore path",
			// "u XY sub m1 m2 m3 mW h1 h2 h3 path": the path is the last field.
			fields := strings.SplitN(rec, " ", 2)
			if len(fields) < 2 {
				continue
			}
			rest := fields[1]
			xy := rest[:min(2, len(rest))]
			n := 8
			if rec[0] == '2' {
				n = 9
			}
			if rec[0] == 'u' {
				n = 10
			}
			parts := strings.SplitN(rest, " ", n)
			if len(parts) < n {
				continue
			}
			path = parts[n-1]
			switch {
			case rec[0] == 'u':
				status = "C"
			case len(xy) == 2 && xy[1] != '.':
				status = string(xy[1])
			case len(xy) >= 1:
				status = string(xy[0])
			}
			if rec[0] == '2' {
				i++ // the old path
			}
		case '?':
			status, path = "U", strings.TrimPrefix(rec, "? ")
		case '!':
			status, path = "I", strings.TrimPrefix(rec, "! ")
		default:
			continue
		}
		if path == "" || status == "" || status == "." {
			continue
		}
		if len(files) >= max {
			return files, true
		}
		files = append(files, GitChange{Path: path, Status: status})
	}
	return files, false
}
