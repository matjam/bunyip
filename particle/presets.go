package particle

import (
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Presets are starting points tuned for a screen a few hundred units
// across with +Y down. They draw plain quads, so they work with no
// assets; setting Texture to a SoftCircle upload makes fire and smoke
// glow instead of flicker as squares.

// Fire is a rising flame: additive, yellow at the core fading through
// orange to dark red, shrinking as it climbs.
func Fire() Emitter {
	return Emitter{
		Rate:         90,
		Lifetime:     Range{0.6, 1.2},
		Shape:        Circle(10),
		Direction:    -math.Pi / 2,
		Spread:       0.6,
		Speed:        Range{40, 90},
		Acceleration: lin.V2(0, -50),
		Damping:      0.3,
		Size:         Range{14, 26},
		SizeOverLife: Keys(0, 0.5, 0.25, 1, 1, 0.15),
		ColorOverLife: []ColorKey{
			{0, gfx.RGB(255, 240, 180)},
			{0.3, gfx.RGB(255, 160, 40)},
			{0.7, gfx.RGB(200, 50, 10)},
			{1, gfx.RGB(60, 10, 0)},
		},
		AlphaOverLife: Keys(0, 0, 0.1, 1, 1, 0),
		Blend:         gfx.BlendAdd,
		Max:           400,
	}
}

// Smoke drifts up slowly, spreading and thinning as it goes. It is a
// WorldSpace effect so a trail stays behind a moving source.
func Smoke() Emitter {
	return Emitter{
		Rate:          22,
		Lifetime:      Range{1.6, 3},
		Shape:         Circle(8),
		Direction:     -math.Pi / 2,
		Spread:        0.7,
		Speed:         Range{18, 40},
		Acceleration:  lin.V2(6, -14),
		Size:          Range{16, 28},
		SizeOverLife:  Linear(0.6, 2.4),
		Spin:          Range{-0.6, 0.6},
		Color:         gfx.RGB(110, 110, 115),
		ColorEnd:      gfx.RGB(60, 60, 65),
		AlphaOverLife: Keys(0, 0, 0.15, 0.35, 1, 0),
		WorldSpace:    true,
		Max:           200,
	}
}

// Sparks fly out in every direction and fall under gravity: a burst of
// 40 per Start or Burst, additive, bright then fading.
func Sparks() Emitter {
	return Emitter{
		Burst:         40,
		Lifetime:      Range{0.3, 0.9},
		Spread:        2 * math.Pi,
		Speed:         Range{120, 340},
		Acceleration:  lin.V2(0, 420),
		Damping:       1.2,
		Size:          Range{2, 4},
		Color:         gfx.RGB(255, 245, 200),
		ColorEnd:      gfx.RGB(255, 120, 30),
		AlphaOverLife: Keys(0, 1, 0.6, 1, 1, 0),
		Blend:         gfx.BlendAdd,
		WorldSpace:    true,
		Max:           600,
	}
}

// Rain falls from a line 800 units wide at the system's position; set
// Shape to Line(lin.V2(width, 0)) to match the screen.
func Rain() Emitter {
	const slant = 0.12
	return Emitter{
		Rate:       350,
		Lifetime:   Range{1.4, 1.8},
		Shape:      Line(lin.V2(800, 0)),
		Direction:  math.Pi/2 - slant,
		Spread:     0.03,
		Speed:      Range{520, 680},
		Size:       Range{1.5, 2.5},
		Aspect:     7,
		Rotation:   Range{Min: slant},
		Color:      gfx.RGBA(170, 200, 255, 170),
		WorldSpace: true,
		Max:        1200,
	}
}

// Confetti pops 150 flat, spinning, coloured pieces upward that fall
// and fade; a burst-only effect that is Finished once they are gone.
func Confetti() Emitter {
	return Emitter{
		Burst:         150,
		Lifetime:      Range{1.6, 2.6},
		Direction:     -math.Pi / 2,
		Spread:        1.6,
		Speed:         Range{220, 480},
		Acceleration:  lin.V2(0, 520),
		Damping:       1.4,
		Size:          Range{5, 9},
		Aspect:        0.55,
		Rotation:      Range{0, 2 * math.Pi},
		Spin:          Range{-9, 9},
		AlphaOverLife: Keys(0, 1, 0.75, 1, 1, 0),
		Palette: []gfx.Color{
			gfx.RGB(255, 80, 90), gfx.RGB(255, 200, 40), gfx.RGB(70, 220, 120),
			gfx.RGB(70, 150, 255), gfx.RGB(220, 90, 255), gfx.RGB(255, 255, 255),
		},
		Max: 600,
	}
}
