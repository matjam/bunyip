package gfx

import "github.com/matjam/bunyip/lin"

// Layered runs draw on layer, then restores the original queue's layer,
// including when draw panics. Drawing already queued is not undone.
func (g *Graphics) Layered(layer int, draw func()) {
	q := g.cur
	previous := q.layer
	q.layer = int32(layer)
	defer func() { q.layer = previous }()
	draw()
}

// WithCamera2D runs draw under cam, then restores the original queue's
// camera or screen-space state, including when draw panics.
func (g *Graphics) WithCamera2D(cam Camera2D, draw func()) {
	q := g.cur
	previous, hadCamera := q.cam2D, q.hasCam2D
	projection, visible := q.spriteProj, q.visible
	g.SetCamera2D(cam)
	defer func() {
		q.cam2D, q.hasCam2D = previous, hadCamera
		q.spriteProj, q.visible = projection, visible
	}()
	draw()
}

// ConfigurePost edits a copy of the current global post-processing
// settings and commits it when edit returns normally. A panic leaves
// the settings unchanged unless edit changed them directly. Numeric zero
// is kept as supplied; fields not edited retain their current values.
// Like SetPost, the result applies to the screen and all render textures
// when the frame submits. Do not retain the pointer passed to edit.
func (g *Graphics) ConfigurePost(edit func(*PostSettings)) {
	settings := g.Post()
	edit(&settings)
	g.SetPost(settings)
}

// Blended runs draw with the blend mode set, then restores the original
// queue's mode, including when draw panics.
func (g *Graphics) Blended(b Blend, draw func()) {
	q := g.cur
	previous, custom := q.blend, q.customBlend
	g.SetBlend(b)
	defer func() { q.blend, q.customBlend = previous, custom }()
	draw()
}

// Transformed runs draw with the transform pushed, then restores the
// original queue's transform stack, including when draw panics.
func (g *Graphics) Transformed(m lin.Affine, draw func()) {
	q := g.cur
	previous, stack := q.xform, q.xforms
	g.PushTransform(m)
	defer func() { q.xform, q.xforms = previous, stack }()
	draw()
}

// Shaded runs draw with the shader set, then restores the original
// queue's shader, including when draw panics.
func (g *Graphics) Shaded(s *Shader, draw func()) {
	q := g.cur
	previous := q.shader
	g.SetShader(s)
	defer func() { q.shader = previous }()
	draw()
}

// ColorMatrixed runs draw with the matrix set, then restores the original
// queue's matrix, including when draw panics.
func (g *Graphics) ColorMatrixed(m ColorMatrix, draw func()) {
	q := g.cur
	previous := q.colorMatrix
	g.SetColorMatrix(&m)
	defer func() {
		q.colorMatrix = previous
		if g.cur == q && previous != nil {
			g.recordDrawError(g.matrixShader.SetUniforms(previous))
		}
	}()
	draw()
}

// Clip runs draw clipped to r intersected with the enclosing clip, then
// restores the original queue's clip stack, including when draw panics.
func (g *Graphics) Clip(r lin.Rect, draw func()) {
	q := g.cur
	previous := q.clips
	g.PushClip(r)
	defer func() { q.clips = previous }()
	draw()
}
