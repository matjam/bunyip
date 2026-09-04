package asset

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/matjam/bunyip/gfx"
)

// Atlas resolves the image beside the atlas file and reports what is
// missing before it needs the GPU.
func TestAtlasResolvesImage(t *testing.T) {
	withImage := `{"frames":{"a":{"frame":{"x":0,"y":0,"w":4,"h":3}}},"meta":{"image":"hero.png"}}`
	noImage := `{"frames":{"a":{"frame":{"x":0,"y":0,"w":4,"h":3}}},"meta":{}}`
	fs, err := OpenFS(FSSource(fstest.MapFS{
		"sprites/hero.json":    {Data: []byte(withImage)},
		"sprites/noimage.json": {Data: []byte(noImage)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	// The image is looked up next to the atlas: sprites/hero.png, which
	// this filesystem lacks, so the error names it.
	if _, err := Atlas(nil, fs, "sprites/hero.json", gfx.TextureOptions{}); err == nil || !strings.Contains(err.Error(), "sprites/hero.png") {
		t.Errorf("missing image error %v", err)
	}
	if _, err := Atlas(nil, fs, "sprites/noimage.json", gfx.TextureOptions{}); err == nil || !strings.Contains(err.Error(), "names no image") {
		t.Errorf("atlas without an image name: %v", err)
	}
}

// Aseprite names the file in its errors and fails before the GPU when
// the bytes are not an Aseprite file.
func TestAsepriteNamesTheFile(t *testing.T) {
	fs, err := OpenFS(FSSource(fstest.MapFS{
		"sprites/hero.aseprite": {Data: []byte("not an aseprite file at all, but long enough to reach the magic")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	_, err = Aseprite(nil, fs, "sprites/hero.aseprite", gfx.AsepriteOptions{}, gfx.TextureOptions{})
	if err == nil || !strings.Contains(err.Error(), "sprites/hero.aseprite") {
		t.Errorf("error %v", err)
	}
	if _, err := Aseprite(nil, fs, "sprites/missing.aseprite", gfx.AsepriteOptions{}, gfx.TextureOptions{}); err == nil {
		t.Error("a missing file loaded")
	}
}
