package particle

import (
	"image"
	"image/color"
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// ShapeKind is the area particles are born in.
type ShapeKind uint8

const (
	ShapePoint  ShapeKind = iota // the system's position
	ShapeCircle                  // a disc of Radius, or its rim with Edge
	ShapeRect                    // a W by H box centred on the position, or its border with Edge
	ShapeLine                    // the segment from the position to position + To
)

// Shape is where particles are born, relative to the system's position.
// The zero Shape is a point. Point, Circle, Ring, Rect and Line make the
// common ones.
type Shape struct {
	Kind   ShapeKind
	Radius float32  // ShapeCircle
	W, H   float32  // ShapeRect
	To     lin.Vec2 // ShapeLine: the far end, relative to the position
	// Edge births particles on the outline only: the rim of a circle or
	// the border of a rectangle.
	Edge bool
}

// Point births every particle at the system's position.
func Point() Shape { return Shape{} }

// Circle births particles anywhere in a disc.
func Circle(radius float32) Shape { return Shape{Kind: ShapeCircle, Radius: radius} }

// Ring births particles on the rim of a circle.
func Ring(radius float32) Shape { return Shape{Kind: ShapeCircle, Radius: radius, Edge: true} }

// Rect births particles anywhere in a box centred on the position.
func Rect(w, h float32) Shape { return Shape{Kind: ShapeRect, W: w, H: h} }

// Line births particles along the segment from the position to
// position + to: a rain cloud along the top of the screen.
func Line(to lin.Vec2) Shape { return Shape{Kind: ShapeLine, To: to} }

// ColorKey is one point of a colour over a particle's lifetime.
type ColorKey struct {
	T     float32
	Color gfx.Color
}

// Emitter describes an effect: how often particles are born, where, how
// they move, and how they look from birth to death. Every field has a
// documented zero default, so an effect can start from an empty Emitter
// or a preset and set only what it needs. Emitters are plain values:
// copy one, change a field, and pass it to New or System.SetEmitter.
type Emitter struct {
	// Position is where the system starts; SetPosition moves it later.
	Position lin.Vec2

	// Rate is particles born per second while the system is emitting.
	// Zero births none over time, for effects that only Burst.
	Rate float32
	// Burst is how many particles are born at once by Start (including
	// the Start inside New). Zero is none. System.Burst takes its own
	// count.
	Burst int
	// Lifetime is how long each particle lives, in seconds. Zero is 1.
	Lifetime Range

	// Shape is where particles are born relative to the position. The
	// zero Shape is a point.
	Shape Shape
	// Direction is the angle particles travel in, radians from +X
	// towards +Y: 0 is right, -Pi/2 is up. Spread is the width of the
	// cone around it, so 2*Pi is every direction. Both default to zero.
	Direction float32
	Spread    float32
	// Speed is the starting speed in units per second. Zero is still.
	Speed Range

	// Acceleration is applied every second, so lin.V2(0, 400) is
	// gravity on a +Y-down screen. Zero is none.
	Acceleration lin.Vec2
	// Damping is the fraction of velocity lost per second. Zero is none.
	Damping float32
	// RadialAccel pushes particles away from where they were born
	// (negative pulls them back) and TangentialAccel pushes them around
	// it, both in units per second per second. Zero is none.
	RadialAccel     float32
	TangentialAccel float32

	// Size is a particle's width in view units at birth. Zero is the
	// texture's own width, or 8 with no texture. SizeOverLife scales it
	// over the lifetime; an empty curve is 1.
	Size         Range
	SizeOverLife Curve
	// Aspect is the height as a multiple of the width. Zero is the
	// texture's own proportion, or 1 with no texture.
	Aspect float32
	// Rotation is the starting angle in radians and Spin the turn in
	// radians per second. Zero is none.
	Rotation Range
	Spin     Range

	// Color tints particles at birth and ColorEnd at death; zero Color
	// is white and zero ColorEnd is Color. ColorOverLife, when set,
	// replaces both with keyframes across the lifetime. AlphaOverLife
	// multiplies the alpha; an empty curve is 1. Palette, when set,
	// gives each particle a random colour from it that multiplies the
	// colour over life, for mixed confetti.
	Color         gfx.Color
	ColorEnd      gfx.Color
	ColorOverLife []ColorKey
	AlphaOverLife Curve
	Palette       []gfx.Color

	// Texture is drawn for each particle; nil draws a plain quad, which
	// is fine for sparks and rain. Region draws a piece of a texture
	// instead, and Sheet plays frames across the lifetime: Frames lists
	// the frame indices to play (empty is every frame) and FrameOverLife
	// maps lifetime to a position in that list (empty is linear). Sheet
	// takes precedence over Region, and Region over Texture.
	Texture       *gfx.Texture
	Region        gfx.Region
	Sheet         *gfx.Sheet
	Frames        []int
	FrameOverLife Curve

	// Blend is the blend mode particles draw with; zero is alpha
	// blending, gfx.BlendAdd glows. Layer is the sprite layer; zero is 0.
	Blend gfx.Blend
	Layer int

	// WorldSpace keeps particles where they were born when the system
	// moves, as smoke should. The default, local space, carries them
	// with it, as a thruster flame should.
	WorldSpace bool

	// Max caps live particles; births beyond it wait. Zero is 1000.
	Max int
	// Seed starts the random stream, so the same seed replays the same
	// effect. Zero is a fixed seed.
	Seed uint64
	// Prewarm simulates this many seconds at Start so a fire is already
	// burning on its first frame. Zero is none.
	Prewarm float32
}

// max returns the live particle cap.
func (e *Emitter) max() int {
	if e.Max <= 0 {
		return 1000
	}
	return e.Max
}

// baseSize returns the width and aspect of the image drawn, for the
// Size and Aspect defaults.
func (e *Emitter) baseSize() (width, aspect float32) {
	var w, h float32
	switch {
	case e.Sheet != nil:
		w, h = float32(e.Sheet.FrameW), float32(e.Sheet.FrameH)
	case e.Region.Tex != nil:
		s := e.Region.Size()
		w, h = s.X, s.Y
	case e.Texture != nil:
		w, h = float32(e.Texture.Width), float32(e.Texture.Height)
	}
	if w <= 0 || h <= 0 {
		return 8, 1
	}
	return w, h / w
}

// colorAt is the colour of a particle at lifetime t, before its palette
// tint and alpha curve.
func (e *Emitter) colorAt(t float32) gfx.Color {
	if keys := e.ColorOverLife; len(keys) > 0 {
		if t <= keys[0].T {
			return keys[0].Color
		}
		for i := 1; i < len(keys); i++ {
			if t <= keys[i].T {
				a, b := keys[i-1], keys[i]
				if b.T == a.T {
					return b.Color
				}
				return a.Color.Lerp(b.Color, (t-a.T)/(b.T-a.T))
			}
		}
		return keys[len(keys)-1].Color
	}
	start, end := e.Color, e.ColorEnd
	if start == (gfx.Color{}) {
		start = gfx.White
	}
	if end == (gfx.Color{}) {
		return start
	}
	return start.Lerp(end, t)
}

// SoftCircle draws a white disc that fades to transparent at its edge,
// the texture most glowing particles want. Upload it with
// gfx.Graphics.NewTexture, with Linear filtering, and set it as an
// Emitter's Texture.
func SoftCircle(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			d := math.Sqrt(dx*dx+dy*dy) / r
			a := 1 - d
			if a < 0 {
				a = 0
			}
			a = a * a * (3 - 2*a)
			img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, uint8(a*255 + 0.5)})
		}
	}
	return img
}
