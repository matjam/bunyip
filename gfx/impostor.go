package gfx

import (
	"fmt"
	"image"
	"math"

	"github.com/matjam/bunyip/lin"
)

// Impostor is a model baked into an atlas of views around it, drawn as
// one camera-facing quad. A tree, a rock or a building far enough away
// covers a few pixels, and an impostor spends one quad and no vertex
// work on it where the model would spend thousands of triangles. Bake
// one with BakeImpostor, draw it with DrawImpostor, or hand both the
// model and the impostor to DrawModelImpostor and let the distance
// choose. Every impostor of one model shares its atlas, so a forest of
// them is one instanced draw.
//
// The bake fixes the lighting into the atlas, and it is lit the same way
// relative to each view, so an impostor does not turn its shading as the
// sun moves. Keep them far enough away that this does not read, which is
// where they belong anyway.
type Impostor struct {
	// Size is the quad's width and height in world units. BakeImpostor
	// sets it to the model's bounds, and a game may change it.
	Size lin.Vec2
	// Offset moves the quad in its own plane in units of its size, as
	// Billboard.Offset does. BakeImpostor sets it to stand the quad where
	// the model stood on the ground.
	Offset lin.Vec2
	// Distance is the camera distance past which DrawModelImpostor draws
	// the impostor rather than the model. Zero means always the impostor.
	Distance float32
	// Upright turns the quad about the world's up axis only, so it stays
	// vertical when the camera looks down on it. BakeImpostor sets it.
	Upright bool

	tex        *Texture
	views      int
	cols, rows int
	pitch      float32
}

// ImpostorOptions says how BakeImpostor renders a model.
type ImpostorOptions struct {
	// Views is how many directions around the model are baked, evenly
	// spaced; zero means 8. More views turn more smoothly and cost atlas
	// space. The most is MaxImpostorViews.
	Views int
	// Resolution is the pixels across one view; zero means 128. The atlas
	// is the smallest grid of views that holds them all.
	Resolution int
	// Pitch is how far above the model each view looks down, in radians;
	// zero means 15 degrees. Match it to the camera's usual elevation.
	Pitch float32
	// Light lights the bake. Its Background and Environment are ignored,
	// since the atlas must keep the sky out of its transparent parts. The
	// zero value is a sun from over the viewer's left shoulder.
	Light Light
	// Upright is what the drawn quad's Upright becomes; it is on by
	// default, which is what a ring of views around a model wants. Set
	// FaceCamera to turn it off.
	FaceCamera bool
}

// MaxImpostorViews is the most views one impostor may hold.
const MaxImpostorViews = 64

// impostorMaskCut is the mask pass's red channel above which a texel
// counts as covered by the model. The mask is drawn unlit white on
// black with no anti-aliasing, so every texel is far from the middle.
const impostorMaskCut = 40

// Texture returns the baked atlas, for a game that wants to draw the
// views itself or to inspect the bake.
func (im *Impostor) Texture() *Texture {
	if im == nil {
		return nil
	}
	return im.tex
}

// Views is how many directions the atlas holds.
func (im *Impostor) Views() int {
	if im == nil {
		return 0
	}
	return im.views
}

// Destroy frees the atlas.
func (im *Impostor) Destroy() {
	if im == nil || im.tex == nil {
		return
	}
	im.tex.Destroy()
	im.tex = nil
}

// BakeImpostor renders a model from a ring of directions around it into
// one atlas texture, which DrawImpostor then draws as a billboard. Call
// it from Init or Update, never from Draw: it runs a frame of its own to
// render the views and reads them back, so it costs one stall and must
// not be nested inside the game's frame.
//
// The colour of each view comes from the model with its own materials,
// and its shape from a second pass that draws the model unlit and white,
// so the atlas is a hard cutout with no background bleeding into it.
func (g *Graphics) BakeImpostor(m *Model, opts ImpostorOptions) (*Impostor, error) {
	if m == nil || len(m.Parts) == 0 {
		return nil, fmt.Errorf("gfx: an impostor needs a model with parts")
	}
	if g.frame != nil {
		return nil, fmt.Errorf("gfx: BakeImpostor runs a frame of its own, so call it from Init or Update, not Draw")
	}
	views := opts.Views
	if views <= 0 {
		views = 8
	}
	views = min(views, MaxImpostorViews)
	res := opts.Resolution
	if res <= 0 {
		res = 128
	}
	res = min(max(res, 8), 1024)
	pitch := opts.Pitch
	if pitch == 0 {
		pitch = lin.Radians(15)
	}
	light := opts.Light
	if light.Color == (Color{}) && light.Direction == (lin.Vec3{}) {
		light = Light{Direction: lin.V3(-0.45, -0.75, -0.5), Color: Color{1.6, 1.55, 1.45, 1},
			Sky: Sky{Zenith: Color{0.28, 0.34, 0.45, 1}, Ground: Color{0.16, 0.15, 0.13, 1}}}
	}
	light.Background, light.Environment, light.Shadows = false, nil, false

	cols := int(math.Ceil(math.Sqrt(float64(views))))
	rows := (views + cols - 1) / cols
	im := &Impostor{views: views, cols: cols, rows: rows, pitch: pitch, Upright: !opts.FaceCamera}
	centre := m.Min.Add(m.Max).Mul(0.5)
	radius := max(m.Max.Sub(m.Min).Len()*0.5, 1e-4)
	cell := radius * 2.02 // a sliver of padding, so filtering has somewhere to go
	im.Size = lin.V2(cell, cell)
	// The model's own centre sits in the middle of its cell, so the quad
	// must be lifted by however far that centre is above the model's feet.
	im.Offset = lin.V2(0, (centre.Y-m.Min.Y)/cell-0.5)

	colour, mask, err := g.bakeViews(m, im, cell, radius, res, light)
	if err != nil {
		return nil, err
	}
	atlas := image.NewRGBA(image.Rect(0, 0, cols*res, rows*res))
	for i := 0; i+3 < len(atlas.Pix); i += 4 {
		a := uint8(0)
		if mask.Pix[i] >= impostorMaskCut {
			a = 255
		}
		atlas.Pix[i], atlas.Pix[i+1], atlas.Pix[i+2], atlas.Pix[i+3] = colour.Pix[i], colour.Pix[i+1], colour.Pix[i+2], a
	}
	if im.tex, err = g.NewTexture(atlas, TextureOptions{}); err != nil {
		return nil, err
	}
	return im, nil
}

// bakeViews renders the colour and mask atlases in one frame of their
// own and reads both back. The model is drawn once per view, rotated so
// that the view's direction faces the fixed camera and moved into that
// view's cell, which puts every view in one orthographic pass.
func (g *Graphics) bakeViews(m *Model, im *Impostor, cell, radius float32, res int, light Light) (colour, mask *image.RGBA, err error) {
	w, h := im.cols*res, im.rows*res
	colourRT, err := g.NewRenderTexture(w, h)
	if err != nil {
		return nil, nil, err
	}
	defer colourRT.Destroy()
	maskRT, err := g.NewRenderTexture(w, h)
	if err != nil {
		return nil, nil, err
	}
	defer maskRT.Destroy()
	centre := m.Min.Add(m.Max).Mul(0.5)
	dist := radius * 3
	cam := Camera{Position: lin.V3(0, 0, dist), Target: lin.Vec3{}, Up: lin.V3(0, 1, 0),
		Ortho: float32(im.rows) * cell * 0.5, Near: radius, Far: radius * 5}
	models := make([]lin.Mat4, im.views)
	halfW := float32(im.cols) * cell * 0.5
	halfH := float32(im.rows) * cell * 0.5
	for k := range im.views {
		theta := 2 * math.Pi * float64(k) / float64(im.views)
		// Turning the model by the view's inverse rotation leaves the
		// camera where it is: yaw back by theta, then pitch the top
		// towards the camera.
		rot := lin.AxisAngle(lin.V3(1, 0, 0), im.pitch).Mul(lin.AxisAngle(lin.V3(0, 1, 0), -float32(theta)))
		cx, cy := k%im.cols, k/im.cols
		at := lin.V3(-halfW+(float32(cx)+0.5)*cell, halfH-(float32(cy)+0.5)*cell, 0)
		models[k] = lin.TRS(at, rot, lin.V3(1, 1, 1)).Mul(lin.Translate(centre.Neg()))
	}
	// The bake must not inherit the game's grade, bloom or occlusion:
	// bloom would bleed one view into the next.
	post := g.Post()
	defer g.SetPost(post)
	white := Material{BaseColor: White, Unlit: true}

	// A swapchain rebuilt between the acquire and now skips the frame, so
	// the bake is worth one retry before it gives up.
	for attempt := range 2 {
		ok, err := g.begin(Color{})
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			if attempt == 1 {
				return nil, nil, fmt.Errorf("gfx: the swapchain was rebuilt twice while baking an impostor")
			}
			continue
		}
		g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
		g.DrawTo(colourRT, Color{}, func() {
			g.SetCamera(cam)
			g.SetLight(light)
			for _, model := range models {
				g.DrawModel(m, model)
			}
		})
		g.SetPost(PostSettings{Exposure: 4, Saturation: 1, Contrast: 1, NoAntiAlias: true})
		g.DrawTo(maskRT, Color{}, func() {
			g.SetCamera(cam)
			g.SetLight(light)
			for _, model := range models {
				for _, p := range m.Parts {
					g.DrawMesh(p.Mesh, white, model.Mul(p.World))
				}
			}
		})
		if _, err := g.end(false); err != nil {
			return nil, nil, err
		}
		break
	}
	if colour, err = colourRT.Read(); err != nil {
		return nil, nil, err
	}
	if mask, err = maskRT.Read(); err != nil {
		return nil, nil, err
	}
	return colour, mask, nil
}

// view picks the atlas cell nearest a direction from the model to the
// camera, with the model turned by yaw.
func (im *Impostor) view(toCamera lin.Vec3, yaw float32) int {
	theta := float32(math.Atan2(float64(toCamera.X), float64(toCamera.Z))) - yaw
	step := 2 * math.Pi / float32(im.views)
	k := int(math.Round(float64(theta / step)))
	k %= im.views
	if k < 0 {
		k += im.views
	}
	return k
}

// region is the atlas rectangle of one view.
func (im *Impostor) region(k int) (uv0, uv1 lin.Vec2) {
	cx, cy := float32(k%im.cols), float32(k/im.cols)
	return lin.V2(cx/float32(im.cols), cy/float32(im.rows)),
		lin.V2((cx+1)/float32(im.cols), (cy+1)/float32(im.rows))
}

// DrawImpostor draws the baked view nearest the camera as a quad at pos,
// with the model turned by yaw radians about the world's up axis. A zero
// tint means white. The quad is a cutout, so it writes depth and casts
// shadows like the model it stands for.
func (g *Graphics) DrawImpostor(im *Impostor, pos lin.Vec3, yaw float32, tint Color) {
	if im == nil || im.tex == nil {
		return
	}
	q := g.cur
	q.ensureCamera()
	k := im.view(q.camera.Position.Sub(pos), yaw)
	uv0, uv1 := im.region(k)
	g.DrawBillboard(Billboard{
		Texture:  im.tex,
		Region:   Region{Tex: im.tex, UV0: uv0, UV1: uv1},
		Position: pos,
		Size:     im.Size,
		Offset:   im.Offset,
		Color:    tint,
		Upright:  im.Upright,
		Cutout:   true,
	})
}

// DrawModelImpostor draws the model while the camera is inside the
// impostor's Distance and the impostor beyond it, which is a level of
// detail whose far level costs one quad. A nil impostor draws the model
// at any distance, and a nil model draws the impostor at any distance,
// so a game can bake lazily and still call this every frame.
func (g *Graphics) DrawModelImpostor(m *Model, im *Impostor, t Transform) {
	q := g.cur
	q.ensureCamera()
	pos := t.Position
	near := im == nil || im.tex == nil
	if !near && im.Distance > 0 && q.camera.Position.Distance(pos) < im.Distance {
		near = true
	}
	if near {
		if m != nil {
			g.DrawModel(m, t.Matrix())
			return
		}
	}
	if im == nil || im.tex == nil {
		return
	}
	g.DrawImpostor(im, pos, yawOf(t.Rotation), White)
}
