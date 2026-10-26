package main

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// A media node (kind media) names a file the window loads itself, by
// path, rather than bytes the program uploads: relative paths are under
// the pane's working directory, on the pane's host, read through the
// server as the editor reads files, or at a git revision (rev) through
// the server's git. Pictures and SVGs draw inline, a click showing a
// picture whole (surfaceZoom) and a disk SVG in GoRex's viewer; other
// files draw as a card naming the file, with Open when GoRex's editor
// tab shows that kind and the file is the disk's. Linked pan and zoom
// (link) is not drawn.

const (
	mediaPictureMax = 32 << 20 // the largest picture file read
	mediaSVGMax     = 512 << 10
	mediaSide       = 8192 // the most pixels a side of a picture decoded
	mediaKeep       = 64   // the most files kept loaded
	// mediaBudget is the most decoded pixels kept, 256 MiB of RGBA.
	mediaBudget = 64 << 20
	// mediaRecheck is how old a load may be before a drawing asks the
	// host whether the file changed.
	mediaRecheck = 3 * time.Second
)

// mediaData is what was read of a media file: whether it is there, its
// size and time, and a picture decoded or an SVG's source, else why not;
// read tells that bytes were read, whose checksum is sum, and blob is
// their git object id when read at a revision.
type mediaData struct {
	found   bool
	size    int64
	modTime time.Time
	err     string
	bitmap  *ui.Bitmap
	pixels  int64 // the bitmap's, width by height
	src     string
	read    bool
	sum     uint32
	blob    string
}

// mediaFile is a media file loaded from the host: what was read, and
// the SVG parsed for the theme it was parsed for. Only the main thread
// touches it; loads in flight write it through update.
type mediaFile struct {
	mediaData
	client  *rex.Client
	busy    bool // a load or a check in flight
	loaded  bool
	checked time.Time // when the host last answered for it
	used    uint64    // the frame it was last drawn in
	svg     *tspSVG
	svgDark bool
	svgDone bool // svg is parsed for svgDark, nil when it does not parse
}

// mediaFiles are the files media nodes loaded, by path on the host and
// revision.
var mediaFiles = map[string]*mediaFile{}

// tspMedia draws a media node: the picture or SVG, at its own size cut
// to the node's max and its container's width; else a card.
func (a *App) tspMedia(c *ui.Context, key *ui.Context, k *colors, p *Pane, n *rex.TSPNode) ui.Element {
	name, _ := n.Props["path"].(string)
	rev, _ := n.Props["rev"].(string)
	file := mediaPath(p, name)
	kind := mediaKind(file)
	cw, lh := mediaCell(p)
	var maxW, maxH, frac float32
	if m, ok := n.Props["max"].(map[string]any); ok {
		maxW, frac = mediaExtent(m["w"], cw, lh)
		maxH, _ = mediaExtent(m["h"], cw, lh)
	}
	pct := float32(100)
	if frac > 0 {
		pct = frac * 100
	}
	if name == "" {
		return a.mediaCard(c, key, k, p, "", "missing", "No file named", true, false)
	}
	// at names the revision after a file's name or size, "" for the disk.
	at := ""
	if rev != "" {
		at = " at " + rev
	}
	f := a.mediaEntry(file, rev, kind)
	if f == nil {
		return a.mediaCard(c, key, k, p, file, kind, mediaLabel(file, kind)+at+" · too many pictures on screen", true, false)
	}
	if !f.loaded {
		box := ui.Row(key).Gap(8).Padding(6, 12).Radius(6).Background(k.hover).AlignItems(ui.Center).AlignSelf(ui.Start)
		box.Children(func() {
			ui.Text(c, mediaGlyph(kind)).Font(symbolsFont).FontSize(15).TextColor(k.textMuted)
			ui.Text(c, "Loading "+baseName(file)+"…").FontSize(12).TextColor(k.textMuted)
		})
		return box
	}
	if !f.found {
		detail := f.err
		if mediaMissing(detail) {
			detail = "Not found" + at
		}
		return a.mediaCard(c, key, k, p, file, "missing", detail, true, false)
	}
	actions, _ := n.Props["actions"].(map[string]any)
	_, ownClick := actions["click"].(string)
	if bm := f.bitmap; bm != nil {
		bw, bh := bm.Size()
		w := mediaWidth(float32(bw), float32(bh), maxW, maxH)
		img := ui.Image(key, bm).Width(w).MaxWidthPercent(pct).AlignSelf(ui.Start).Radius(6).Tooltip(file + at)
		if !ownClick {
			img.Cursor(ui.CursorPointer)
			if img.Clicked() {
				a.tspZoom, a.tspZoomAlt, a.tspZoomPane = bm, baseName(file)+at, p
				// The viewer opens the disk file; one at a rev it would not.
				a.tspZoomPath = ""
				if rev == "" {
					a.tspZoomPath = file
				}
			}
		}
		return img
	}
	if kind == "svg" && f.src != "" {
		if !f.svgDone || f.svgDark != a.dark {
			f.svg, f.svgDark, f.svgDone = a.tspSVGFor(k, f.src), a.dark, true
		}
		if s := f.svg; s != nil && s.w > 0 && s.h > 0 {
			box := ui.Box(key).Width(mediaWidth(s.w, s.h, maxW, maxH)).MaxWidthPercent(pct).AlignSelf(ui.Start).Tooltip(file + at)
			box.Children(func() { s.draw(c) })
			// The viewer shows the disk's file, not a revision's.
			if !ownClick && rev == "" {
				box.Cursor(ui.CursorPointer)
				if box.Clicked() {
					a.update(func() { a.openPaneFile(p, file) })
				}
			}
			return box
		}
		f.err = "not an SVG GoRex can show"
	}
	detail := mediaLabel(file, kind) + " · " + sizeText(f.size) + at
	if f.err != "" {
		detail += " · " + f.err
	}
	return a.mediaCard(c, key, k, p, file, kind, detail, f.err != "", rev == "" && mediaOpenable(file))
}

// mediaCard draws a pane's file as a card: its kind's glyph (a warning
// for kind "missing"), its name and a line of detail, with Open when
// open; warn marks the detail a problem.
func (a *App) mediaCard(c *ui.Context, key *ui.Context, k *colors, p *Pane, file, kind, detail string, warn, open bool) ui.Element {
	card := ui.Row(key).Gap(10).Padding(8, 12).Radius(8).Background(k.card).Border(1, k.cardBorder).
		AlignItems(ui.Center).AlignSelf(ui.Start).MaxWidthPercent(100)
	if file != "" {
		card.Tooltip(file)
	}
	card.Children(func() {
		color := k.textMuted
		if kind == "missing" {
			color = k.attention
		}
		ui.Text(c, mediaGlyph(kind)).Font(symbolsFont).FontSize(20).TextColor(color).Shrink(0)
		info := ui.Column(c).Gap(2).Shrink(1).MinWidth(0)
		info.Children(func() {
			if file != "" {
				ui.Text(c, baseName(file)).FontSize(13).Bold().TextColor(k.text).NoWrap()
			}
			d := ui.Text(c, detail).FontSize(12).TextColor(k.textMuted)
			if warn {
				d.TextColor(k.attention)
			}
		})
		if open {
			b := textButton(c, k, "Open").Border(1, k.cardBorder)
			if b.Clicked() {
				a.update(func() { a.openPaneFile(p, file) })
			}
		}
	})
	return card
}

// mediaEntry returns the entry of a file, at rev when that is set,
// loading it the first time; once its load is mediaRecheck old, a disk
// file is checked for changes and a revision that moves (HEAD, a
// branch, the index) read again. A full sha never changes. It is nil
// when the cache is full of files drawn in this frame (mediaAdmit).
func (a *App) mediaEntry(file, rev, kind string) *mediaFile {
	if a.client == nil {
		return &mediaFile{loaded: true, mediaData: mediaData{err: "Not connected to the server."}}
	}
	key := file + "\x00" + rev
	f := mediaFiles[key]
	if f == nil || f.client != a.client {
		delete(mediaFiles, key)
		if !mediaAdmit(mediaFiles, a.frame) {
			return nil
		}
		f = &mediaFile{client: a.client}
		mediaFiles[key] = f
	}
	f.used = a.frame
	switch {
	case f.busy:
	case !f.loaded:
		a.mediaLoad(f, file, rev, kind)
	case time.Since(f.checked) <= mediaRecheck:
	case rev == "":
		a.mediaCheck(f, file, kind)
	case mediaMoves(rev):
		a.mediaLoad(f, file, rev, kind)
	}
	return f
}

// mediaAdmit makes room for one more entry, evicting those least
// recently drawn while there are mediaKeep or more, or their pixels are
// over mediaBudget. Entries drawn in frame stay, so that a frame never
// evicts what it draws and loads it again the next; when only those are
// left and there is no room, it reports false.
func mediaAdmit(files map[string]*mediaFile, frame uint64) bool {
	var pixels int64
	for _, e := range files {
		pixels += e.pixels
	}
	for len(files) >= mediaKeep || pixels >= mediaBudget {
		var oldest string
		var at *mediaFile
		for k, e := range files {
			if e.used != frame && (at == nil || e.used < at.used) {
				oldest, at = k, e
			}
		}
		if at == nil {
			return false
		}
		pixels -= at.pixels
		delete(files, oldest)
	}
	return true
}

// mediaLoad reads a file from the host in the background, from the disk
// or at rev, decoding a picture there. Read again the same as it was,
// or answered unchanged for the blob it has, the entry keeps what it has
// and the window is not redrawn.
func (a *App) mediaLoad(f *mediaFile, file, rev, kind string) {
	f.busy = true
	client, had, loaded := f.client, f.mediaData, f.loaded
	go func() {
		var d mediaData
		var data []byte
		var unchanged bool
		if rev == "" {
			d, data = mediaReadDisk(client, file, kind)
		} else {
			blob := ""
			if loaded && had.read {
				blob = had.blob
			}
			d, data, unchanged = mediaReadRev(client, file, rev, kind, blob)
		}
		var same bool
		switch {
		case unchanged:
			same, d.size, d.modTime = true, had.size, had.modTime
		case data != nil:
			d.read, d.sum = true, crc32.ChecksumIEEE(data)
			same = loaded && had.read && had.sum == d.sum
		default:
			same = loaded && !had.read && had.found == d.found && had.size == d.size && had.err == d.err
		}
		if data != nil && !same {
			var err error
			if kind == "svg" {
				d.src = string(data)
			} else if d.bitmap, err = mediaDecode(data); err != nil {
				d.err = err.Error()
			} else {
				w, h := d.bitmap.Size()
				d.pixels = int64(w) * int64(h)
			}
		}
		a.update(func() {
			f.busy, f.loaded, f.checked = false, true, time.Now()
			if same {
				f.size, f.modTime = d.size, d.modTime
				return
			}
			f.mediaData = d
			f.svg, f.svgDone = nil, false
			a.changed()
		})
	}()
}

// mediaReadDisk reads what stat says of a file, and a picture's or an
// SVG's bytes under their cap.
func mediaReadDisk(client *rex.Client, file, kind string) (d mediaData, data []byte) {
	fi, err := client.Stat(file)
	switch {
	case err != nil:
		d.err = err.Error()
		return d, nil
	case fi.Dir:
		d.err = "A folder, not a file"
		return d, nil
	}
	d.found, d.size, d.modTime = true, fi.Size, fi.ModTime
	limit := mediaLimit(kind)
	if limit == 0 {
		return d, nil
	}
	if fi.Size > limit {
		d.err = fmt.Sprintf("over %s, too large to show here", sizeText(limit))
		return d, nil
	}
	fd, err := client.ReadFile(file, limit)
	if err != nil {
		d.err = err.Error()
		return d, nil
	}
	d.size, d.modTime = int64(len(fd.Data)), fd.ModTime
	return d, fd.Data
}

// mediaReadRev reads a file at a git revision: a picture's or an SVG's
// bytes under their cap, else its size alone, which a read capped at
// one byte tells as too large without sending the bytes; unchanged, and
// nothing read, when the revision's blob is blob, the one already read.
func mediaReadRev(client *rex.Client, file, rev, kind, blob string) (d mediaData, data []byte, unchanged bool) {
	limit := mediaLimit(kind)
	probe := limit == 0
	if probe {
		limit = 1
	}
	fd, err := client.ReadGitFile(file, rev, blob, limit)
	switch {
	case err != nil && strings.Contains(err.Error(), rex.ErrTooLarge):
		d.found, d.size = true, sizeIn(err.Error())
		if !probe {
			d.err = fmt.Sprintf("over %s, too large to show here", sizeText(limit))
		}
	case err != nil:
		d.err = err.Error()
	case fd.Unchanged:
		return d, nil, true
	default:
		d.found, d.size, d.blob = true, int64(len(fd.Data)), fd.Blob
		if !probe {
			data = fd.Data
		}
	}
	return d, data, false
}

// mediaLimit caps the bytes read of a kind of file, 0 for one whose
// bytes are not read.
func mediaLimit(kind string) int64 {
	switch kind {
	case "picture":
		return mediaPictureMax
	case "svg":
		return mediaSVGMax
	}
	return 0
}

// mediaMoves reports whether a revision can name other contents later:
// anything but a full sha (40 or 64 hex digits).
func mediaMoves(rev string) bool {
	if len(rev) != 40 && len(rev) != 64 {
		return true
	}
	return strings.ContainsFunc(rev, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F')
	})
}

// mediaCheck asks the host whether a loaded disk file changed, appeared
// or went, and loads it again when it did.
func (a *App) mediaCheck(f *mediaFile, file, kind string) {
	f.busy = true
	client := f.client
	go func() {
		fi, err := client.Stat(file)
		a.update(func() {
			f.busy, f.checked = false, time.Now()
			found := err == nil && !fi.Dir
			if found != f.found || found && (fi.Size != f.size || !fi.ModTime.Equal(f.modTime)) {
				a.mediaLoad(f, file, "", kind)
			}
		})
	}()
}

// mediaDecode decodes a picture, refusing one over mediaSide pixels a
// side before decoding it.
func mediaDecode(data []byte) (*ui.Bitmap, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("not a picture GoRex can show")
	}
	if cfg.Width > mediaSide || cfg.Height > mediaSide {
		return nil, fmt.Errorf("%d×%d, over %d pixels a side", cfg.Width, cfg.Height, mediaSide)
	}
	bm, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil, errors.New("not a picture GoRex can show")
	}
	return bm, nil
}

// openPaneFile shows a file of a pane's host, absolute or under the
// pane's working directory, in an editor tab: pictures in GoRex's viewer
// page and PDF, HTML, video and audio in the engine's (View), text and
// code in the editor; for another file it says GoRex can't show it. The
// tab is the first editor tab, else a new one in the file's folder.
func (a *App) openPaneFile(p *Pane, path string) {
	file := mediaPath(p, path)
	if !mediaOpenable(file) {
		a.toast(ui.Toast{Title: "Can't show " + baseName(file), Description: "Not a file GoRex's viewer or editor shows.", Timeout: 4 * time.Second})
		return
	}
	for i, t := range a.tabs {
		if t.Kind == paneEditor {
			a.selectTab(i)
			a.openFile(t, file, false)
			return
		}
	}
	if t := a.newEditorTab(dirName(file)); t != nil {
		a.openFile(t, file, false)
	}
}

// mediaOpenable reports whether GoRex's editor tab shows a file: in
// View (pictures, PDF, video, audio, HTML) or as text.
func mediaOpenable(file string) bool {
	return viewable(file) != "" || textLike(file)
}

// mediaPath is where a media node's file is on the pane's host: an
// absolute path as it is, ~ the host's home, else under the pane's
// working directory.
func mediaPath(p *Pane, name string) string {
	name = expandHome(name)
	if name == "" || mediaAbs(name) {
		return name
	}
	dir := p.info.Dir
	if dir == "" {
		dir = p.startDir
	}
	if dir == "" {
		return name
	}
	return joinPath(dir, name)
}

// mediaAbs reports whether a path of the host's is absolute.
func mediaAbs(name string) bool {
	if strings.HasPrefix(name, "/") {
		return true
	}
	if hostGOOS != "windows" {
		return false
	}
	return strings.HasPrefix(name, `\`) || len(name) >= 3 && name[1] == ':' && (name[2] == '\\' || name[2] == '/')
}

// mediaKind is how a file draws, by its extension: "picture", "svg",
// "pdf", "video", "audio", "model" (3D), or "" for another file.
func mediaKind(file string) string {
	name := strings.ToLower(baseName(file))
	if strings.LastIndex(name, ".") <= 0 {
		return "" // no extension, as of .hidden
	}
	ext := strings.TrimPrefix(path.Ext(name), ".")
	if ext == "gz" {
		// 3D models open gzip-compressed too, those not compressed already.
		switch strings.TrimPrefix(path.Ext(strings.TrimSuffix(name, ".gz")), ".") {
		case "obj", "ply", "wrl", "vrml", "x3dv", "stl", "gltf", "glb", "fbx", "usd", "usda", "usdc", "3ds":
			return "model"
		}
		return ""
	}
	switch ext {
	case "png", "apng", "jpg", "jpeg", "jpe", "jfif", "gif", "webp", "bmp":
		return "picture"
	case "svg":
		return "svg"
	case "pdf":
		return "pdf"
	case "mp4", "m4v", "mov", "webm", "mkv", "avi":
		return "video"
	case "mp3", "m4a", "wav", "aac", "flac", "ogg", "oga", "opus":
		return "audio"
	case "obj", "ply", "wrl", "vrml", "wrz", "x3dv", "x3dvz", "stl", "gltf", "glb", "fbx", "usd", "usda", "usdc", "usdz", "3ds":
		return "model"
	}
	return ""
}

// mediaLabel names a file's kind for its card: "PDF", "Video · MP4", …
func mediaLabel(file, kind string) string {
	ext := strings.ToUpper(strings.TrimPrefix(path.Ext(baseName(file)), "."))
	label := map[string]string{"picture": "Picture", "svg": "SVG", "pdf": "PDF", "video": "Video", "audio": "Audio", "model": "3D model"}[kind]
	switch {
	case label == "":
		if ext == "" {
			return "File"
		}
		return ext
	case ext == "" || ext == label:
		return label
	}
	return label + " · " + ext
}

// mediaGlyph is a kind's icon, of the Nerd Fonts.
func mediaGlyph(kind string) string {
	switch kind {
	case "picture", "svg":
		return "\uf1c5"
	case "pdf":
		return "\uf1c1"
	case "video":
		return "\uf1c8"
	case "audio":
		return "\uf1c7"
	case "model":
		return "\uf1b2"
	case "missing":
		return "\uf071"
	}
	return "\uf15b"
}

// mediaMissing reports whether an error of stat, or of git reading a
// revision, says there is no such file.
func mediaMissing(err string) bool {
	for _, s := range []string{"no such file", "cannot find", "does not exist", "but not in"} {
		if strings.Contains(err, s) {
			return true
		}
	}
	return false
}

// mediaCell is the size of the pane's terminal cells, which "ch" and
// "lines" count, in DIPs; a usual one while no view shows the terminal.
func mediaCell(p *Pane) (w, h float32) {
	if p.term != nil {
		if r, ok := p.term.CellRect(0, 0); ok && r.W > 0 && r.H > 0 {
			return r.W, r.H
		}
	}
	return 8, 17
}

// mediaExtent reads a bound of the protocol's: "<n>ch" in cells of
// width cw, "<n>lines" in lines of height lh, as px; a number as a
// fraction of the parent. Zero for no bound.
func mediaExtent(v any, cw, lh float32) (px, frac float32) {
	switch v := v.(type) {
	case float64:
		if v > 0 {
			frac = float32(min(v, 1))
		}
	case string:
		unit := cw
		num, ok := strings.CutSuffix(v, "ch")
		if !ok {
			num, ok = strings.CutSuffix(v, "lines")
			unit = lh
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(num), 32); ok && err == nil && f > 0 {
			px = float32(f) * unit
		}
	}
	return px, frac
}

// mediaWidth is how wide a picture of w×h draws: its own width, cut to
// maxW and to the width at which it is maxH high, where they are set.
func mediaWidth(w, h, maxW, maxH float32) float32 {
	if maxW > 0 && w > maxW {
		w, h = maxW, h*maxW/w
	}
	if maxH > 0 && h > maxH {
		w = w * maxH / h
	}
	return w
}
