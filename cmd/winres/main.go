// Command winres turns assets/icon.ico into rsrc_windows_amd64.syso, the
// Windows resource that gives AudioTracker.exe, its window and its taskbar
// button their icon. go build links any .syso in the package automatically.
// Run it from the repository root after changing the icon:
//
//	go run ./cmd/winres
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

const (
	icoPath  = "assets/icon.ico"
	sysoPath = "rsrc_windows_amd64.syso"

	rtIcon      = 3
	rtGroupIcon = 14
	// iconGroupID is the resource ID Gio loads as the window icon.
	iconGroupID = 1
	langNeutral = 0
)

var le = binary.LittleEndian

// icoImage is one image of an .ico file, with its directory entry.
type icoImage struct {
	entry [12]byte // width, height, colors, reserved, planes, bit count, size
	data  []byte
}

func main() {
	b, err := os.ReadFile(icoPath)
	if err == nil {
		var imgs []icoImage
		if imgs, err = parseICO(b); err == nil {
			err = os.WriteFile(sysoPath, buildSyso(imgs), 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
	fmt.Println("escrito", sysoPath)
}

func parseICO(b []byte) ([]icoImage, error) {
	if len(b) < 6 || le.Uint16(b[2:]) != 1 {
		return nil, errors.New(icoPath + " no es un .ico")
	}
	n := int(le.Uint16(b[4:]))
	var imgs []icoImage
	for i := range n {
		e := b[6+16*i : 6+16*i+16]
		size, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		if int(off)+int(size) > len(b) {
			return nil, fmt.Errorf("imagen %d fuera del archivo", i)
		}
		var img icoImage
		copy(img.entry[:], e[:12])
		img.data = b[off : off+size]
		imgs = append(imgs, img)
	}
	return imgs, nil
}

// buildSyso writes a COFF object with a single .rsrc section holding one
// RT_ICON per image and the RT_GROUP_ICON that lists them.
func buildSyso(imgs []icoImage) []byte {
	// Group directory: like the .ico header, with resource IDs for offsets.
	var group bytes.Buffer
	binary.Write(&group, le, [3]uint16{0, 1, uint16(len(imgs))})
	for i, img := range imgs {
		group.Write(img.entry[:])
		binary.Write(&group, le, uint16(i+1))
	}

	// Resource tree: type -> ID -> language -> data.
	type leaf struct {
		id   int
		data []byte
	}
	types := []struct {
		id     int
		leaves []leaf
	}{{rtIcon, nil}, {rtGroupIcon, []leaf{{iconGroupID, group.Bytes()}}}}
	for i, img := range imgs {
		types[0].leaves = append(types[0].leaves, leaf{i + 1, img.data})
	}

	// Layout: all directories first, then data entries, then the data.
	dirSize := func(entries int) int { return 16 + 8*entries }
	off := dirSize(len(types))
	typeDir := make([]int, len(types))
	idDir := make([][]int, len(types))
	for t := range types {
		typeDir[t] = off
		off += dirSize(len(types[t].leaves))
	}
	for t := range types {
		for range types[t].leaves {
			idDir[t] = append(idDir[t], off)
			off += dirSize(1)
		}
	}
	dataEntry := make([][]int, len(types))
	for t := range types {
		for range types[t].leaves {
			dataEntry[t] = append(dataEntry[t], off)
			off += 16
		}
	}
	dataOff := make([][]int, len(types))
	for t := range types {
		for _, l := range types[t].leaves {
			off = (off + 7) &^ 7
			dataOff[t] = append(dataOff[t], off)
			off += len(l.data)
		}
	}
	sec := make([]byte, (off+7)&^7)

	dir := func(at, entries int) int {
		le.PutUint16(sec[at+14:], uint16(entries)) // all entries are IDs
		return at + 16
	}
	entry := func(at, id, target int, subdir bool) {
		le.PutUint32(sec[at:], uint32(id))
		if subdir {
			target |= 0x80000000
		}
		le.PutUint32(sec[at+4:], uint32(target))
	}
	var relocs []int // section offsets holding an RVA
	e := dir(0, len(types))
	for t, typ := range types {
		entry(e+8*t, typ.id, typeDir[t], true)
		e2 := dir(typeDir[t], len(typ.leaves))
		for i, l := range typ.leaves {
			entry(e2+8*i, l.id, idDir[t][i], true)
			e3 := dir(idDir[t][i], 1)
			entry(e3, langNeutral, dataEntry[t][i], false)
			d := dataEntry[t][i]
			le.PutUint32(sec[d:], uint32(dataOff[t][i])) // + section RVA, via reloc
			le.PutUint32(sec[d+4:], uint32(len(l.data)))
			relocs = append(relocs, d)
			copy(sec[dataOff[t][i]:], l.data)
		}
	}

	// COFF: file header, section header, raw data, relocations, symbols.
	const (
		fileHdr    = 20
		secHdr     = 40
		relocSize  = 10
		amd64      = 0x8664
		addr32NB   = 3 // IMAGE_REL_AMD64_ADDR32NB: RVA of the symbol + addend
		secFlags   = 0x40000040
		classStatc = 3
	)
	rawAt := fileHdr + secHdr
	relocAt := rawAt + len(sec)
	symAt := relocAt + relocSize*len(relocs)

	var out bytes.Buffer
	binary.Write(&out, le, struct {
		Machine, Sections        uint16
		Time, SymTab, Symbols    uint32
		OptHeader, Characterists uint16
	}{amd64, 1, 0, uint32(symAt), 1, 0, 0})
	var name [8]byte
	copy(name[:], ".rsrc")
	binary.Write(&out, le, struct {
		Name                        [8]byte
		VirtSize, VirtAddr, RawSize uint32
		RawAt, RelocAt, LinesAt     uint32
		Relocs, Lines               uint16
		Characteristics             uint32
	}{name, 0, 0, uint32(len(sec)), uint32(rawAt), uint32(relocAt), 0, uint16(len(relocs)), 0, secFlags})
	out.Write(sec)
	for _, r := range relocs {
		binary.Write(&out, le, struct {
			Addr, Sym uint32
			Type      uint16
		}{uint32(r), 0, addr32NB})
	}
	binary.Write(&out, le, struct {
		Name    [8]byte
		Value   uint32
		Section int16
		Type    uint16
		Class   uint8
		Aux     uint8
	}{name, 0, 1, 0, classStatc, 0})
	binary.Write(&out, le, uint32(4)) // empty string table
	return out.Bytes()
}
