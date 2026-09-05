package gfx

import (
	"encoding/binary"
	"image/color"
	"testing"
)

func TestParseAsepritePaletteChannels(t *testing.T) {
	tests := []struct {
		name string
		kind int
		in   color.RGBA
		want color.RGBA
	}{
		{"six-bit zero", aseChunkOldPalette2, color.RGBA{0, 0, 0, 255}, color.RGBA{0, 0, 0, 255}},
		{"six-bit one", aseChunkOldPalette2, color.RGBA{1, 1, 1, 255}, color.RGBA{4, 4, 4, 255}},
		{"six-bit midpoint", aseChunkOldPalette2, color.RGBA{32, 32, 32, 255}, color.RGBA{129, 129, 129, 255}},
		{"six-bit white", aseChunkOldPalette2, color.RGBA{63, 63, 63, 255}, color.RGBA{255, 255, 255, 255}},
		{"six-bit mixed", aseChunkOldPalette2, color.RGBA{1, 32, 63, 255}, color.RGBA{4, 129, 255, 255}},
		{"legacy eight-bit", aseChunkOldPalette, color.RGBA{63, 128, 255, 255}, color.RGBA{63, 128, 255, 255}},
		{"modern eight-bit", aseChunkPalette, color.RGBA{63, 128, 255, 255}, color.RGBA{63, 128, 255, 255}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var palette []byte
			if tt.kind == aseChunkPalette {
				palette = paletteChunk([]color.RGBA{{}, tt.in})
			} else {
				var p aseWriter
				p.u16(1) // one packet, starting after the transparent index
				p.u8(1)
				p.u8(1)
				p.u8(tt.in.R)
				p.u8(tt.in.G)
				p.u8(tt.in.B)
				palette = chunk(tt.kind, p.buf.Bytes())
			}
			var w aseWriter
			w.header(1, 1, 1, 8)
			w.frame(100, [][]byte{
				palette,
				layerChunk("indexed", 1, 0, 0, 255),
				celChunk(0, 0, 0, 255, 0, 1, 1, []byte{1}),
			})
			data := w.buf.Bytes()
			binary.LittleEndian.PutUint32(data, uint32(len(data)))
			a, err := ParseAseprite(data, AsepriteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(a.Palette) != 2 {
				t.Fatalf("palette has %d entries, want 2", len(a.Palette))
			}
			if got := a.Palette[1]; got != tt.want {
				t.Errorf("palette entry = %v, want %v", got, tt.want)
			}
			if got := a.Image.RGBAAt(0, 0); got != tt.want {
				t.Errorf("indexed pixel = %v, want %v", got, tt.want)
			}
		})
	}
}
