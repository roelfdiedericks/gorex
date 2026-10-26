// Devicon gives dev builds on Windows the app's icon: it writes the icon
// of resources/icon.png, at the sizes Windows shows, as a resource object
// (.syso) that go build links into the executable, as mygo build does
// for its builds. Windows shows it on the executable, and MyGo loads it
// for the windows, the taskbar and the tray (resource 1).
//
// Only the icon: mygo build's object also has a manifest and the version
// information that tells MyGo the app was built by mygo build, which a
// dev build is not. The object is named as mygo build names its own, so
// mygo build replaces it while it builds, and removes it after (make dev
// writes it again).
//
//	go run ./tools/devicon -out mygo_windows_amd64.syso -arch amd64
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"

	"golang.org/x/image/draw"
)

// sizes are the sizes of the icon's images, as mygo build makes them.
var sizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	icon := flag.String("icon", "resources/icon.png", "the icon, a square PNG")
	out := flag.String("out", "", "the object to write")
	arch := flag.String("arch", "amd64", "the GOARCH it is for")
	flag.Parse()
	src, err := os.ReadFile(*icon)
	if err != nil {
		log.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		log.Fatalf("%s: %v", *icon, err)
	}
	obj, err := syso(img, *arch)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, obj, 0o644); err != nil {
		log.Fatal(err)
	}
}

// images renders the icon at each of the sizes, as PNG.
func images(img image.Image) ([][]byte, error) {
	out := make([][]byte, len(sizes))
	for i, s := range sizes {
		dst := image.NewNRGBA(image.Rect(0, 0, s, s))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
		var b bytes.Buffer
		if err := png.Encode(&b, dst); err != nil {
			return nil, err
		}
		out[i] = b.Bytes()
	}
	return out, nil
}

// syso builds a COFF object with a resource section of the icon: an
// RT_ICON for each image, and the RT_GROUP_ICON 1 that lists them.
func syso(img image.Image, goarch string) ([]byte, error) {
	// The COFF machine and the relocation type of relative addresses.
	arch, ok := map[string]struct{ machine, relocation uint16 }{
		"amd64": {0x8664, 0x0003}, // IMAGE_REL_AMD64_ADDR32NB
		"arm64": {0xAA64, 0x0002}, // IMAGE_REL_ARM64_ADDR32NB
		"386":   {0x014C, 0x0007}, // IMAGE_REL_I386_DIR32NB
	}[goarch]
	if !ok {
		return nil, fmt.Errorf("no Windows resources for %s", goarch)
	}
	pngs, err := images(img)
	if err != nil {
		return nil, err
	}
	const rtIcon, rtGroupIcon = 3, 14
	le := binary.LittleEndian
	// The resources by type, each by id, in order.
	type resource struct {
		id   uint16
		data []byte
	}
	var icons []resource
	var group bytes.Buffer
	_ = binary.Write(&group, le, [3]uint16{0, 1, uint16(len(pngs))})
	for i, p := range pngs {
		id := uint16(i + 1)
		icons = append(icons, resource{id, p})
		dim := byte(sizes[i])
		if sizes[i] >= 256 {
			dim = 0
		}
		group.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&group, le, [2]uint16{1, 32}) // planes, bits
		_ = binary.Write(&group, le, uint32(len(p)))
		_ = binary.Write(&group, le, id)
	}
	types := []struct {
		typ uint16
		res []resource
	}{
		{rtIcon, icons},
		{rtGroupIcon, []resource{{1, group.Bytes()}}},
	}

	// The resource section: three levels of directories (type, id,
	// language, neutral), the data entries, then the data. Directory
	// entries point to subdirectories by section offset; data entries hold
	// relative virtual addresses, which the linker computes from
	// relocations.
	dirSize := func(n int) int { return 16 + 8*n }
	size := dirSize(len(types))
	count := 0
	for _, t := range types {
		size += dirSize(len(t.res)) + len(t.res)*dirSize(1)
		count += len(t.res)
	}
	entriesAt := size
	dataAt := entriesAt + 16*count
	raw := make([]byte, dataAt)
	dir := func(at, n int) { le.PutUint16(raw[at+14:], uint16(n)) } // NumberOfIdEntries
	var relocs []uint32
	var data bytes.Buffer
	dir(0, len(types))
	next := dirSize(len(types))
	entry := entriesAt
	for i, t := range types {
		le.PutUint32(raw[16+8*i:], uint32(t.typ))
		le.PutUint32(raw[16+8*i+4:], uint32(next)|0x80000000)
		idDir := next
		dir(idDir, len(t.res))
		next += dirSize(len(t.res))
		for j, r := range t.res {
			le.PutUint32(raw[idDir+16+8*j:], uint32(r.id))
			le.PutUint32(raw[idDir+16+8*j+4:], uint32(next)|0x80000000)
			dir(next, 1)
			le.PutUint32(raw[next+16:], 0) // LANG_NEUTRAL
			le.PutUint32(raw[next+20:], uint32(entry))
			next += dirSize(1)
			for data.Len()%8 != 0 {
				data.WriteByte(0)
			}
			le.PutUint32(raw[entry:], uint32(dataAt+data.Len())) // OffsetToData
			le.PutUint32(raw[entry+4:], uint32(len(r.data)))
			relocs = append(relocs, uint32(entry))
			entry += 16
			data.Write(r.data)
		}
	}
	raw = append(raw, data.Bytes()...)
	for len(raw)%4 != 0 {
		raw = append(raw, 0)
	}

	// The object: a header, the .rsrc section's header, its bytes, its
	// relocations, and the section's symbol with an empty string table.
	var out bytes.Buffer
	const headerSize, sectionHeaderSize = 20, 40
	rawAt := headerSize + sectionHeaderSize
	relocAt := rawAt + len(raw)
	symbolsAt := relocAt + 10*len(relocs)
	_ = binary.Write(&out, le, struct {
		Machine, Sections                   uint16
		Time, Symbols, SymbolCount          uint32
		OptionalHeaderSize, Characteristics uint16
	}{arch.machine, 1, 0, uint32(symbolsAt), 1, 0, 0})
	_ = binary.Write(&out, le, struct {
		Name                                        [8]byte
		VirtualSize, VirtualAddress, RawSize, RawAt uint32
		RelocAt, LineNumbersAt                      uint32
		Relocs, LineNumbers                         uint16
		Characteristics                             uint32
	}{[8]byte{'.', 'r', 's', 'r', 'c'}, 0, 0, uint32(len(raw)), uint32(rawAt), uint32(relocAt), 0, uint16(len(relocs)), 0, 0x40000040})
	out.Write(raw)
	for _, r := range relocs {
		_ = binary.Write(&out, le, r)         // VirtualAddress
		_ = binary.Write(&out, le, uint32(0)) // symbol 0: the section
		_ = binary.Write(&out, le, arch.relocation)
	}
	out.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0})
	_ = binary.Write(&out, le, uint32(0)) // Value
	_ = binary.Write(&out, le, uint16(1)) // SectionNumber
	_ = binary.Write(&out, le, uint16(0)) // Type
	out.Write([]byte{3, 0})               // IMAGE_SYM_CLASS_STATIC, no aux symbols
	_ = binary.Write(&out, le, uint32(4)) // the string table's size
	return out.Bytes(), nil
}
