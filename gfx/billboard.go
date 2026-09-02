package gfx

import "github.com/matjam/bunyip/lin"

// Billboard is a textured quad in the 3D scene that turns to face the
// camera: a health bar over a unit, a name over a player, a tree or a
// bush in a scene that cannot afford a model, a glow around a star, a
// puff of smoke. It draws through the mesh path, so it is lit, fogged
// and shadowed like any mesh when asked to be, and many billboards with
// one texture become one instanced draw.
type Billboard struct {
	Texture  *Texture // nil draws a flat colour
	Region   Region   // a part of the texture, for atlases and sprite sheets; zero means all of it
	Position lin.Vec3 // where the quad's centre sits, before Offset
	Size     lin.Vec2 // width and height in world units; zero means 1 by 1
	// Offset moves the quad in its own plane in units of its size: (0,
	// 0.5) puts Position at the bottom edge, for a sprite standing on
	// the ground.
	Offset lin.Vec2
	Color  Color // tint; zero means white
	// Upright turns the quad about the world's up axis only, so it stays
	// vertical when the camera looks down on it: trees, characters.
	Upright bool
	// Lit shades the quad with the scene's lights; otherwise it shows
	// its texture as it is.
	Lit bool
	// Cutout draws hard edges: alpha under half is discarded, the rest
	// writes depth and casts shadows. Otherwise the quad blends its
	// alpha over the scene, after the opaque draws.
	Cutout bool
	// OnTop draws over everything, for labels that must not be hidden.
	OnTop bool
}

// DrawBillboard draws a camera-facing quad in the scene.
func (g *Graphics) DrawBillboard(b Billboard) {
	q := g.cur
	cam := q.camera
	if !q.hasCam {
		cam = Camera{Position: lin.V3(0, 0, 5)}
	}
	up, _, _, _ := cam.defaults()
	size := b.Size
	if size == (lin.Vec2{}) {
		size = lin.V2(1, 1)
	}
	// The quad's +z faces the camera: its forward is away from it.
	forward := b.Position.Sub(cam.Position)
	if b.Upright {
		forward = forward.Sub(up.Mul(forward.Dot(up)))
	}
	if forward.Len() < 1e-6 {
		forward = cam.Target.Sub(cam.Position)
	}
	if !b.Upright {
		// Face the camera's plane rather than its position, so a field of
		// billboards all turn together instead of fanning.
		forward = cam.Target.Sub(cam.Position)
		if b.Upright {
			forward = forward.Sub(up.Mul(forward.Dot(up)))
		}
	}
	rot := lin.QuatLookAt(forward.Norm(), up)
	pos := b.Position.Add(rot.Rotate(lin.V3(b.Offset.X*size.X, b.Offset.Y*size.Y, 0)))
	model := lin.TRS(pos, rot, lin.V3(size.X, size.Y, 1))
	tex := b.Texture
	if tex == nil {
		tex = b.Region.Tex
	}
	mat := Material{Texture: tex, BaseColor: b.Color, Unlit: !b.Lit, DoubleSided: true, NoDepthTest: b.OnTop, NoDepthWrite: b.OnTop}
	if b.Cutout {
		mat.AlphaCutoff = 0.5
	} else {
		mat.Blend = true
	}
	if r := b.Region; r.UV1 != (lin.Vec2{}) || r.UV0 != (lin.Vec2{}) {
		mat.UVTransform = lin.Affine{A: r.UV1.X - r.UV0.X, C: r.UV0.X, E: r.UV1.Y - r.UV0.Y, F: r.UV0.Y}
	}
	g.DrawMesh(g.quadMesh(), mat, model)
}

// quadMesh is the shared unit quad billboards and 3D text draw with.
func (g *Graphics) quadMesh() *Mesh {
	mp := &g.meshes
	if mp.quad == nil {
		v, i := QuadMesh()
		mp.quad, _ = g.NewMesh(v, i)
	}
	return mp.quad
}

// DrawText3D draws a line of text in the scene facing the camera, its
// origin at pos, scale world units per view unit of the font (a 32 unit
// font at 0.02 stands about 0.64 units tall). The text is centred on pos
// and drawn on top of the scene when onTop is set; opts gives alignment
// and size as for DrawText. Each glyph is a billboard, so a label is one
// instanced draw.
func (g *Graphics) DrawText3D(f *Font, text string, pos lin.Vec3, scale float32, c Color, onTop bool, opts TextOptions) {
	if f == nil || text == "" {
		return
	}
	if scale <= 0 {
		scale = 0.01
	}
	if c == (Color{}) {
		c = White
	}
	q := g.cur
	cam := q.camera
	if !q.hasCam {
		cam = Camera{Position: lin.V3(0, 0, 5)}
	}
	up, _, _, _ := cam.defaults()
	rot := lin.QuatLookAt(cam.Target.Sub(cam.Position).Norm(), up)
	glyphs := f.Shape(text, opts)
	w, _ := f.Measure(text, opts)
	tex := f.Texture()
	if tex == nil {
		return
	}
	// Glyph positions are in view units, y down from the ascent; place
	// each as a quad in the text's plane, centred on pos.
	for _, gl := range glyphs {
		if gl.Empty {
			continue
		}
		cx := (gl.Pos.X + gl.Size.X/2 - w/2) * scale
		cy := (f.Ascent - gl.Pos.Y - gl.Size.Y/2 - f.Ascent/2) * scale
		centre := pos.Add(rot.Rotate(lin.V3(cx, cy, 0)))
		mat := Material{Texture: tex, BaseColor: c, Unlit: true, DoubleSided: true, Blend: true, NoDepthTest: onTop, NoDepthWrite: onTop,
			UVTransform: lin.Affine{A: gl.UV1.X - gl.UV0.X, C: gl.UV0.X, E: gl.UV1.Y - gl.UV0.Y, F: gl.UV0.Y}}
		g.DrawMesh(g.quadMesh(), mat, lin.TRS(centre, rot, lin.V3(gl.Size.X*scale, gl.Size.Y*scale, 1)))
	}
}
