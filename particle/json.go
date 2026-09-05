package particle

import (
	"encoding/json"
	"fmt"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// An Emitter is JSON so a game can ship its effects as assets and an
// editor can save what it tuned. Every plain field is written; the GPU
// resources (Texture, Region and Sheet) are not, because a file cannot
// hold an uploaded texture. TextureName carries the asset path instead,
// and asset.Emitter loads the texture it names.
//
// Colours are the linear values the engine works in rather than sRGB
// bytes, so a saved emitter reloads exactly as it was. Zero fields are
// left out, so a file holds only what the effect changed from the
// defaults.

// emitterJSON mirrors Emitter's plain fields. It is a separate struct
// rather than tags on Emitter so the field names in the file stay
// stable if the struct is rearranged, and so the GPU fields cannot be
// written by accident.
type emitterJSON struct {
	Position        *lin.Vec2   `json:"position,omitempty"`
	Rate            float32     `json:"rate,omitempty"`
	Burst           int         `json:"burst,omitempty"`
	Lifetime        *Range      `json:"lifetime,omitempty"`
	Shape           *Shape      `json:"shape,omitempty"`
	Direction       float32     `json:"direction,omitempty"`
	Spread          float32     `json:"spread,omitempty"`
	Speed           *Range      `json:"speed,omitempty"`
	Acceleration    *lin.Vec2   `json:"acceleration,omitempty"`
	Damping         float32     `json:"damping,omitempty"`
	RadialAccel     float32     `json:"radialAccel,omitempty"`
	TangentialAccel float32     `json:"tangentialAccel,omitempty"`
	Size            *Range      `json:"size,omitempty"`
	SizeOverLife    Curve       `json:"sizeOverLife,omitempty"`
	Aspect          float32     `json:"aspect,omitempty"`
	Rotation        *Range      `json:"rotation,omitempty"`
	Spin            *Range      `json:"spin,omitempty"`
	Color           *gfx.Color  `json:"color,omitempty"`
	ColorEnd        *gfx.Color  `json:"colorEnd,omitempty"`
	ColorOverLife   []ColorKey  `json:"colorOverLife,omitempty"`
	AlphaOverLife   Curve       `json:"alphaOverLife,omitempty"`
	Palette         []gfx.Color `json:"palette,omitempty"`
	TextureName     string      `json:"texture,omitempty"`
	Frames          []int       `json:"frames,omitempty"`
	FrameOverLife   Curve       `json:"frameOverLife,omitempty"`
	Blend           string      `json:"blend,omitempty"`
	Layer           int         `json:"layer,omitempty"`
	WorldSpace      bool        `json:"worldSpace,omitempty"`
	Max             int         `json:"max,omitempty"`
	Seed            uint64      `json:"seed,omitempty"`
	Prewarm         float32     `json:"prewarm,omitempty"`
	Stateless       bool        `json:"stateless,omitempty"`
}

// omit returns p when v is not the zero value, so omitempty works for
// the struct fields Go would otherwise always write.
func omit[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// value reads a pointer that may be absent.
func value[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// MarshalJSON writes the emitter's plain fields. The texture, region and
// sheet are not written; TextureName is, so a loader can put them back.
func (e Emitter) MarshalJSON() ([]byte, error) {
	j := emitterJSON{
		Position:        omit(e.Position),
		Rate:            e.Rate,
		Burst:           e.Burst,
		Lifetime:        omit(e.Lifetime),
		Shape:           omit(e.Shape),
		Direction:       e.Direction,
		Spread:          e.Spread,
		Speed:           omit(e.Speed),
		Acceleration:    omit(e.Acceleration),
		Damping:         e.Damping,
		RadialAccel:     e.RadialAccel,
		TangentialAccel: e.TangentialAccel,
		Size:            omit(e.Size),
		SizeOverLife:    e.SizeOverLife,
		Aspect:          e.Aspect,
		Rotation:        omit(e.Rotation),
		Spin:            omit(e.Spin),
		Color:           omit(e.Color),
		ColorEnd:        omit(e.ColorEnd),
		ColorOverLife:   e.ColorOverLife,
		AlphaOverLife:   e.AlphaOverLife,
		Palette:         e.Palette,
		TextureName:     e.TextureName,
		Frames:          e.Frames,
		FrameOverLife:   e.FrameOverLife,
		Layer:           e.Layer,
		WorldSpace:      e.WorldSpace,
		Max:             e.Max,
		Seed:            e.Seed,
		Prewarm:         e.Prewarm,
		Stateless:       e.Stateless,
	}
	if e.Blend != gfx.BlendAlpha {
		j.Blend = e.Blend.String()
	}
	return json.Marshal(j)
}

// UnmarshalJSON reads an emitter written by MarshalJSON. Fields the file
// leaves out keep their zero, which is their documented default, so a
// short file is a valid emitter. The texture, region and sheet are left
// alone, so an emitter can be unmarshalled over one that already has
// them.
func (e *Emitter) UnmarshalJSON(data []byte) error {
	var j emitterJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("particle: emitter: %w", err)
	}
	blend := gfx.BlendAlpha
	if j.Blend != "" {
		b, ok := gfx.ParseBlend(j.Blend)
		if !ok {
			return fmt.Errorf("particle: emitter: unknown blend mode %q", j.Blend)
		}
		blend = b
	}
	e.Position = value(j.Position)
	e.Rate = j.Rate
	e.Burst = j.Burst
	e.Lifetime = value(j.Lifetime)
	e.Shape = value(j.Shape)
	e.Direction = j.Direction
	e.Spread = j.Spread
	e.Speed = value(j.Speed)
	e.Acceleration = value(j.Acceleration)
	e.Damping = j.Damping
	e.RadialAccel = j.RadialAccel
	e.TangentialAccel = j.TangentialAccel
	e.Size = value(j.Size)
	e.SizeOverLife = j.SizeOverLife
	e.Aspect = j.Aspect
	e.Rotation = value(j.Rotation)
	e.Spin = value(j.Spin)
	e.Color = value(j.Color)
	e.ColorEnd = value(j.ColorEnd)
	e.ColorOverLife = j.ColorOverLife
	e.AlphaOverLife = j.AlphaOverLife
	e.Palette = j.Palette
	e.TextureName = j.TextureName
	e.Frames = j.Frames
	e.FrameOverLife = j.FrameOverLife
	e.Blend = blend
	e.Layer = j.Layer
	e.WorldSpace = j.WorldSpace
	e.Max = j.Max
	e.Seed = j.Seed
	e.Prewarm = j.Prewarm
	e.Stateless = j.Stateless
	return nil
}

// Save writes an emitter as indented JSON, the form an editor saves and
// asset.Emitter loads.
func Save(e Emitter) ([]byte, error) {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Load reads an emitter from the JSON Save writes. The GPU fields are
// left nil; set Texture, Region or Sheet after loading, or use
// asset.Emitter, which loads the texture TextureName asks for.
func Load(data []byte) (Emitter, error) {
	var e Emitter
	if err := json.Unmarshal(data, &e); err != nil {
		return Emitter{}, err
	}
	return e, nil
}
