package gfx

import (
	"image"

	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/render"
)

// driver is how the engine loop drives a Graphics: it builds the frame,
// resizes it and destroys it. The methods are exported to satisfy
// hook.Graphics, but the type is not, so none of this plumbing appears
// on the surface a game reads.
type driver struct{ g *Graphics }

func (d driver) Begin(clear [4]float32) (bool, error) {
	return d.g.begin(Color{R: clear[0], G: clear[1], B: clear[2], A: clear[3]})
}

func (d driver) End(capture bool) (*image.RGBA, error) { return d.g.end(capture) }
func (d driver) Resize(width, height int)              { d.g.resize(width, height) }
func (d driver) SetTime(seconds float64)               { d.g.setTime(seconds) }
func (d driver) Destroy()                              { d.g.destroy() }
func (d driver) Game() any                             { return d.g }

func init() {
	hook.NewGraphics = func(r *render.Renderer) (hook.Graphics, error) {
		g, err := newGraphics(r)
		if err != nil {
			return nil, err
		}
		return driver{g}, nil
	}
}
