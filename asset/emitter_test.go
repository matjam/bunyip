package asset

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/particle"
)

// TestEmitterWithoutTexture checks the part of the loader that needs no
// GPU: the file is parsed, and an emitter naming no texture comes back
// drawing plain quads.
func TestEmitterWithoutTexture(t *testing.T) {
	e := particle.Sparks()
	data, err := particle.Save(e)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := OpenFS(FSSource(fstest.MapFS{
		"effects/sparks.json": {Data: data},
		"effects/bad.json":    {Data: []byte("{")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	got, err := Emitter(nil, fs, "effects/sparks.json", gfx.TextureOptions{})
	if err != nil {
		t.Fatalf("Emitter: %v", err)
	}
	if got.Texture != nil {
		t.Error("an emitter naming no texture came back with one")
	}
	if got.Burst != e.Burst || got.Blend != e.Blend {
		t.Errorf("loaded %+v, want %+v", got, e)
	}

	if _, err := Emitter(nil, fs, "effects/bad.json", gfx.TextureOptions{}); err == nil ||
		!strings.Contains(err.Error(), "asset effects/bad.json:") {
		t.Fatalf("bad emitter error %v", err)
	}
	if _, err := Emitter(nil, fs, "effects/missing.json", gfx.TextureOptions{}); err == nil {
		t.Error("a missing emitter loaded without an error")
	}
}
