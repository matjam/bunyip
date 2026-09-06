package gfx

import (
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/lin"
)

// StencilOptions controls stencil testing and updates for 2D fragments.
// Zero options draw normally and leave stencil unchanged. Zero ReadMask and
// WriteMask mean all eight bits; DisableWrite prevents all stencil updates.
// NoColor suppresses colour writes while retaining fragment stencil updates.
// The test compares the masked stored value to the masked Reference.
type StencilOptions struct {
	Test                  StencilTest
	Reference             uint8
	Pass, Fail, DepthFail StencilOp
	ReadMask, WriteMask   uint8
	DisableWrite          bool
	NoColor               bool
}

type stencil2D struct {
	options StencilOptions
	set     bool
}

func (s stencil2D) renderState() *render.StencilState {
	if !s.set {
		return nil
	}
	o := s.options
	return &render.StencilState{Compare: o.Test.compareOp(), Ref: uint32(o.Reference), Write: !o.DisableWrite,
		Pass: o.Pass.vkOp(), Fail: o.Fail.vkOp(), DepthFail: o.DepthFail.vkOp(), ReadMask: uint32(o.ReadMask), WriteMask: uint32(o.WriteMask)}
}

func (g *Graphics) requireStencil() {
	if g.cur.out.noDepth || !render.HasStencil(g.r.DepthFormat) {
		panic("gfx: stencil drawing requires a target with a stencil attachment")
	}
}

// Stenciled applies advanced stencil controls to 2D drawing, including
// geometry and flat particles, and restores the previous options on panic.
// Options are captured per draw, but ordinary layer and sort-key ordering
// still applies. Use Masked for automatic mask setup and ordering boundaries.
// Stencil contents persist for the rest of this target's frame. Invalid
// options or a target without stencil panic before draw is called.
func (g *Graphics) Stenciled(options StencilOptions, draw func()) {
	if options.Test >= stencilTestCount || options.Pass >= stencilOpCount || options.Fail >= stencilOpCount || options.DepthFail >= stencilOpCount {
		panic("gfx: invalid stencil options")
	}
	g.requireStencil()
	q := g.cur
	previous := q.stencil2D
	defer func() { q.stencil2D = previous }()
	q.stencil2D = stencil2D{options: options, set: true}
	draw()
}

// ClearStencil queues a stencil-only clear in the current view and clips.
// Drawing before and after it cannot reorder across the clear, even when
// their layers or sort keys differ. Colour and depth remain unchanged.
// It panics if the target has no stencil attachment.
func (g *Graphics) ClearStencil(value uint8) {
	g.requireStencil()
	q := g.cur
	q.stream.barrier()
	g.clearStencilBits(q, value, 0xff, true)
	q.stream.barrier()
}

// Masked draws only where mask rasterizes fragments, composing up to eight
// nested masks. Mask drawing writes no colour. Transparent fragments still
// mark coverage unless their shader discards them; use an opaque shape or
// a shader that discards outside the desired mask.
//
// Setup, mask, body and cleanup are separate ordering groups. Layers and
// sort keys order within each group, so callbacks may change either safely.
// Masked restores the original stencil, layer and sort key even on panic;
// queued draws remain queued. It temporarily uses one low stencil bit per
// nesting level, clears that bit afterward, and preserves all other bits.
// Advanced Stenciled state is suspended during the mask and then restored.
// A helper using Masked may also be called from mask: its clipped drawing
// contributes to the enclosing mask's coverage without writing colour.
// A depthless target or more than eight nested masks panic before mutation.
func (g *Graphics) Masked(mask, draw func()) {
	g.requireStencil()
	q := g.cur
	if q.maskDepth >= 8 {
		panic("gfx: at most eight nested masks are supported")
	}
	bit := uint8(1 << q.maskDepth)
	parent, writes := q.maskTests, q.maskWrites
	previous, layer, key := q.stencil2D, q.layer, q.sortKey
	q.maskDepth++
	defer func() {
		q.stream.barrier()
		g.clearStencilBits(q, 0, bit, false)
		q.stream.barrier()
		q.stencil2D, q.layer, q.sortKey = previous, layer, key
		q.maskTests, q.maskWrites = parent, writes
		q.maskDepth--
	}()
	q.stream.barrier()
	g.clearStencilBits(q, 0, bit, false)
	q.stream.barrier()
	writer := StencilOptions{Reference: parent | bit, Pass: StencilReplace, ReadMask: parent, WriteMask: bit, NoColor: true}
	if parent != 0 {
		writer.Test = StencilEqual
	}
	q.stencil2D = stencil2D{options: writer, set: true}
	q.maskTests, q.maskWrites = parent, bit
	mask()
	q.stream.barrier()
	q.layer, q.sortKey = layer, key
	q.maskTests, q.maskWrites = parent|bit, writes
	body := StencilOptions{Test: StencilEqual, Reference: parent | bit | writes, ReadMask: parent | bit, DisableWrite: writes == 0}
	if writes != 0 {
		body.Pass, body.WriteMask, body.NoColor = StencilReplace, writes, true
	}
	q.stencil2D = stencil2D{options: body, set: true}
	draw()
}

// clearStencilBits uses the stock shader and no drawing transform/camera.
// Masked clears its owned bit over the target; public clears retain the view.
func (g *Graphics) clearStencilBits(q *drawQueue, value, bits uint8, local bool) {
	w, h, proj := q.rootW, q.rootH, lin.Ortho2D(q.rootW, q.rootH)
	clip := lin.Rect{}
	if local {
		w, h, proj = q.viewW, q.viewH, q.proj
		if n := len(q.clips); n > 0 {
			clip = q.clips[n-1]
		}
	}
	st := state2D{shader: g.spriteShader, uniform: -1, set: g.white.setFor(FilterDefault),
		proj: q.stream.proj(proj), transform: lin.Identity2(), clip: clip, group: q.stream.group,
		frame: lin.V4(g.time, w, h, 1), stencil: stencil2D{set: true, options: StencilOptions{Reference: value, WriteMask: bits, Pass: StencilReplace, NoColor: true}}}
	vertices := [6]vertex2D{{pos: lin.V2(0, 0)}, {pos: lin.V2(w, 0)}, {pos: lin.V2(w, h)}, {pos: lin.V2(0, 0)}, {pos: lin.V2(w, h)}, {pos: lin.V2(0, h)}}
	q.stream.add(st, 0, 0, vertices[:])
}
