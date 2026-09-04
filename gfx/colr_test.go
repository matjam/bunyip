package gfx

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"slices"
	"testing"

	ot "github.com/go-text/typesetting/font/opentype"
)

// The COLR tests build their own colour font: no free font small enough
// to embed carries COLR tables, so the tables are written by hand onto a
// copy of Go Regular, whose outlines the layers then use.

// withTables returns a font with tables added to it, replacing any table
// of the same tag.
func withTables(t *testing.T, ttf []byte, add ...ot.Table) []byte {
	t.Helper()
	ld, err := ot.NewLoader(bytes.NewReader(ttf))
	if err != nil {
		t.Fatalf("load the test font: %v", err)
	}
	var tables []ot.Table
	for _, tag := range ld.Tables() {
		if slices.ContainsFunc(add, func(a ot.Table) bool { return a.Tag == tag }) {
			continue
		}
		raw, err := ld.RawTable(tag)
		if err != nil {
			t.Fatalf("read table %s: %v", tag, err)
		}
		tables = append(tables, ot.Table{Tag: tag, Content: raw})
	}
	tables = append(tables, add...)
	slices.SortFunc(tables, func(a, b ot.Table) int { return int(a.Tag) - int(b.Tag) })
	return ot.WriteTTF(tables)
}

// cpalTable builds a CPAL version 0 table holding one palette.
func cpalTable(palette []color.RGBA) ot.Table {
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 0)                    // version
	b = binary.BigEndian.AppendUint16(b, uint16(len(palette))) // entries per palette
	b = binary.BigEndian.AppendUint16(b, 1)                    // palettes
	b = binary.BigEndian.AppendUint16(b, uint16(len(palette))) // colour records
	b = binary.BigEndian.AppendUint32(b, uint32(12+2))         // offset to the records
	b = binary.BigEndian.AppendUint16(b, 0)                    // the palette's first record
	for _, c := range palette {
		b = append(b, c.B, c.G, c.R, c.A)
	}
	return ot.Table{Tag: ot.MustNewTag("CPAL"), Content: b}
}

// colrLayer is one layer of a version 0 colour glyph.
type colrLayer struct {
	gid     uint16
	palette uint16
}

// colrV0Table builds a COLR version 0 table for one base glyph.
func colrV0Table(base uint16, layers []colrLayer) ot.Table {
	const header = 14
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 0)        // version
	b = binary.BigEndian.AppendUint16(b, 1)        // base glyph records
	b = binary.BigEndian.AppendUint32(b, header)   // offset to them
	b = binary.BigEndian.AppendUint32(b, header+6) // offset to the layer records
	b = binary.BigEndian.AppendUint16(b, uint16(len(layers)))
	b = binary.BigEndian.AppendUint16(b, base) // the base glyph
	b = binary.BigEndian.AppendUint16(b, 0)    // its first layer
	b = binary.BigEndian.AppendUint16(b, uint16(len(layers)))
	for _, l := range layers {
		b = binary.BigEndian.AppendUint16(b, l.gid)
		b = binary.BigEndian.AppendUint16(b, l.palette)
	}
	return ot.Table{Tag: ot.MustNewTag("COLR"), Content: b}
}

// colrV1Layer is one layer of a version 1 colour glyph: an outline
// filled either with a palette colour or with a linear gradient between
// two of them.
type colrV1Layer struct {
	gid      uint16
	palette  uint16
	gradient bool
	from, to uint16 // palette entries of a gradient
	x0, y0   int16  // the gradient's start, in font units
	x1, y1   int16  // its end
	x2, y2   int16  // its rotation point
}

// paint returns the layer's paint table, with the offset to its child
// counted from the start of the PaintGlyph table.
func (l colrV1Layer) paint() []byte {
	var child []byte
	if l.gradient {
		child = []byte{4, 0, 0, 16} // format 4, Offset24 of 16 to the colour line
		for _, v := range []int16{l.x0, l.y0, l.x1, l.y1, l.x2, l.y2} {
			child = binary.BigEndian.AppendUint16(child, uint16(v))
		}
		child = append(child, 0)                        // extend: pad
		child = binary.BigEndian.AppendUint16(child, 2) // two stops
		child = binary.BigEndian.AppendUint16(child, 0) // at offset 0
		child = binary.BigEndian.AppendUint16(child, l.from)
		child = binary.BigEndian.AppendUint16(child, 1<<14) // alpha 1
		child = binary.BigEndian.AppendUint16(child, 1<<14) // at offset 1
		child = binary.BigEndian.AppendUint16(child, l.to)
		child = binary.BigEndian.AppendUint16(child, 1<<14) // alpha 1
	} else {
		child = append(child, 2) // format 2, a solid fill
		child = binary.BigEndian.AppendUint16(child, l.palette)
		child = binary.BigEndian.AppendUint16(child, 1<<14) // alpha 1
	}
	out := []byte{10, 0, 0, 6} // format 10, Offset24 of 6 to the child
	out = binary.BigEndian.AppendUint16(out, l.gid)
	return append(out, child...)
}

// colrV1Table builds a COLR version 1 table whose one base glyph paints
// a list of layers from the layer list.
func colrV1Table(base uint16, layers []colrV1Layer) ot.Table {
	const header = 34
	// The base glyph list holds one record and the paint it points at.
	baseList := binary.BigEndian.AppendUint32(nil, 1)
	baseList = binary.BigEndian.AppendUint16(baseList, base)
	baseList = binary.BigEndian.AppendUint32(baseList, 10) // the paint follows the record
	baseList = append(baseList, 1, byte(len(layers)))      // PaintColrLayers
	baseList = binary.BigEndian.AppendUint32(baseList, 0)  // from the first layer
	// The layer list holds an offset to each layer's paint.
	paints := make([][]byte, len(layers))
	offset := 4 + 4*len(layers)
	layerList := binary.BigEndian.AppendUint32(nil, uint32(len(layers)))
	for i, l := range layers {
		paints[i] = l.paint()
		layerList = binary.BigEndian.AppendUint32(layerList, uint32(offset))
		offset += len(paints[i])
	}
	for _, p := range paints {
		layerList = append(layerList, p...)
	}
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 1) // version
	b = binary.BigEndian.AppendUint16(b, 0) // no version 0 base glyph records
	b = binary.BigEndian.AppendUint32(b, 0)
	b = binary.BigEndian.AppendUint32(b, 0)
	b = binary.BigEndian.AppendUint16(b, 0)
	b = binary.BigEndian.AppendUint32(b, header)                       // base glyph list
	b = binary.BigEndian.AppendUint32(b, uint32(header+len(baseList))) // layer list
	b = binary.BigEndian.AppendUint32(b, 0)                            // no clip list
	b = binary.BigEndian.AppendUint32(b, 0)                            // no variation index map
	b = binary.BigEndian.AppendUint32(b, 0)                            // no variation store
	b = append(b, baseList...)
	b = append(b, layerList...)
	return ot.Table{Tag: ot.MustNewTag("COLR"), Content: b}
}
