package main

import (
	"fmt"
	"os"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/particle"
	"github.com/matjam/bunyip/ui"
)

// editor tunes a particle emitter while it burns: every widget writes
// into the emitter and hands it straight back to the running system, so
// the preview beside the panel is always the effect the numbers
// describe. Save writes the emitter as JSON, the form asset.Emitter
// loads, so a game ships its effects as assets rather than as code.
type editor struct {
	emitter particle.Emitter
	system  *particle.System
	win     ui.Rect
	tab     int
	preset  int
	blend   int
	// The two curves as the points ui.CurveEditor edits, converted back
	// into the emitter whenever they change.
	size  []lin.Vec2
	alpha []lin.Vec2
	// spread and direction are shown in degrees, which is what anyone
	// tuning an effect thinks in; the emitter keeps radians.
	direction float32
	spread    float32
	gravity   float32
	path      string
	status    string
	soft      *gfx.Texture
	at        lin.Vec2
}

// presets are the starting points the dropdown offers, in the order it
// lists them.
var presets = []string{"Fire", "Smoke", "Sparks", "Confetti"}

// preset returns one of them by index.
func preset(i int) particle.Emitter {
	switch i {
	case 1:
		return particle.Smoke()
	case 2:
		e := particle.Sparks()
		e.Rate, e.Burst = 120, 0 // a steady fountain rather than one pop
		return e
	case 3:
		e := particle.Confetti()
		e.Rate, e.Burst = 40, 0
		return e
	}
	return particle.Fire()
}

// newEditor starts the editor on the fire preset at a point in the view.
func newEditor(soft *gfx.Texture, at lin.Vec2) *editor {
	ed := &editor{
		win:  ui.Rect{X: 900, Y: 24, W: 300, H: 560},
		soft: soft,
		at:   at,
		path: "emitter.json",
		// The Look tab holds the curve editors, which is the widget this
		// panel exists to show, so the gallery opens on it.
		tab: 2,
	}
	ed.load(preset(0))
	return ed
}

// load takes an emitter as the one being edited: the texture and the
// position are the editor's, the widgets read their state out of it, and
// a fresh system runs it.
func (ed *editor) load(e particle.Emitter) {
	e.Position = ed.at
	e.Texture = ed.soft
	e.TextureName = "soft-circle" // what asset.Emitter would load
	e.Layer = 1
	if e.Seed == 0 {
		e.Seed = 5 // a fixed stream, so the preview replays the same way
	}
	ed.emitter = e
	ed.size = e.SizeOverLife.Points()
	ed.alpha = e.AlphaOverLife.Points()
	ed.direction = lin.Degrees(e.Direction)
	ed.spread = lin.Degrees(e.Spread)
	ed.gravity = e.Acceleration.Y
	ed.blend = int(e.Blend)
	ed.system = particle.New(e)
}

// apply pushes the widgets' values back into the emitter and retunes the
// running system, keeping the particles already in the air.
func (ed *editor) apply() {
	e := &ed.emitter
	e.Direction = lin.Radians(ed.direction)
	e.Spread = lin.Radians(ed.spread)
	e.Acceleration.Y = ed.gravity
	e.SizeOverLife = particle.CurveOf(ed.size)
	e.AlphaOverLife = particle.CurveOf(ed.alpha)
	e.Blend = gfx.Blend(ed.blend)
	ed.system.SetEmitter(*e)
}

// update advances the preview.
func (ed *editor) update(dt float64) { ed.system.Update(dt) }

// preview draws the effect and a mark where it is emitting from.
func (ed *editor) preview(g *gfx.Graphics) {
	g.SetLayer(0)
	g.StrokeCircle(ed.at.X, ed.at.Y, 5, 1, gfx.RGBA(200, 200, 220, 90))
	ed.system.Draw(g)
	g.SetLayer(0)
}

// draw builds the editor window. Every widget reports whether it changed
// something, so the emitter is rebuilt only when one did.
func (ed *editor) draw(u *ui.Context) {
	u.Window("Particle editor", &ed.win, func() {
		if u.Dropdown("Preset", &ed.preset, presets) {
			ed.load(preset(ed.preset))
		}
		u.Tabs([]string{"Emission", "Motion", "Look"}, &ed.tab)
		changed := false
		switch ed.tab {
		case 0:
			changed = u.Slider("Rate (per second)", &ed.emitter.Rate, 0, 400) || changed
			changed = u.Slider("Life from (s)", &ed.emitter.Lifetime.Min, 0.05, 4) || changed
			changed = u.Slider("Life to (s)", &ed.emitter.Lifetime.Max, 0.05, 4) || changed
			changed = u.Slider("Radius", &ed.emitter.Shape.Radius, 0, 80) || changed
			u.Label(fmt.Sprintf("%d alive of %d", ed.system.Alive(), ed.emitter.Max))
		case 1:
			changed = u.Slider("Direction (deg)", &ed.direction, -180, 180) || changed
			changed = u.Slider("Spread (deg)", &ed.spread, 0, 360) || changed
			changed = u.Slider("Speed from", &ed.emitter.Speed.Min, 0, 400) || changed
			changed = u.Slider("Speed to", &ed.emitter.Speed.Max, 0, 400) || changed
			changed = u.Slider("Gravity", &ed.gravity, -400, 400) || changed
			changed = u.Slider("Damping", &ed.emitter.Damping, 0, 4) || changed
		case 2:
			changed = u.Slider("Size from", &ed.emitter.Size.Min, 1, 60) || changed
			changed = u.Slider("Size to", &ed.emitter.Size.Max, 1, 60) || changed
			// Drag a point to bend the curve, click the graph to add one,
			// right-click a point to take it away.
			changed = u.CurveEditor("Size over life", &ed.size, 0, 3, 64) || changed
			changed = u.CurveEditor("Alpha over life", &ed.alpha, 0, 1, 64) || changed
			changed = u.Dropdown("Blend", &ed.blend, []string{"alpha", "add", "multiply", "screen"}) || changed
			if len(ed.emitter.ColorOverLife) == 0 {
				changed = u.ColorPicker("Tint", &ed.emitter.Color) || changed
			} else {
				u.Label("Tinted by a colour-over-life gradient.")
			}
		}
		if changed {
			ed.apply()
		}
		u.Separator()
		u.TextField("File", &ed.path)
		u.Row(2, func() {
			if u.Button("Save") {
				ed.status = ed.save()
			}
			if u.Button("Load") {
				ed.status = ed.loadFile()
			}
		})
		if ed.status != "" {
			u.Label(ed.status)
		}
	})
}

// save writes the emitter as JSON and reports what happened, for the
// line under the buttons.
func (ed *editor) save() string {
	data, err := particle.Save(ed.emitter)
	if err != nil {
		return err.Error()
	}
	if err := os.WriteFile(ed.path, data, 0o644); err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Saved %d bytes to %s", len(data), ed.path)
}

// loadFile reads an emitter back. The texture and the position are the
// editor's own, so a file written anywhere still previews here.
func (ed *editor) loadFile() string {
	data, err := os.ReadFile(ed.path)
	if err != nil {
		return err.Error()
	}
	e, err := particle.Load(data)
	if err != nil {
		return err.Error()
	}
	ed.load(e)
	return "Loaded " + ed.path
}

// softCircle uploads the white disc that fades at its edge, the texture
// most glowing particles want.
func softCircle(g *gfx.Graphics) (*gfx.Texture, error) {
	return g.NewTexture(particle.SoftCircle(64), gfx.TextureOptions{Linear: true})
}
