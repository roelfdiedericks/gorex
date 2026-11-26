package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// Files dragged from the desktop and dropped on a folder of the file
// panel or an editor tab's tree, or on a terminal, go to the window's
// host: written by its server through connections of their own
// (rex.Upload), two at a time, with a toast telling how far they are and
// a Cancel. A name that exists asks Replace, Keep Both or Skip. Folders
// go with all they hold; one dropped on a folder of its name goes into
// it. Files keep their modification time.

// uploadEntry is a folder to make or a file to write on the host.
type uploadEntry struct {
	local, remote string
	dir           bool
	size          int64
	exec          bool
	mod           time.Time
	replace       bool
}

// uploadPlan is what a drop uploads: its entries in order, each folder
// before what it holds; top, the host's paths of what was dropped, as
// named there; the folders that get something new, to read again.
type uploadPlan struct {
	entries []uploadEntry
	top     []string
	files   int
	bytes   int64
	touched []string
}

// What to do with a name that exists.
const (
	clashReplace = iota
	clashKeep
	clashSkip
	clashCancel
)

var errUploadCancelled = errors.New("cancelled")

// uploadSeq numbers the uploads, for their toasts.
var uploadSeq atomic.Int64

// uploadDrop uploads the files and folders of paths, of this machine,
// into the host's folder dest; then, when given, gets the host's paths
// of what was dropped once all is there.
func (a *App) uploadDrop(paths []string, dest string, then func(top []string)) {
	if len(paths) == 0 || dest == "" {
		return
	}
	if a.bind.noFiles() {
		a.toast(ui.Toast{ID: "plain/editor", Title: "Can't upload files", Description: a.noFilesReason() + ".", Timeout: 4 * time.Second})
		return
	}
	client := a.client
	if client == nil {
		return
	}
	go func() {
		plan, err := a.planUpload(client, paths, dest)
		switch {
		case errors.Is(err, errUploadCancelled):
			return
		case err != nil:
			a.toast(ui.Toast{Title: "Can't upload to " + baseName(dest), Description: opError(err), Type: "error"})
			return
		case len(plan.entries) == 0:
			return
		}
		err = a.runUpload(client, plan, dest)
		a.update(func() {
			for _, dir := range append([]string{dest}, plan.touched...) {
				a.dirChanged(dir)
			}
			if err == nil && then != nil {
				then(plan.top)
			}
		})
	}()
}

// planUpload makes the plan of an upload of paths into dest, asking on
// the window what to do with each name that exists there.
func (a *App) planUpload(client *rex.Client, paths []string, dest string) (*uploadPlan, error) {
	plan := &uploadPlan{}
	listing := func(dir string) (map[string]bool, error) {
		fis, err := client.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		names := make(map[string]bool, len(fis))
		for _, fi := range fis {
			names[fi.Name] = fi.Dir
		}
		return names, nil
	}
	// The choice for every name that exists, once the user said so.
	always := -1
	var add func(local, remote string, fi os.FileInfo, existing map[string]bool) (string, error)
	walk := func(local, remote string, existing map[string]bool) error {
		entries, err := os.ReadDir(local)
		if err != nil {
			return err
		}
		for _, e := range entries {
			full := filepath.Join(local, e.Name())
			fi, err := os.Stat(full)
			switch {
			case err != nil:
				return err
			case e.Type()&os.ModeSymlink != 0 && fi.IsDir():
				continue // a link to a folder, which could hold itself
			case !fi.IsDir() && !fi.Mode().IsRegular():
				continue // a device, a socket or a pipe
			}
			if _, err := add(full, joinPath(remote, e.Name()), fi, existing); err != nil {
				return err
			}
		}
		return nil
	}
	add = func(local, remote string, fi os.FileInfo, existing map[string]bool) (string, error) {
		name := baseName(remote)
		replace := false
		if isDir, ok := existing[name]; ok {
			if fi.IsDir() && isDir {
				// A folder into the folder of its name.
				sub, err := listing(remote)
				if err != nil {
					return "", err
				}
				plan.touched = append(plan.touched, remote)
				return remote, walk(local, remote, sub)
			}
			canReplace := !fi.IsDir() && !isDir
			choice := always
			if choice < 0 || (choice == clashReplace && !canReplace) {
				var all bool
				choice, all = a.askClash(name, dirName(remote), canReplace)
				if all {
					always = choice
				}
			}
			switch choice {
			case clashSkip:
				return "", nil
			case clashCancel:
				return "", errUploadCancelled
			case clashKeep:
				name = freeName(name, fi.IsDir(), existing)
				remote = joinPath(dirName(remote), name)
			case clashReplace:
				replace = true
			}
		}
		existing[name] = fi.IsDir()
		if fi.IsDir() {
			plan.entries = append(plan.entries, uploadEntry{local: local, remote: remote, dir: true})
			return remote, walk(local, remote, map[string]bool{})
		}
		plan.entries = append(plan.entries, uploadEntry{local: local, remote: remote, size: fi.Size(),
			exec: fi.Mode().Perm()&0o111 != 0, mod: fi.ModTime(), replace: replace})
		plan.files++
		plan.bytes += fi.Size()
		return remote, nil
	}
	existing, err := listing(dest)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() && !fi.Mode().IsRegular() {
			continue
		}
		remote, err := add(p, joinPath(dest, filepath.Base(p)), fi, existing)
		if err != nil {
			return nil, err
		}
		if remote != "" {
			plan.top = append(plan.top, remote)
		}
	}
	return plan, nil
}

// askClash asks, on the window, what to do with a name that exists in the
// host's folder dir, and whether to do it for every other one.
func (a *App) askClash(name, dir string, canReplace bool) (choice int, all bool) {
	buttons := []string{"Replace", "Keep Both", "Skip", "Cancel"}
	choices := []int{clashReplace, clashKeep, clashSkip, clashCancel}
	message := "Replace it with the one you dropped, keep both, or skip it?"
	if !canReplace {
		// A file and a folder: one doesn't replace the other.
		buttons, choices = buttons[1:], choices[1:]
		message = "One is a file and the other a folder. Keep both, or skip it?"
	}
	ch := make(chan mygo.MessageResult, 1)
	a.update(func() {
		res, err := messageBox(mygo.MessageOptions{
			Parent: a.win, Type: mygo.MessageWarning, Title: name + " already exists in " + baseName(dir), Message: message,
			Buttons: buttons, DefaultButton: 0, CancelButton: len(buttons) - 1,
			CheckboxLabel: "Do this for the others too",
		})
		if err != nil {
			res = mygo.MessageResult{Button: len(buttons) - 1}
		}
		ch <- res
	})
	res := <-ch
	if res.Button < 0 || res.Button >= len(choices) {
		return clashCancel, false
	}
	return choices[res.Button], res.CheckboxChecked
}

// freeName is name with the first number in parentheses that no entry of
// existing has: notes (1).txt, archive (2).tar.gz, my.folder (1).
func freeName(name string, dir bool, existing map[string]bool) string {
	stem, ext := name, ""
	if i := strings.Index(name[min(1, len(name)):], "."); i >= 0 && !dir {
		stem, ext = name[:i+1], name[i+1:]
	}
	for n := 1; ; n++ {
		try := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if _, taken := existing[try]; !taken {
			return try
		}
	}
}

// uploadInto uploads files dropped on a browser pane into its folder dir,
// and shows the first of them in the tree once it is there.
func (a *App) uploadInto(p *Pane, files []string, dir string) {
	a.uploadDrop(files, dir, func(top []string) {
		if b := p.files; b != nil && len(top) > 0 {
			b.reveal = top[0]
			a.changed()
		}
	})
}

// runUpload makes the plan's folders and writes its files, two at a time,
// with a toast that tells how far it is and cancels it.
func (a *App) runUpload(client *rex.Client, plan *uploadPlan, dest string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := fmt.Sprintf("upload/%d", uploadSeq.Add(1))
	what := fmt.Sprintf("%d files", plan.files)
	if len(plan.top) == 1 {
		what = path.Base(strings.ReplaceAll(plan.top[0], `\`, "/"))
	}
	var sent, done atomic.Int64
	var once sync.Once
	var failed error
	fail := func(err error) {
		once.Do(func() { failed = err; cancel() })
	}
	show := func() {
		frac := 1.0
		if plan.bytes > 0 {
			frac = float64(sent.Load()) / float64(plan.bytes)
		}
		a.toast(ui.Toast{ID: id, Title: "Uploading " + what, Type: "working", Timeout: -1,
			Description: sizeText(sent.Load()) + " of " + sizeText(plan.bytes) + " to " + baseName(dest),
			Data:        toastProgress(frac), Action: "Cancel", OnAction: cancel})
	}
	show()
	stopShow := make(chan struct{})
	go func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stopShow:
				return
			case <-tick.C:
				show()
			}
		}
	}()
	files := make(chan uploadEntry)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for e := range files {
				if ctx.Err() != nil {
					continue
				}
				if err := a.uploadOne(ctx, client, e, &sent); err != nil {
					fail(fmt.Errorf("%s: %w", baseName(e.remote), err))
				}
				done.Add(1)
			}
		})
	}
	for _, e := range plan.entries {
		if ctx.Err() != nil {
			break
		}
		if e.dir {
			if err := client.Mkdir(e.remote); err != nil {
				fail(fmt.Errorf("%s: %w", baseName(e.remote), err))
			}
			continue
		}
		files <- e
	}
	close(files)
	wg.Wait()
	close(stopShow)
	switch {
	case failed != nil && errors.Is(failed, context.Canceled):
		a.toast(ui.Toast{ID: id, Title: "Upload cancelled", Description: fmt.Sprintf("%d of %d files uploaded to %s", done.Load(), plan.files, baseName(dest)), Timeout: 4 * time.Second})
	case failed != nil:
		a.toast(ui.Toast{ID: id, Title: "Couldn't upload " + what, Description: opError(failed), Type: "error", Timeout: -1})
	default:
		a.toast(ui.Toast{ID: id, Title: "Uploaded " + what, Description: sizeText(plan.bytes) + " to " + baseName(dest), Timeout: 3 * time.Second})
	}
	return failed
}

// uploadOne writes a file of the plan, adding what it sends to sent.
func (a *App) uploadOne(ctx context.Context, client *rex.Client, e uploadEntry, sent *atomic.Int64) error {
	f, err := os.Open(e.local)
	if err != nil {
		return err
	}
	defer f.Close()
	var last int64
	return client.Upload(ctx, rex.Upload{Path: e.remote, Size: e.size, Exec: e.exec, ModTime: e.mod, Replace: e.replace}, f, func(n int64) {
		sent.Add(n - last)
		last = n
	})
}

// toastProgress is a toast's progress bar, from 0 to 1 (Toast.Data).
type toastProgress float64

// dropOnTerminal takes files dropped on a terminal pane of a host's
// window: uploads them to the folder its program is in, then pastes their
// paths there, quoted for its shell, as a drop on a local terminal pastes
// the local ones.
func (a *App) dropOnTerminal(p *Pane, paths []string) {
	dir := p.info.Dir
	if dir == "" {
		dir = hostHome
	}
	a.uploadDrop(paths, dir, func(top []string) {
		if p.term == nil {
			return
		}
		quoted := make([]string, len(top))
		for i, t := range top {
			quoted[i] = hostQuote(t)
		}
		p.pasteText = true
		p.term.Paste(strings.Join(quoted, " "))
	})
}

// pasteLocalPaths pastes paths of this machine into a terminal, quoted,
// as a drop on the terminal's view does.
func pasteLocalPaths(p *Pane, paths []string) {
	if p.term == nil {
		return
	}
	quoted := make([]string, len(paths))
	for i, t := range paths {
		quoted[i] = hostQuote(t)
	}
	p.pasteText = true
	p.term.Paste(strings.Join(quoted, " "))
}

// hostQuote quotes a path of the host's as one argument of its shell:
// in double quotes on Windows when it needs them, else for a POSIX
// shell.
func hostQuote(s string) string {
	if hostGOOS == "windows" {
		if strings.ContainsAny(s, " &()[]{}^=;!'+,`~") {
			return `"` + s + `"`
		}
		return s
	}
	return shellQuote(s)
}
