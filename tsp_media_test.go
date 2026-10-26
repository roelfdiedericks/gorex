package main

import (
	"strconv"
	"testing"

	"gorex/internal/rex"
)

// A media node's path is the host's: absolute as it is, by the host's
// system, else under the pane's working directory, or the directory it
// started in before the server said one.
func TestMediaPath(t *testing.T) {
	defer func(goos string) { hostGOOS = goos }(hostGOOS)
	for _, c := range []struct {
		goos, dir, start, name, want string
	}{
		{"linux", "/home/u/proj", "", "/tmp/a.png", "/tmp/a.png"},
		{"linux", "/home/u/proj", "", "docs/b.svg", "/home/u/proj/docs/b.svg"},
		{"linux", "/home/u/proj/", "", "b.svg", "/home/u/proj/b.svg"},
		{"linux", "", "/srv", "b.svg", "/srv/b.svg"},
		{"linux", "", "", "b.svg", "b.svg"},
		{"linux", "/home/u", "", `C:\x.png`, `/home/u/C:\x.png`},
		{"windows", `C:\proj`, "", `D:\x.png`, `D:\x.png`},
		{"windows", `C:\proj`, "", `D:/x.png`, `D:/x.png`},
		{"windows", `C:\proj`, "", `\\server\share\x.png`, `\\server\share\x.png`},
		{"windows", `C:\proj`, "", `img\x.png`, `C:\proj\img\x.png`},
	} {
		hostGOOS = c.goos
		p := &Pane{info: rex.SessionInfo{Dir: c.dir}, startDir: c.start}
		if got := mediaPath(p, c.name); got != c.want {
			t.Errorf("%s %q in %q: %q, want %q", c.goos, c.name, c.dir, got, c.want)
		}
	}
}

// A file draws by its extension, in any case; 3D models gzip-compressed
// too, but not those that are compressed already.
func TestMediaKind(t *testing.T) {
	for name, want := range map[string]string{
		"/a/Shot.PNG":      "picture",
		"x.jfif":           "picture",
		"chart.svg":        "svg",
		`C:\docs\spec.pdf`: "pdf",
		"clip.webm":        "video",
		"note.opus":        "audio",
		"mesh.glb":         "model",
		"mesh.stl.gz":      "model",
		"mesh.usdz.gz":     "",
		"backup.tar.gz":    "",
		"README":           "",
		".png":             "",
		"notes.txt":        "",
	} {
		if got := mediaKind(name); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

// Bounds are cells, lines or a fraction of the parent; anything else is
// no bound.
func TestMediaExtent(t *testing.T) {
	for _, c := range []struct {
		v        any
		px, frac float32
	}{
		{"40ch", 320, 0},
		{"10lines", 170, 0},
		{" 2.5lines", 42.5, 0},
		{0.5, 0, 0.5},
		{3.0, 0, 1},
		{-1.0, 0, 0},
		{"-4ch", 0, 0},
		{"12px", 0, 0},
		{"lines", 0, 0},
		{nil, 0, 0},
	} {
		if px, frac := mediaExtent(c.v, 8, 17); px != c.px || frac != c.frac {
			t.Errorf("%v: %v, %v, want %v, %v", c.v, px, frac, c.px, c.frac)
		}
	}
}

// A picture keeps its ratio as either bound cuts it, and never grows.
func TestMediaWidth(t *testing.T) {
	for _, c := range []struct{ w, h, maxW, maxH, want float32 }{
		{400, 200, 0, 0, 400},
		{400, 200, 100, 0, 100},
		{400, 200, 0, 50, 100},
		{400, 200, 300, 50, 100},
		{400, 200, 50, 100, 50},
		{40, 20, 300, 300, 40},
	} {
		if got := mediaWidth(c.w, c.h, c.maxW, c.maxH); got != c.want {
			t.Errorf("%v×%v in %v×%v: %v, want %v", c.w, c.h, c.maxW, c.maxH, got, c.want)
		}
	}
}

// Only a full sha names contents that never change: HEAD, branches,
// short shas and the index are read again.
func TestMediaMoves(t *testing.T) {
	for rev, want := range map[string]bool{
		"HEAD": true, ":0": true, "main": true, "0123abc": true,
		"0123456789abcdef0123456789abcdef01234567":                         false,
		"0123456789ABCDEF0123456789abcdef01234567":                         false,
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": false,
		"0123456789abcdef0123456789abcdef0123456g":                         true,
	} {
		if got := mediaMoves(rev); got != want {
			t.Errorf("%q: %v, want %v", rev, got, want)
		}
	}
}

// More files on screen than the cache keeps never evict one another:
// those drawn this frame stay and the rest are refused (drawn as cards),
// so a frame never reloads what the last evicted. Older entries go least
// recently drawn first, by count and by decoded pixels.
func TestMediaAdmit(t *testing.T) {
	files := map[string]*mediaFile{}
	const frame = 10
	for i := range mediaKeep {
		if !mediaAdmit(files, frame) {
			t.Fatalf("entry %d refused with room", i)
		}
		files[strconv.Itoa(i)] = &mediaFile{used: frame}
	}
	for range 1000 {
		if mediaAdmit(files, frame) {
			t.Fatal("admitted past mediaKeep with every entry on screen")
		}
	}
	if len(files) != mediaKeep {
		t.Fatalf("evicted an entry drawn this frame: %d left", len(files))
	}
	// The next frame, entries not drawn go, oldest first.
	files["0"].used, files["1"].used = frame-2, frame-1
	if !mediaAdmit(files, frame+1) {
		t.Fatal("refused with stale entries to evict")
	}
	if files["0"] != nil || files["1"] == nil {
		t.Fatal("did not evict the least recently drawn")
	}

	// Pixels: two huge pictures on screen fill the budget; a third is
	// refused, and evicted once off screen.
	files = map[string]*mediaFile{
		"a": {used: frame, mediaData: mediaData{pixels: mediaSide * mediaSide / 2}},
		"b": {used: frame, mediaData: mediaData{pixels: mediaSide * mediaSide / 2}},
	}
	if mediaAdmit(files, frame) {
		t.Fatal("admitted over the pixel budget with all on screen")
	}
	files["a"].used = frame - 1
	if !mediaAdmit(files, frame) || files["a"] != nil || len(files) != 1 {
		t.Fatal("did not evict the off-screen picture over budget")
	}
}
