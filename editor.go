package main

import (
	"bytes"
	"fmt"
	"log"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"gorex/internal/rex"
)

// An editor tab: one file browser on the left, a tree of editor panes on
// the right, as the terminal tabs' trees. Files are the host's, read and
// written through the server; buffers live in the window.

const (
	modeEditor  = "editor"
	modeChanges = "changes"
	// browserWidth is a new browser's share of the tab, and browserMin
	// the least width it keeps.
	browserWidth = 0.25
	browserMin   = 160
)

// browserState is a file browser pane's: the root it shows, the entries
// of each directory read so far, and the outline's state.
type browserState struct {
	root   string
	hidden bool // hidden files shown
	// dirs holds each directory's entries once read; loading the ones
	// being read, and errs why a directory could not be read.
	dirs    map[string][]rex.FileInfo
	loading map[string]bool
	errs    map[string]string
	isDir   map[string]bool
	tree    ui.OutlineState[string]
	row     int
	// reveal is a file the tree is to show chosen, its folders opened,
	// once its rows are there; revealed the last file shown so, which
	// is the focused pane's: the tree follows it.
	reveal, revealed string
	// rootErr is why the root cannot be read, shown in place of the tree.
	rootErr string
	// changeRoot is the Change root… field, open while editing.
	changing  bool
	rootField string
	menuOpen  bool
	// watched are the directories the server watches for this browser,
	// on watchClient; repos what git says of the repositories under the
	// root, and noRepo the directories asked about that are in none.
	watched     map[string]bool
	watchClient *rex.Client
	repos       []*gitRepo
	noRepo      map[string]bool
}

func (b *browserState) saved(ratio float32) *savedBrowser {
	s := &savedBrowser{Root: b.root, Ratio: ratio, Hidden: b.hidden}
	for p := range b.tree.Open.All() {
		s.Open = append(s.Open, p)
	}
	slices.Sort(s.Open)
	return s
}

// editorState is an editor pane's: the file, its buffer, what it was
// when read for the diff, and how the pane shows it.
type editorState struct {
	path string
	mode string // modeEditor or modeChanges
	// buf is the text edited, base what the file held when read, and
	// modTime when the file was last modified as read or written.
	buf     *editor.Buffer
	base    *editor.Buffer
	state   *editor.State
	diff    *editor.DiffState
	modTime time.Time
	// loading tells the file is being read; err why it could not be, or
	// why it is not shown; tooLarge tells err is the cap, which Open
	// anyway lifts.
	loading  bool
	err      string
	tooLarge bool
	// elsewhere notes that another window closed this pane's place in
	// the layout while it had unsaved changes.
	elsewhere bool
	// wrap is the pane's soft wrap, from the setting until toggled.
	wrap *bool
	// onDisk notes a file changed or gone on the host since it was read,
	// while the buffer has changes of its own.
	onDisk string
	// preview is the pane's Markdown preview while in that mode;
	// previewOnLoad asks for it once the file is read, scrolled to
	// previewFragment.
	preview         *previewState
	previewOnLoad   bool
	previewFragment string
}

func (e *editorState) label() (name, detail string) {
	if e.path == "" {
		return "No file", ""
	}
	return baseName(e.path), shortDir(dirName(e.path))
}

func (e *editorState) modified() bool { return e.buf != nil && e.buf.Modified() }

// baseName and dirName split a path of the host's, whose separators the
// host's system decides.
func baseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func dirName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		if i == 0 {
			return p[:1]
		}
		if i == 2 && p[1] == ':' { // C:\
			return p[:3]
		}
		return p[:i]
	}
	return p
}

// joinPath joins a directory of the host's and a name.
func joinPath(dir, name string) string {
	if strings.HasSuffix(dir, "/") || strings.HasSuffix(dir, `\`) {
		return dir + name
	}
	sep := "/"
	if hostGOOS == "windows" {
		sep = `\`
	}
	return dir + sep + name
}

// expandHome replaces a leading ~ with the host's home.
func expandHome(p string) string {
	if p == "~" {
		return hostHome
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return joinPath(hostHome, p[2:])
	}
	return p
}

// update runs fn on the main thread of the window, or in the next frame
// of a view without one.
func (a *App) update(fn func()) {
	if win := a.win; win != nil {
		win.Update(fn)
	} else {
		a.post(fn)
	}
}

// newEditorTab opens an editor tab rooted at the host's home, with one
// empty editor pane, and focuses its browser.
func (a *App) newEditorTab(root string) *Tab {
	if a.client == nil {
		return nil
	}
	if root == "" {
		root = hostHome
	}
	t := &Tab{ID: a.id(), Kind: paneEditor, BrowserRatio: browserWidth}
	t.Browser = a.newBrowserPane(t, &savedBrowser{Root: root, Hidden: is(a.eff.Editor.HiddenFiles)})
	p := a.newEditorPane("")
	t.Root = &Node{ID: a.id(), Pane: p}
	p.Tab, p.Node = t, t.Root
	t.setFocus(p)
	t.setFocus(t.Browser)
	at := min(a.active+1, len(a.tabs))
	a.tabs = slices.Insert(a.tabs, at, t)
	a.active = at
	a.focusReq = t.Browser
	a.changed()
	return t
}

// newBrowserPane makes a tab's file browser from its saved state.
func (a *App) newBrowserPane(t *Tab, s *savedBrowser) *Pane {
	b := &browserState{root: s.Root, hidden: s.Hidden, dirs: map[string][]rex.FileInfo{}, loading: map[string]bool{}, errs: map[string]string{}, isDir: map[string]bool{}, row: -1, noRepo: map[string]bool{}}
	b.isDir[b.root] = true
	b.tree.Open.Add(b.root)
	for _, d := range s.Open {
		b.isDir[d] = true
		b.tree.Open.Add(d)
	}
	if t.BrowserRatio == 0 {
		t.BrowserRatio = browserWidth
	}
	p := &Pane{ID: a.id(), Kind: paneBrowser, Tab: t, files: b}
	return p
}

// newEditorPane makes an editor pane on a file, "" for an empty one, and
// reads the file in the background; a buffer kept from before a
// reconnect with unsaved changes is taken over instead.
func (a *App) newEditorPane(path string) *Pane {
	p := &Pane{ID: a.id(), Kind: paneEditor, edit: &editorState{path: path, mode: modeEditor}}
	if kept, ok := a.keptEdits[path]; ok && path != "" {
		delete(a.keptEdits, path)
		p.edit = kept
		p.edit.elsewhere = false
		return p
	}
	if viewable(path) != "" {
		p.edit.mode = modeView
	} else if path != "" {
		a.loadFile(p, 0)
	}
	return p
}

// loadFile reads the pane's file from the host, of at most max bytes
// (the setting when 0, no cap when -1), and shows it.
func (a *App) loadFile(p *Pane, max int64) {
	e := p.edit
	client := a.client
	if client == nil {
		e.err = "Not connected to the server."
		return
	}
	if max == 0 {
		max = a.eff.Editor.MaxFile
	}
	if max < 0 {
		max = 0
	}
	e.loading, e.err, e.tooLarge = true, "", false
	path := e.path
	go func() {
		fd, err := client.ReadFile(path, max)
		a.update(func() {
			if p.edit != e || e.path != path {
				return
			}
			e.loading = false
			if err != nil {
				e.err = err.Error()
				if strings.Contains(err.Error(), rex.ErrTooLarge) {
					e.tooLarge = true
					e.err = fmt.Sprintf("%s is %s; files over %s are not opened by themselves.", baseName(path), sizeText(sizeIn(err.Error())), sizeText(max))
				}
				return
			}
			if !utf8.Valid(fd.Data) || bytes.IndexByte(fd.Data[:min(len(fd.Data), 8000)], 0) >= 0 {
				e.err = baseName(path) + " is not a text file."
				return
			}
			a.setFileText(p, string(fd.Data), fd.ModTime)
		})
	}()
}

// sizeIn reads the byte count a "too large: N bytes" error carries.
func sizeIn(msg string) int64 {
	var n int64
	fmt.Sscanf(msg[strings.LastIndex(msg, ":")+1:], " %d bytes", &n)
	return n
}

// sizeText shows bytes as KB or MB.
func sizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// setFileText gives an editor pane its file's text as read.
func (a *App) setFileText(p *Pane, text string, mod time.Time) {
	e := p.edit
	lang := editor.LanguageFor(e.path)
	e.base = editor.NewBuffer(text)
	e.base.SetLanguage(lang)
	e.buf = editor.NewBuffer(text)
	e.buf.SetLanguage(lang)
	e.state = &editor.State{Buffer: e.buf, DiffBase: e.base}
	e.diff = &editor.DiffState{Old: e.base, New: e.buf, Editable: true, HunkActions: true, LineSelection: true}
	e.modTime = mod
	e.err, e.tooLarge, e.onDisk = "", false, ""
	if e.mode == modePreview && !isMarkdown(e.path) {
		e.mode = modeEditor
	}
	if e.previewOnLoad {
		e.previewOnLoad = false
		if isMarkdown(e.path) {
			e.mode = modePreview
		}
	}
	if e.mode == modePreview {
		a.ensurePreview(p)
		if e.previewFragment != "" {
			a.previewScrollTo(p, e.previewFragment)
			e.previewFragment = ""
		}
	} else if e.preview != nil && !isMarkdown(e.path) {
		a.dropPreview(p)
	}
}

// openFile shows a file in the tab's focused editor pane, or in a new
// split when that pane has unsaved changes or split is asked; a pane
// already on the file is focused instead.
func (a *App) openFile(t *Tab, path string, split bool) {
	for _, p := range t.panes() {
		if p.Kind == paneEditor && p.edit.path == path {
			// Its page showing something a link opened: the document.
			if pv := p.edit.preview; pv != nil && pv.showing != "" && p.edit.mode == modePreview {
				a.previewBack(p)
			}
			t.setFocus(p)
			a.focusReq = p
			return
		}
	}
	target := t.treeFocus()
	if target == nil {
		return
	}
	if split || target.edit.modified() || target.edit.path != "" && target.edit.loading {
		target = a.splitPane(target, false)
	}
	// The pane's page stays for the next Markdown file, or one the
	// engine shows, with no reload.
	var pv *previewState
	if isMarkdown(path) || viewable(path) != "" {
		pv = target.edit.preview
		target.edit.preview = nil
	}
	a.dropPreview(target)
	if kept, ok := a.keptEdits[path]; ok {
		delete(a.keptEdits, path)
		target.edit = kept
	} else if viewable(path) != "" {
		// Shown by the engine, in View: the frame asks the page.
		target.edit = &editorState{path: path, mode: modeView}
	} else {
		target.edit = &editorState{path: path, mode: modeEditor, previewOnLoad: isMarkdown(path) && is(a.eff.Editor.PreviewOnOpen)}
		a.loadFile(target, 0)
	}
	if pv != nil {
		a.dropPreview(target)
		target.edit.preview = pv
	}
	t.setFocus(target)
	a.focusReq = target
	a.changed()
}

// saveFile writes an editor pane's buffer to the host, refusing a file
// changed on disk since it was read, which asks what to do.
func (a *App) saveFile(p *Pane) {
	e := p.edit
	if e == nil || e.buf == nil || e.path == "" || a.client == nil {
		return
	}
	a.writeFile(p, e.modTime)
}

func (a *App) writeFile(p *Pane, ifMod time.Time) {
	e := p.edit
	client := a.client
	path, data := e.path, []byte(e.buf.String())
	buf := e.buf
	go func() {
		mod, err := client.WriteFile(path, data, ifMod)
		a.update(func() {
			if p.edit != e {
				return
			}
			if err != nil {
				if strings.Contains(err.Error(), rex.ErrChanged) {
					a.askChanged(p)
					return
				}
				a.toast(ui.Toast{ID: "save/" + path, Title: "Couldn't save " + baseName(path), Description: err.Error(), Type: "error"})
				return
			}
			e.modTime = mod
			if e.buf == buf && string(data) == buf.String() {
				buf.MarkSaved()
				e.base.Reset(string(data))
			}
			a.toast(ui.Toast{ID: "save/" + path, Title: "Saved " + baseName(path), Timeout: 1500 * time.Millisecond})
		})
	}()
}

// askChanged asks about a file changed on disk since it was read:
// overwrite it, reload it (dropping the edits), or leave it.
func (a *App) askChanged(p *Pane) {
	e := p.edit
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: a.win, Type: mygo.MessageWarning, Title: baseName(e.path) + " changed on disk",
		Message: "Something else wrote the file since it was opened here. Overwrite it with your edits, or reload it and lose them?",
		Buttons: []string{"Overwrite", "Reload", "Cancel"}, DefaultButton: 2, CancelButton: 2,
	})
	if err != nil {
		return
	}
	switch res.Button {
	case 0:
		a.writeFile(p, time.Time{})
	case 1:
		a.loadFile(p, 0)
	}
}

// reloadFile reads a pane's file again, keeping nothing of its edits.
func (a *App) reloadFile(p *Pane) {
	if p.edit.mode == modeView {
		if pv := p.edit.preview; pv != nil && pv.web != nil {
			a.showInPage(pv, p.edit.path)
		}
		return
	}
	if p.edit.path != "" {
		a.loadFile(p, 0)
	}
}

// confirmUnsaved asks, for each pane with unsaved changes, to save it,
// drop the changes, or stop; it reports whether to go on.
func (a *App) confirmUnsaved(panes []*Pane) bool {
	for _, p := range panes {
		if p.Kind != paneEditor || !p.edit.modified() {
			continue
		}
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: a.win, Type: mygo.MessageWarning, Title: "Save " + baseName(p.edit.path) + "?",
			Message: "The file has unsaved changes.",
			Buttons: []string{"Save", "Don't Save", "Cancel"}, DefaultButton: 0, CancelButton: 2,
		})
		if err != nil {
			continue
		}
		switch res.Button {
		case 0:
			a.saveFile(p)
		case 2:
			return false
		}
	}
	return true
}

// modifiedPanes lists the editor panes with unsaved changes.
func (a *App) modifiedPanes() []*Pane {
	var out []*Pane
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.Kind == paneEditor && p.edit.modified() {
				out = append(out, p)
			}
		}
	}
	return out
}

// keepEdits keeps the buffers of editor panes with unsaved changes as
// the window detaches, for the panes of the next connection to take
// over by path.
func (a *App) keepEdits() {
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.Kind == paneEditor {
				a.dropPreview(p)
			}
			if p.Kind == paneEditor && p.edit.modified() && p.edit.path != "" {
				if a.keptEdits == nil {
					a.keptEdits = map[string]*editorState{}
				}
				a.keptEdits[p.edit.path] = p.edit
			}
		}
	}
}

// adoptKeptEdits gives the buffers kept across a reconnect that no pane
// of the restored layout took a pane: split into the first editor tab,
// or an editor tab of their own, noted as closed elsewhere.
func (a *App) adoptKeptEdits() {
	if len(a.keptEdits) == 0 {
		return
	}
	paths := make([]string, 0, len(a.keptEdits))
	for p := range a.keptEdits {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	var t *Tab
	for _, tab := range a.tabs {
		if tab.Kind == paneEditor {
			t = tab
			break
		}
	}
	if t == nil {
		t = a.newEditorTab(dirName(paths[0]))
		if t == nil {
			return
		}
	}
	for _, path := range paths {
		kept := a.keptEdits[path]
		delete(a.keptEdits, path)
		kept.elsewhere = true
		target := t.treeFocus()
		if target.edit.path != "" {
			target = a.splitPane(target, false)
		}
		target.edit = kept
	}
	a.changed()
}

// browserEntries lists a directory's entries for the tree, folders first
// and by name without case, reading it once; nil while it loads.
func (a *App) browserEntries(p *Pane, dir string) []string {
	b := p.files
	if !b.isDir[dir] {
		return nil
	}
	entries, ok := b.dirs[dir]
	if !ok {
		a.readDir(p, dir)
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, fi := range entries {
		if !b.hidden && strings.HasPrefix(fi.Name, ".") {
			continue
		}
		out = append(out, joinPath(dir, fi.Name))
	}
	return out
}

// readDir reads a directory of the browser's from the host, once.
func (a *App) readDir(p *Pane, dir string) {
	b := p.files
	client := a.client
	if client == nil || b.loading[dir] {
		return
	}
	b.loading[dir] = true
	go func() {
		entries, err := client.ReadDir(dir)
		a.update(func() {
			if p.files != b {
				return
			}
			delete(b.loading, dir)
			if err != nil {
				log.Printf("reading %s: %v", dir, err)
				b.errs[dir] = err.Error()
				if dir == b.root {
					b.rootErr = err.Error()
				}
				b.dirs[dir] = nil
				return
			}
			delete(b.errs, dir)
			if dir == b.root {
				b.rootErr = ""
			}
			slices.SortFunc(entries, func(x, y rex.FileInfo) int {
				if x.Dir != y.Dir {
					if x.Dir {
						return -1
					}
					return 1
				}
				return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
			})
			for _, fi := range entries {
				if fi.Dir {
					b.isDir[joinPath(dir, fi.Name)] = true
				}
			}
			b.dirs[dir] = entries
			a.askGit(p, dir)
		})
	}()
}

// refreshDir drops what the browser read of a directory and what is
// under it, to read it again.
func (a *App) refreshDir(p *Pane, dir string) {
	b := p.files
	for d := range b.dirs {
		if d == dir || strings.HasPrefix(d, joinPath(dir, "")) {
			delete(b.dirs, d)
		}
	}
	delete(b.noRepo, dir)
	if r := a.repoOf(b, dir); r != nil {
		a.scheduleGit(p, r)
	}
	a.readDir(p, dir)
}

// setRoot moves the browser to another root.
func (a *App) setRoot(p *Pane, root string) {
	b := p.files
	root = strings.TrimSpace(expandHome(root))
	if root == "" {
		return
	}
	if hostGOOS == "windows" {
		root = strings.ReplaceAll(root, "/", `\`)
		if len(root) == 2 && root[1] == ':' {
			root += `\`
		}
	} else {
		root = path.Clean(root)
	}
	a.unwatchAll(p)
	b.root, b.rootErr = root, ""
	b.repos, b.noRepo = nil, map[string]bool{}
	b.dirs, b.loading, b.errs, b.isDir = map[string][]rex.FileInfo{}, map[string]bool{}, map[string]string{}, map[string]bool{root: true}
	b.tree.Open.Clear()
	b.tree.Open.Add(root)
	b.row, b.reveal, b.revealed = -1, "", ""
	a.changed()
}

// focusedEditor is the focused editor pane, nil when another kind has
// the focus.
func (a *App) focusedEditor() *Pane {
	if t := a.tab(); t != nil && t.Focus != nil && t.Focus.Kind == paneEditor {
		return t.Focus
	}
	return nil
}

// focusedBrowser is the file browser of the active tab, when it is an
// editor tab.
func (a *App) focusedBrowser() *Pane {
	if t := a.tab(); t != nil && t.Kind == paneEditor {
		return t.Browser
	}
	return nil
}

func (a *App) saveFocused() {
	if p := a.focusedEditor(); p != nil {
		a.saveFile(p)
	}
}

func (a *App) reloadFocused() {
	if p := a.focusedEditor(); p != nil {
		a.reloadFile(p)
	}
}

func (a *App) refreshFocusedBrowser() {
	if p := a.focusedBrowser(); p != nil {
		a.refreshFocusedDir(p)
	}
}

func (a *App) changeRootFocused() {
	if p := a.focusedBrowser(); p != nil {
		p.files.changing, p.files.rootField = true, p.files.root
		a.focusReq = p
	}
}

// Watching and git status: the server tells of directories the browsers
// have open, and git tells of the files it sees changed.

// gitRepo is what git says of a repository under a browser's root.
type gitRepo struct {
	root string
	// files holds each changed file's status by its path relative to
	// root, with "/"; dirs the strongest status under each directory.
	files, dirs map[string]string
	truncated   bool
	lastRun     time.Time
	due         bool // a run is scheduled
	running     bool
}

// gitStrength orders statuses for a folder's roll-up, the strongest
// first.
const gitStrength = "CDMARUTI"

func stronger(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if strings.Index(gitStrength, a) <= strings.Index(gitStrength, b) {
		return a
	}
	return b
}

// syncWatches has the server watch the directories the browser has open,
// and no others.
func (a *App) syncWatches(p *Pane) {
	b := p.files
	client := a.client
	if client == nil {
		return
	}
	if b.watchClient != client {
		b.watchClient, b.watched = client, map[string]bool{}
	}
	want := map[string]bool{b.root: true}
	for d := range b.tree.Open.All() {
		if b.isDir[d] {
			want[d] = true
		}
	}
	for d := range want {
		if !b.watched[d] {
			b.watched[d] = true
			go client.Watch(d)
		}
	}
	for d := range b.watched {
		if !want[d] {
			delete(b.watched, d)
			go client.Unwatch(d)
		}
	}
}

// unwatchAll drops a browser's watches, as its tab closes.
func (a *App) unwatchAll(p *Pane) {
	b := p.files
	if client := b.watchClient; client != nil {
		for d := range b.watched {
			go client.Unwatch(d)
		}
	}
	b.watched, b.watchClient = nil, nil
}

// dirChanged takes the server's word that a directory changed: browsers
// showing it read it again, editors on files in it look at them again,
// and git is asked again for the repository it is in.
func (a *App) dirChanged(dir string) {
	for _, t := range a.tabs {
		if t.Kind != paneEditor {
			continue
		}
		if b := t.Browser; b != nil {
			if _, ok := b.files.dirs[dir]; ok || samePath(dir, b.files.root) {
				delete(b.files.dirs, dir)
				a.readDir(b, dir)
			}
			if r := a.repoOf(b.files, dir); r != nil {
				a.scheduleGit(b, r)
			}
		}
		for _, p := range t.panes() {
			if p.Kind == paneEditor && p.edit.path != "" && samePath(dirName(p.edit.path), dir) {
				a.checkFile(p)
			}
		}
	}
}

// checkFile looks at an open file on the host: one that changed since it
// was read reloads when the buffer is unmodified, else is marked.
func (a *App) checkFile(p *Pane) {
	e := p.edit
	client := a.client
	if client == nil || e.buf == nil || e.loading {
		return
	}
	path, had := e.path, e.modTime
	go func() {
		fi, err := client.Stat(path)
		a.update(func() {
			if p.edit != e || e.path != path || e.modTime != had {
				return
			}
			switch {
			case err != nil:
				e.onDisk = "gone from disk"
			case !fi.ModTime.Equal(had):
				if e.modified() {
					e.onDisk = "changed on disk"
				} else {
					a.loadFile(p, 0)
				}
			default:
				e.onDisk = ""
			}
		})
	}()
}

// repoOf finds the repository a path is in among those the browser
// knows.
func (a *App) repoOf(b *browserState, path string) *gitRepo {
	for _, r := range b.repos {
		if underDir(path, r.root) {
			return r
		}
	}
	return nil
}

// underDir reports whether path is dir or inside it.
func underDir(path, dir string) bool {
	if samePath(path, dir) {
		return true
	}
	dir = strings.TrimRight(dir, `/\`)
	return len(path) > len(dir) && samePath(path[:len(dir)], dir) && (path[len(dir)] == '/' || path[len(dir)] == '\\')
}

// gitStatusOf is what git says of an item of the tree: a file's status,
// or the strongest under a folder; "" for nothing.
func (a *App) gitStatusOf(b *browserState, item string) string {
	r := a.repoOf(b, item)
	if r == nil {
		return ""
	}
	if samePath(item, r.root) {
		return ""
	}
	rel := strings.ReplaceAll(item[len(strings.TrimRight(r.root, `/\`))+1:], `\`, "/")
	if st, ok := r.files[rel]; ok {
		return st
	}
	if st, ok := r.files[rel+"/"]; ok {
		return st
	}
	return r.dirs[rel]
}

// askGit asks git about a directory just read, unless it is in a
// repository the browser knows or one it asked about without a result.
func (a *App) askGit(p *Pane, dir string) {
	b := p.files
	if a.repoOf(b, dir) != nil || b.noRepo[dir] {
		return
	}
	for d := range b.noRepo {
		if underDir(dir, d) && !b.isDir[d] {
			return
		}
	}
	a.runGit(p, dir, nil)
}

// scheduleGit runs git again for a repository, at most every two
// seconds.
func (a *App) scheduleGit(p *Pane, r *gitRepo) {
	if r.due || r.running {
		return
	}
	wait := 2*time.Second - time.Since(r.lastRun)
	if wait <= 0 {
		a.runGit(p, r.root, r)
		return
	}
	r.due = true
	time.AfterFunc(wait, func() {
		a.update(func() {
			r.due = false
			if a.repoOf(p.files, r.root) == r {
				a.runGit(p, r.root, r)
			}
		})
	})
}

// runGit asks the server for git's status of the repository dir is in,
// and keeps it on the browser.
func (a *App) runGit(p *Pane, dir string, r *gitRepo) {
	b := p.files
	client := a.client
	if client == nil {
		return
	}
	if r != nil {
		r.running = true
	}
	go func() {
		st, err := client.GitStatus(dir)
		a.update(func() {
			if p.files != b || a.client != client {
				return
			}
			if r != nil {
				r.running = false
			}
			if err != nil || st.Root == "" {
				if r == nil {
					b.noRepo[dir] = true
				}
				return
			}
			if r == nil {
				r = a.repoOf(b, st.Root)
			}
			if r == nil {
				r = &gitRepo{root: st.Root}
				b.repos = append(b.repos, r)
			}
			r.lastRun = time.Now()
			r.truncated = st.Truncated
			r.files = make(map[string]string, len(st.Files))
			r.dirs = map[string]string{}
			for _, f := range st.Files {
				r.files[f.Path] = f.Status
				rel := strings.TrimSuffix(f.Path, "/")
				for i := strings.LastIndex(rel, "/"); i > 0; i = strings.LastIndex(rel[:i], "/") {
					d := rel[:i]
					r.dirs[d] = stronger(r.dirs[d], f.Status)
				}
			}
		})
	}()
}
