package gfx

import (
	"fmt"
	"math"

	"github.com/matjam/bunyip/lin"
)

// PathOptions selects the paints baked into a CompiledPath. The zero value
// fills white. When either Fill or Stroke is non-nil, only the non-nil paints
// are used; Fill precedes Stroke. Zero colours mean white. To make a paint
// transparent, use a nonzero colour with A=0, or omit that paint.
type PathOptions struct {
	Fill                   *FillOptions
	Stroke                 *StrokeOptions
	FillColor, StrokeColor Color
	// PixelsPerUnit chooses curve precision and antialias fringe width.
	// Zero means 1. Choose the expected framebuffer pixels per local path
	// unit, including camera zoom and scale. Recompile when a substantially
	// different density is needed; drawing never tessellates the path again.
	PixelsPerUnit float32
}

// CompiledPath is a path's tessellated fill and stroke stored on the GPU.
// It captures the path, paint coordinates and colours at compilation time,
// and borrows any paint textures. Keep those textures alive while drawing it.
// Later changes to the source Path or paint options do not change the result.
// Graphics owns the compiled geometry; Destroy releases it early.
type CompiledPath struct {
	g      *Graphics
	parts  []compiledPathPart
	bounds lin.Rect
}

type compiledPathPart struct {
	texture  *Texture
	geometry *Geometry2D
}

// CompilePath tessellates a path once using opts, independently of current
// graphics state. DrawPath applies the current transform, camera, clipping,
// layer, blend and shader. Empty paths compile successfully and draw nothing.
func (g *Graphics) CompilePath(path *Path, opts PathOptions) (*CompiledPath, error) {
	if path == nil {
		return nil, fmt.Errorf("gfx: compile nil path")
	}
	density := opts.PixelsPerUnit
	if density == 0 {
		density = 1
	}
	if density <= 0 || math.IsInf(float64(density), 0) || math.IsNaN(float64(density)) {
		return nil, fmt.Errorf("gfx: path pixels per unit must be positive and finite")
	}
	if opts.Fill == nil && opts.Stroke == nil {
		opts.Fill = &FillOptions{}
	}
	if opts.FillColor == (Color{}) {
		opts.FillColor = White
	}
	if opts.StrokeColor == (Color{}) {
		opts.StrokeColor = White
	}
	c := &CompiledPath{g: g}
	add := func(tex *Texture, vertices []vertex2D) error {
		if len(vertices) == 0 {
			return nil
		}
		geometry, err := g.newGeometry2D(vertices, nil)
		if err != nil {
			return err
		}
		if len(c.parts) == 0 {
			c.bounds = geometry.Bounds()
		} else {
			c.bounds = c.bounds.Union(geometry.Bounds())
		}
		c.parts = append(c.parts, compiledPathPart{texture: tex, geometry: geometry})
		return nil
	}
	if opts.Fill != nil {
		if err := add(g.fillPath(path, opts.FillColor, *opts.Fill, 1/density)); err != nil {
			c.Destroy()
			return nil, err
		}
	}
	if opts.Stroke != nil {
		if err := add(g.strokePath(path, opts.StrokeColor, *opts.Stroke, 1/density)); err != nil {
			c.Destroy()
			return nil, err
		}
	}
	g.owned.add(c)
	return c, nil
}

// Bounds returns the local bounds of the compiled triangles, including
// stroke and antialias fringes. It excludes the drawing transform and camera.
func (c *CompiledPath) Bounds() lin.Rect { return c.bounds }

// Destroy releases the compiled geometry after queued GPU work finishes.
// Paint textures remain owned by their creators. Repeated calls do nothing.
func (c *CompiledPath) Destroy() {
	if c.g == nil {
		return
	}
	c.g.owned.remove(c)
	for _, p := range c.parts {
		p.geometry.Destroy()
	}
	c.parts = nil
}

// DrawPath queues a compiled path's fill followed by its stroke. Nil or
// destroyed compiled paths draw nothing. It performs no tessellation or
// vertex upload, and uses the same drawing state as DrawGeometry.
func (g *Graphics) DrawPath(path *CompiledPath) {
	if path == nil {
		return
	}
	for _, p := range path.parts {
		g.DrawGeometry(p.texture, p.geometry)
	}
}
