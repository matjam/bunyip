package gfx

import (
	"slices"
	"testing"

	"github.com/matjam/bunyip/lin"
)

const texturePackerHash = `{"frames": {
  "hero/idle.png": {"frame": {"x": 0, "y": 0, "w": 32, "h": 32}, "rotated": false, "trimmed": false,
    "spriteSourceSize": {"x": 0, "y": 0, "w": 32, "h": 32}, "sourceSize": {"w": 32, "h": 32}},
  "hero/run.png": {"frame": {"x": 32, "y": 0, "w": 24, "h": 30}, "rotated": false, "trimmed": true,
    "spriteSourceSize": {"x": 4, "y": 2, "w": 24, "h": 30}, "sourceSize": {"w": 32, "h": 32}},
  "coin.png": {"frame": {"x": 64, "y": 0, "w": 16, "h": 8}, "rotated": true, "trimmed": false,
    "spriteSourceSize": {"x": 0, "y": 0, "w": 16, "h": 8}, "sourceSize": {"w": 16, "h": 8}}
}, "meta": {"app": "https://www.codeandweb.com/texturepacker", "image": "sheet.png", "size": {"w": 128, "h": 64}}}`

const texturePackerArray = `{"frames": [
  {"filename": "a", "frame": {"x": 0, "y": 0, "w": 8, "h": 8}, "rotated": false, "trimmed": false,
    "spriteSourceSize": {"x": 0, "y": 0, "w": 8, "h": 8}, "sourceSize": {"w": 8, "h": 8}},
  {"filename": "b", "frame": {"x": 8, "y": 0, "w": 8, "h": 8}, "rotated": false, "trimmed": false,
    "spriteSourceSize": {"x": 0, "y": 0, "w": 8, "h": 8}, "sourceSize": {"w": 8, "h": 8}}
], "meta": {"image": "s.png", "size": {"w": 16, "h": 8}}}`

const aseprite = `{"frames": {
  "walk 0.": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "rotated": false, "trimmed": false,
    "spriteSourceSize": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}, "duration": 100},
  "walk 1.": {"frame": {"x": 16, "y": 0, "w": 16, "h": 16}, "rotated": false, "trimmed": false,
    "spriteSourceSize": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}, "duration": 150},
  "walk 2.": {"frame": {"x": 32, "y": 0, "w": 16, "h": 16}, "rotated": false, "trimmed": false,
    "spriteSourceSize": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}, "duration": 200},
  "walk 3.": {"frame": {"x": 48, "y": 0, "w": 16, "h": 16}, "rotated": false, "trimmed": false,
    "spriteSourceSize": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}, "duration": 250}
}, "meta": {"app": "http://www.aseprite.org/", "image": "walk.png", "size": {"w": 64, "h": 16},
  "frameTags": [
    {"name": "walk", "from": 0, "to": 2, "direction": "forward"},
    {"name": "bounce", "from": 0, "to": 2, "direction": "pingpong"},
    {"name": "back", "from": 2, "to": 3, "direction": "reverse"}
  ]}}`

func TestParseAtlasTexturePackerHash(t *testing.T) {
	d, err := ParseAtlas([]byte(texturePackerHash))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"hero/idle.png", "hero/run.png", "coin.png"}; !slices.Equal(d.Order, want) {
		t.Errorf("order %v", d.Order)
	}
	if d.Image != "sheet.png" || d.Size != lin.V2(128, 64) {
		t.Errorf("meta: %q %v", d.Image, d.Size)
	}
	run := d.Frames["hero/run.png"]
	if run.Rect != lin.R(32, 0, 24, 30) || !run.Trimmed || run.Offset != lin.V2(4, 2) || run.SourceSize != lin.V2(32, 32) {
		t.Errorf("run: %+v", run)
	}
	coin := d.Frames["coin.png"]
	if !coin.Rotated || coin.Rect != lin.R(64, 0, 8, 16) || coin.SourceSize != lin.V2(16, 8) {
		t.Errorf("rotated coin should have its packed size: %+v", coin)
	}
	if len(d.Tags) != 0 {
		t.Errorf("tags %v", d.Tags)
	}
	a := d.Bind(&Texture{Width: 128, Height: 64})
	r, ok := a.Region("hero/run.png")
	if !ok || r.UV0 != lin.V2(0.25, 0) || r.UV1 != lin.V2(56.0/128, 30.0/64) {
		t.Errorf("region: %v %v", ok, r)
	}
	if _, ok := a.Region("nope"); ok {
		t.Error("unknown frame found")
	}
	if !slices.Equal(a.Names(), d.Order) {
		t.Error("Names should follow file order")
	}
}

func TestParseAtlasTexturePackerArray(t *testing.T) {
	d, err := ParseAtlas([]byte(texturePackerArray))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Order, []string{"a", "b"}) || d.Frames["b"].Rect != lin.R(8, 0, 8, 8) {
		t.Errorf("array form: %v %v", d.Order, d.Frames)
	}
	a := d.Bind(&Texture{Width: 16, Height: 8})
	if r, _ := a.Region("b"); r.UV0 != lin.V2(0.5, 0) || r.UV1 != lin.V2(1, 1) {
		t.Errorf("region b %v", r)
	}
}

func TestParseAtlasAseprite(t *testing.T) {
	d, err := ParseAtlas([]byte(aseprite))
	if err != nil {
		t.Fatal(err)
	}
	if d.Frames["walk 1."].Duration != 0.15 {
		t.Errorf("duration %v", d.Frames["walk 1."].Duration)
	}
	cases := []struct {
		tag  string
		want []string
	}{
		{"walk", []string{"walk 0.", "walk 1.", "walk 2."}},
		{"bounce", []string{"walk 0.", "walk 1.", "walk 2.", "walk 1."}},
		{"back", []string{"walk 3.", "walk 2."}},
	}
	for _, c := range cases {
		if got := d.Tags[c.tag]; !slices.Equal(got, c.want) {
			t.Errorf("tag %s: %v, want %v", c.tag, got, c.want)
		}
	}
	a := d.Bind(&Texture{Width: 64, Height: 16})
	regions := a.Tag("walk")
	if len(regions) != 3 || regions[2].UV0 != lin.V2(0.5, 0) || regions[2].UV1 != lin.V2(0.75, 1) {
		t.Errorf("walk regions %v", regions)
	}
	if got := a.Durations("bounce"); !slices.Equal(got, []float32{0.1, 0.15, 0.2, 0.15}) {
		t.Errorf("bounce durations %v", got)
	}
	if a.Tag("nope") != nil || a.Durations("nope") != nil {
		t.Error("unknown tag should be nil")
	}
	if r, _ := a.Region("walk 0."); r.Size() != lin.V2(16, 16) {
		t.Errorf("region size %v", r.Size())
	}
}

func TestParseAtlasErrors(t *testing.T) {
	for _, src := range []string{`{`, `{"meta": {}}`, `{"frames": {"a": 1}}`} {
		if _, err := ParseAtlas([]byte(src)); err == nil {
			t.Errorf("%s: no error", src)
		}
	}
}
