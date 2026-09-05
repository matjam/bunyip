package particle

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// TestEmitterRoundTrip checks that every preset survives being saved and
// loaded unchanged, which is what lets a game ship its effects as
// assets and an editor save what it tuned.
func TestEmitterRoundTrip(t *testing.T) {
	presets := map[string]Emitter{
		"fire":     Fire(),
		"smoke":    Smoke(),
		"sparks":   Sparks(),
		"rain":     Rain(),
		"confetti": Confetti(),
		"empty":    {},
	}
	for name, want := range presets {
		t.Run(name, func(t *testing.T) {
			data, err := Save(want)
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, err := Load(data)
			if err != nil {
				t.Fatalf("Load: %v\n%s", err, data)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip changed the emitter\n got %+v\nwant %+v\n%s", got, want, data)
			}
		})
	}
}

// TestEmitterRoundTripEverySet sets every plain field to something other
// than its zero, so a field added without being added to the JSON form
// fails here rather than being silently dropped.
func TestEmitterRoundTripEverySet(t *testing.T) {
	want := Emitter{
		Position: lin.V2(3, 4), Rate: 12, Burst: 5,
		Lifetime: Range{1, 2}, Shape: Rect(10, 20), Direction: 0.5, Spread: 1.5,
		Speed: Range{6, 7}, Acceleration: lin.V2(0, 9), Damping: 0.25,
		RadialAccel: 1.5, TangentialAccel: -2.5,
		Size: Range{2, 3}, SizeOverLife: Linear(1, 0), Aspect: 0.5,
		Rotation: Range{0.1, 0.2}, Spin: Range{-1, 1},
		Color: gfx.RGB(255, 10, 20), ColorEnd: gfx.RGB(1, 2, 3),
		ColorOverLife: []ColorKey{{0, gfx.White}, {1, gfx.Black}},
		AlphaOverLife: Keys(0, 0, 1, 1),
		Palette:       []gfx.Color{gfx.RGB(1, 2, 3), gfx.RGB(4, 5, 6)},
		TextureName:   "effects/spark.png",
		Frames:        []int{2, 3, 5}, FrameOverLife: Linear(0, 1),
		Blend: gfx.BlendAdd, Layer: 4, WorldSpace: true,
		Max: 5000, Seed: 99, Prewarm: 1.25, Stateless: true,
	}
	data, err := Save(want)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(data)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed the emitter\n got %+v\nwant %+v\n%s", got, want, data)
	}
	// Count the fields written against the fields an Emitter has, less
	// the three GPU resources that cannot be saved.
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	const gpuFields = 3 // Texture, Region, Sheet
	if want := reflect.TypeOf(Emitter{}).NumField() - gpuFields; len(fields) != want {
		t.Errorf("saved %d fields, an Emitter has %d that can be saved; add the new one to emitterJSON", len(fields), want)
	}
}

// TestEmitterJSONKeepsGPUFields checks that loading over an emitter that
// already has a texture leaves it alone, so an editor can reload an
// effect without losing what it uploaded.
func TestEmitterJSONKeepsGPUFields(t *testing.T) {
	sheet := &gfx.Sheet{FrameW: 8, FrameH: 8, Columns: 2, Rows: 2}
	e := Emitter{Sheet: sheet, Rate: 1}
	data, err := Save(Fire())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if e.Sheet != sheet {
		t.Error("loading an emitter cleared the sheet it was loaded over")
	}
	if e.Rate != Fire().Rate {
		t.Errorf("rate = %v, want the loaded %v", e.Rate, Fire().Rate)
	}
}

// TestEmitterJSONOmitsDefaults checks that a file holds only what the
// effect changed, so a hand-written emitter can be three lines long.
func TestEmitterJSONOmitsDefaults(t *testing.T) {
	data, err := Save(Emitter{Rate: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), ":"); got != 1 {
		t.Errorf("an emitter with one field set saved %d fields:\n%s", got, data)
	}
}

// TestEmitterJSONBadBlend checks that an unknown blend mode is an error
// rather than silently becoming alpha.
func TestEmitterJSONBadBlend(t *testing.T) {
	if _, err := Load([]byte(`{"blend":"glow"}`)); err == nil {
		t.Error("an unknown blend mode loaded without an error")
	}
}

// TestEmitterJSONNamesBlends checks the readable form, since the file is
// meant to be edited by hand as well as by the editor.
func TestEmitterJSONNamesBlends(t *testing.T) {
	data, err := Save(Emitter{Blend: gfx.BlendAdd})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"blend": "add"`) {
		t.Errorf("blend was not saved by name:\n%s", data)
	}
}
