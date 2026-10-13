package rex

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
