package rex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// readGitFile answers readGitFile: a file's bytes as git has them at a
// revision, a commit-ish (HEAD, a branch, a sha) or :0 to :3 for the
// index's stages, by the user's own git, in the repository the file is
// in, with the id of their blob; Unchanged alone when that is blob, the
// one the client has; ErrTooLarge when it is over max bytes (0 for no
// cap) without reading it. The file need not be on disk: one whose
// folders are gone is named from the nearest folder that is there.
func readGitFile(path, rev, blob string, max int64) (any, error) {
	path, err := filePath(path)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s is not an absolute path", path)
	}
	if err := gitRevOK(rev); err != nil {
		return nil, err
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git is not installed")
	}
	// rev:./rel names the file relative to the folder git runs in, as
	// the repository has it: no need to know the repository's root, nor
	// how symbolic links on the way to it resolve.
	dir, rel := gitAnchor(path)
	spec := rev + ":./" + rel
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The blob's id first: a client that has it is answered with that
	// one call, and the bytes are read by id, as of that moment.
	out, err := gitOutput(exec.CommandContext(ctx, git, "-C", dir, "rev-parse", spec))
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(string(out))
	if !gitObjectID(id) {
		return nil, fmt.Errorf("git rev-parse: %q", out)
	}
	if id == blob {
		return GitFileData{Blob: id, Unchanged: true}, nil
	}
	if out, err = gitOutput(exec.CommandContext(ctx, git, "-C", dir, "cat-file", "-s", id)); err != nil {
		return nil, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("git cat-file -s: %q", out)
	}
	if max > 0 && size > max {
		return nil, fmt.Errorf("%s: %d bytes", ErrTooLarge, size)
	}
	// cat-file blob is git show's bytes for a file, without its
	// listing of a folder or any textconv.
	data, err := gitOutput(exec.CommandContext(ctx, git, "-C", dir, "cat-file", "blob", id))
	if err != nil {
		return nil, err
	}
	return GitFileData{Data: data, Blob: id}, nil
}

// gitObjectID tells an object id: 40 hex digits, or 64 in a SHA-256
// repository.
func gitObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// gitRevOK refuses a revision git could read as an option or as more
// than one argument, and a colon but in an index stage (:0 to :3),
// which would name another path.
func gitRevOK(rev string) error {
	switch {
	case rev == "":
		return errors.New("no revision")
	case rev[0] == '-':
		return fmt.Errorf("revision %q starts with -", rev)
	case strings.ContainsFunc(rev, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }):
		return fmt.Errorf("revision %q has spaces or control characters", rev)
	case strings.Contains(rev, ":") && !(len(rev) == 2 && rev[0] == ':' && rev[1] >= '0' && rev[1] <= '3'):
		return fmt.Errorf("revision %q has a colon", rev)
	}
	return nil
}

// gitAnchor splits an absolute path into the nearest folder of it that
// exists and the rest, with slashes: where git runs, and the file
// relative to it.
func gitAnchor(path string) (dir, rel string) {
	dir, rel = filepath.Dir(path), filepath.Base(path)
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir, rel
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir, rel
		}
		rel = filepath.Base(dir) + "/" + rel
		dir = parent
	}
}

// gitOutput runs git and returns its output, or what it said on
// failing.
func gitOutput(cmd *exec.Cmd) ([]byte, error) {
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, errors.New("git: " + strings.TrimSpace(strings.TrimPrefix(string(ee.Stderr), "fatal: ")))
		}
		return nil, fmt.Errorf("git: %v", err)
	}
	return out, nil
}
