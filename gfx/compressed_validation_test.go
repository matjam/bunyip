package gfx

import (
	"encoding/binary"
	"testing"

	"github.com/matjam/bunyip/gfx/ktx2"
)

func TestCompressedExcessMipsRejectedBeforeDeviceAccess(t *testing.T) {
	f := &ktx2.File{Format: ktx2.BC1RGBUnorm, Width: 2, Height: 2, Levels: [][]byte{make([]byte, 8), make([]byte, 8)}}
	data, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	// Both BC1 levels still occupy one block, but 1x1 permits only one.
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[24:], 1)
	for _, noMips := range []bool{false, true} {
		t.Run(map[bool]string{false: "all levels", true: "base level only"}[noMips], func(t *testing.T) {
			// No renderer exists: reaching the device would panic.
			g := &Graphics{}
			if tex, err := g.NewCompressedTexture(data, TextureOptions{NoMipmaps: noMips}); err == nil || tex != nil {
				t.Errorf("NewCompressedTexture = %v, %v, want nil and error", tex, err)
			}
		})
	}
}
