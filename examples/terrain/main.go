// Command terrain is an outdoor scene of the kind a strategy or survival
// game draws, built on gfx.Terrain: a chunked heightfield with per-chunk
// levels of detail and a splat map blending sand, grass, rock and snow,
// a lake, pine models near and baked impostors of them far, billboard
// trees, rocks at several levels of detail, campfires as point lights, a
// watchtower's searchlight as a spot light, an atmospheric sky and
// valley fog, labels in the world, and terrain the player digs into
// with a click.
// The corner shows the frame's draw, instance and cull counts. Drag to
// orbit, scroll to zoom, click the ground to dig.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/engine"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

const (
	cols, rows = 129, 129 // height samples across and deep
	cell       = 1.0      // world units per sample
	chunk      = 32       // samples across one terrain chunk
	splatSize  = 128      // the splat map's pixels a side
)

type game struct {
	seconds  float64
	shot     string
	shotDone bool

	font    *gfx.Font
	terrain *gfx.Terrain
	layers  [4]*gfx.Texture
	water   *gfx.Mesh
	tower   *gfx.Mesh
	roof    *gfx.Mesh
	ember   *gfx.Mesh
	rocks   *gfx.LOD
	rockAt  []gfx.Transform
	pine    *gfx.Model
	pineFar *gfx.Impostor
	pineAt  []gfx.Transform
	tree    *gfx.Texture
	trees   []lin.Vec3
	fires   []lin.Vec3
	towerAt lin.Vec3

	yaw, pitch, dist float32
	dug              bool
	skipped          int
}

// height is the terrain's shape: rolling hills with a lake basin in the
// middle and a ridge to the north.
func height(x, z float32) float32 {
	h := 3*float32(math.Sin(float64(x)*0.08)*math.Cos(float64(z)*0.06)) +
		1.5*float32(math.Sin(float64(x)*0.21+1.3)*math.Sin(float64(z)*0.17)) +
		0.4*float32(math.Sin(float64(x)*0.9)*math.Cos(float64(z)*0.7))
	r := float32(math.Hypot(float64(x)+8, float64(z)-6))
	h -= 4 * float32(math.Exp(-float64(r*r)/300))
	h += 3 * float32(math.Exp(-float64((z+30)*(z+30))/200))
	return h + 1
}

// heights fills the sample grid from height, centred on the origin.
func heights() []float32 {
	h := make([]float32, cols*rows)
	for z := range rows {
		for x := range cols {
			h[z*cols+x] = height(float32(x-cols/2)*cell, float32(z-rows/2)*cell)
		}
	}
	return h
}

// splatImage weights the four ground layers by height and slope: sand by
// the water, grass on gentle land, rock on steep faces and snow on the
// peaks. Each pixel's channels are the weights of layers one to four, so
// the terrain shader blends them where they meet.
func (g *game) splatImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, splatSize, splatSize))
	span := float32(cols-1) * cell
	for py := range splatSize {
		for px := range splatSize {
			x := (float32(px)/(splatSize-1) - 0.5) * span
			z := (float32(py)/(splatSize-1) - 0.5) * span
			h := g.terrain.Height(x, z)
			slope := 1 - g.terrain.Normal(x, z).Y
			sand := clamp01(1.5 - h)
			snow := clamp01((h - 4.5) * 0.5)
			rock := clamp01((slope - 0.18) * 6)
			grass := clamp01(1 - sand - snow - rock)
			img.SetRGBA(px, py, color.RGBA{scale8(sand), scale8(grass), scale8(rock), scale8(snow)})
		}
	}
	return img
}

func clamp01(v float32) float32 { return lin.Clamp(v, 0, 1) }
func scale8(v float32) uint8    { return uint8(clamp01(v)*255 + 0.5) }

// groundLayer makes one tiling ground texture: a flat colour with a
// little per-pixel grain, so the layers read as different materials
// without any art.
func groundLayer(base color.RGBA, grain int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	r := rand.New(rand.NewSource(int64(base.R) + 31*int64(base.G)))
	for y := range 32 {
		for x := range 32 {
			d := r.Intn(2*grain+1) - grain
			img.SetRGBA(x, y, color.RGBA{shade(base.R, d), shade(base.G, d), shade(base.B, d), 255})
		}
	}
	return img
}

func shade(v uint8, d int) uint8 { return uint8(min(max(int(v)+d, 0), 255)) }

// treeImage draws a cutout tree: a green canopy over a brown trunk.
func treeImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 64, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 64; x++ {
			var c color.RGBA
			switch {
			case y >= 62 && x >= 28 && x < 36:
				c = color.RGBA{92, 64, 40, 255}
			case y < 70 && math.Abs(float64(x)-32) < float64(y)*0.42+2:
				shade := uint8(90 + (x%7)*8)
				c = color.RGBA{30, shade, 40, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// sunlight is the scene's directional light, sky and fog, for a camera at
// that altitude. The sky is scattered rather than painted: an Atmosphere
// replaces the Zenith and Horizon colours, so the low sun leaves the
// horizon orange and the sky overhead blue, and the same model tints the
// far hills with the air in front of them. Height is how deep the air is
// in this world's units. Fog is left to the valley: the air handles
// distance.
//
// The impostor bake takes the same light, so a pine baked into the atlas
// is lit the way the pine models beside it are; BakeImpostor drops the
// background, the shadows and the fog, which the frame applies again.
func sunlight(altitude float32) gfx.Light {
	mist := gfx.Color{R: 0.78, G: 0.75, B: 0.68, A: 1}
	return gfx.Light{
		Direction:      lin.V3(-0.6, -0.3, -0.4),
		Color:          gfx.Color{R: 1, G: 0.95, B: 0.85, A: 1},
		Sky:            gfx.Sky{Ground: gfx.Color{R: 0.3, G: 0.32, B: 0.25, A: 1}, Atmosphere: gfx.Atmosphere{Height: 3000, Altitude: max(altitude, 0)}},
		Shadows:        true,
		ShadowDistance: 90,
		Background:     true,
		Fog:            gfx.Fog{Color: mist, Start: 45, End: 200, Height: 0.8, HeightFalloff: 0.4},
	}
}

// pineDocument builds a pine as a glTF document in memory: a brown trunk
// and three green skirts of foliage, each a cone. A file loads the same
// way through gltf.Load; this keeps the example to one program.
func pineDocument() *gltf.Document {
	doc := &gltf.Document{
		Materials: []gltf.Material{
			{Name: "bark", BaseColor: [4]float32{0.22, 0.14, 0.08, 1}, Roughness: 1, Image: -1, MetalRoughImage: -1, NormalImage: -1, EmissiveImage: -1, OcclusionImage: -1, TransmissionImage: -1, ThicknessImage: -1, UVScale: [2]float32{1, 1}},
			{Name: "needles", BaseColor: [4]float32{0.07, 0.24, 0.09, 1}, Roughness: 1, Image: -1, MetalRoughImage: -1, NormalImage: -1, EmissiveImage: -1, OcclusionImage: -1, TransmissionImage: -1, ThicknessImage: -1, UVScale: [2]float32{1, 1}},
		},
	}
	part := func(verts []gfx.Vertex, idx []uint32, material int, m lin.Mat4) gltf.Primitive {
		p := gltf.Primitive{Indices: idx, Material: material}
		nm := m.NormalMatrix()
		for _, v := range verts {
			p.Positions = append(p.Positions, m.MulPoint(v.Pos))
			p.Normals = append(p.Normals, nm.MulVec(v.Normal).Norm())
			p.UVs = append(p.UVs, v.UV)
		}
		return p
	}
	cv, ci := gfx.CylinderMesh(8)
	kv, ki := gfx.ConeMesh(10)
	mesh := gltf.Mesh{Name: "pine", Primitives: []gltf.Primitive{
		part(cv, ci, 0, lin.Translate(lin.V3(0, 1.1, 0)).Mul(lin.Scale(lin.V3(0.16, 1.1, 0.16)))),
	}}
	for i, y := range []float32{1.6, 2.6, 3.5} {
		s := 1.3 - float32(i)*0.35
		mesh.Primitives = append(mesh.Primitives, part(kv, ki, 1, lin.Translate(lin.V3(0, y, 0)).Mul(lin.Scale(lin.V3(s, 1.1, s)))))
	}
	doc.Meshes = []gltf.Mesh{mesh}
	doc.Nodes = []gltf.Node{{Name: "pine", Parent: -1, Rotation: lin.QuatIdentity(), Scale: lin.V3(1, 1, 1), Mesh: 0, Skin: -1}}
	doc.Instances = []gltf.Instance{{Name: "pine", Mesh: 0, Node: 0, Skin: -1, World: lin.Identity()}}
	return doc
}

func (g *game) Init(ctx *engine.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 28, gfx.FontOptions{}); err != nil {
		return err
	}
	for i, c := range [4]color.RGBA{{194, 178, 128, 255}, {86, 125, 50, 255}, {110, 105, 100, 255}, {235, 240, 245, 255}} {
		if g.layers[i], err = ctx.Gfx.NewTexture(groundLayer(c, 14), gfx.TextureOptions{Repeat: true}); err != nil {
			return err
		}
	}
	// The terrain owns the heightfield, the chunk meshes at four
	// resolutions each, the splat texture and the shader that blends the
	// layers. It is built once with a flat splat, then given the real one
	// through its shader, because the weights are computed from the
	// terrain's own height and slope queries.
	if g.terrain, err = ctx.Gfx.NewTerrain(gfx.TerrainOptions{
		Heights: heights(), Cols: cols, Rows: rows, Cell: cell, ChunkSize: chunk,
		Levels: 4, LODDistance: 45,
		Layers: g.layers, LayerScale: [4]float32{6, 5, 4, 7},
		LayerRoughness: [4]float32{0.95, 0.9, 0.85, 0.75},
	}); err != nil {
		return err
	}
	if err := g.terrain.SetSplat(g.splatImage()); err != nil {
		return err
	}
	pv, pi := gfx.PlaneMesh(1)
	if g.water, err = ctx.Gfx.NewMesh(pv, pi); err != nil {
		return err
	}
	cv, ci := gfx.CylinderMesh(16)
	if g.tower, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	kv, ki := gfx.ConeMesh(16)
	if g.roof, err = ctx.Gfx.NewMesh(kv, ki); err != nil {
		return err
	}
	sv, si := gfx.SphereMesh(8, 12)
	if g.ember, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	// Rocks: a fine sphere near, a coarse one far, nothing beyond.
	fine, fineIdx := gfx.SphereMesh(16, 32)
	coarse, coarseIdx := gfx.FlatShaded(gfx.SphereMesh(5, 8))
	fineMesh, err := ctx.Gfx.NewMesh(fine, fineIdx)
	if err != nil {
		return err
	}
	coarseMesh, err := ctx.Gfx.NewMesh(coarse, coarseIdx)
	if err != nil {
		return err
	}
	g.rocks = gfx.NewLOD([]*gfx.Mesh{fineMesh, coarseMesh, nil}, []float32{25, 70})
	if g.tree, err = ctx.Gfx.NewTexture(treeImage(), gfx.TextureOptions{}); err != nil {
		return err
	}
	// The pines are a model up close and a baked impostor beyond thirty
	// units: twelve views around the tree in one atlas, so the far half
	// of the wood costs one quad each and one instanced draw between them.
	if g.pine, err = ctx.Gfx.LoadModel(pineDocument()); err != nil {
		return err
	}
	if g.pineFar, err = ctx.Gfx.BakeImpostor(g.pine, gfx.ImpostorOptions{Views: 12, Resolution: 96, Pitch: lin.Radians(20), Light: sunlight(20)}); err != nil {
		return err
	}
	g.pineFar.Distance = 30
	g.scatter()
	for _, p := range [][2]float32{{-30, 20}, {25, -12}, {12, 30}, {-25, -25}} {
		g.fires = append(g.fires, lin.V3(p[0], g.terrain.Height(p[0], p[1])+0.3, p[1]))
	}
	g.towerAt = lin.V3(0, g.terrain.Height(0, -30), -30)
	g.yaw, g.pitch, g.dist = 0.6, 0.42, 48
	ctx.Gfx.SetPost(gfx.PostSettings{Exposure: 1, Saturation: 1.05, Contrast: 1, Bloom: 0.15})
	return nil
}

// scatter places the trees, pines and rocks on gentle land above the
// water, asking the terrain where the ground is and which way it faces.
func (g *game) scatter() {
	r := rand.New(rand.NewSource(7))
	gentle := func(x, z float32) (float32, bool) {
		h := g.terrain.Height(x, z)
		return h, h > 0.8 && h < 5.5 && g.terrain.Normal(x, z).Y > 0.9
	}
	for len(g.trees) < 400 {
		x, z := (r.Float32()-0.5)*110, (r.Float32()-0.5)*110
		if h, ok := gentle(x, z); ok {
			g.trees = append(g.trees, lin.V3(x, h-0.1, z))
		}
	}
	for len(g.pineAt) < 120 {
		x, z := (r.Float32()-0.5)*110, (r.Float32()-0.5)*110
		if h, ok := gentle(x, z); ok {
			s := 0.8 + r.Float32()*0.6
			g.pineAt = append(g.pineAt, gfx.Transform{Position: lin.V3(x, h, z), Scale: lin.V3(s, s, s)}.
				Rotated(lin.V3(0, 1, 0), r.Float32()*6.28))
		}
	}
	for len(g.rockAt) < 80 {
		x, z := (r.Float32()-0.5)*112, (r.Float32()-0.5)*112
		if h := g.terrain.Height(x, z); h > 0.3 {
			s := 0.4 + r.Float32()*1.2
			t := gfx.Transform{Position: lin.V3(x, h-s*0.3, z), Scale: lin.V3(s, s*0.6, s*(0.7+r.Float32()*0.6))}
			g.rockAt = append(g.rockAt, t.Rotated(lin.V3(0, 1, 0), r.Float32()*6.28))
		}
	}
}

func (g *game) Shutdown(ctx *engine.Context) {
	g.font.Destroy()
	g.terrain.Destroy()
	g.pineFar.Destroy()
	g.pine.Destroy()
	for _, m := range []*gfx.Mesh{g.water, g.tower, g.roof, g.ember} {
		m.Destroy()
	}
	for _, l := range g.rocks.Levels {
		if l.Mesh != nil {
			l.Mesh.Destroy()
		}
	}
	for _, t := range g.layers {
		t.Destroy()
	}
	g.tree.Destroy()
}

// dig lowers the terrain around a point and rebuilds the chunks it
// touched. Terrain.Heights is the terrain's own sample grid, so the edit
// is a write into it followed by Update over the samples that changed.
func (g *game) dig(at lin.Vec3, radius, depth float32) error {
	h := g.terrain.Heights()
	minX, minZ, maxX, maxZ := cols, rows, 0, 0
	for z := range rows {
		for x := range cols {
			wx, wz := float32(x-cols/2)*cell, float32(z-rows/2)*cell
			d := float32(math.Hypot(float64(wx-at.X), float64(wz-at.Z)))
			if d >= radius {
				continue
			}
			t := 1 - d/radius
			h[z*cols+x] -= depth * t * t
			minX, minZ = min(minX, x), min(minZ, z)
			maxX, maxZ = max(maxX, x), max(maxZ, z)
		}
	}
	if minX > maxX {
		return nil
	}
	return g.terrain.Update(minX, minZ, maxX, maxZ)
}

func (g *game) Update(ctx *engine.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if in.MouseDown(input.MouseRight) {
		dx, dy := in.MouseDelta()
		g.yaw -= dx * 0.005
		g.pitch = lin.Clamp(g.pitch-dy*0.005, 0.1, 1.4)
	} else {
		g.yaw += float32(ctx.Delta) * 0.05
	}
	if _, dy := in.Scroll(); dy != 0 {
		g.dist = lin.Clamp(g.dist-dy*2, 10, 120)
	}
	if in.MousePressed(input.MouseLeft) {
		mx, my := in.Mouse()
		vw, vh := ctx.Gfx.View()
		ray := gfx.OrbitCamera(lin.V3(0, 2, 0), g.yaw, g.pitch, g.dist).ScreenRay(mx, my, vw, vh)
		if hit, ok := g.terrain.Raycast(ray, 0); ok {
			if err := g.dig(hit, 5, 2); err != nil {
				return err
			}
		}
	}
	if !g.dug && ctx.Time >= 1 {
		g.dug = true
		if err := g.dig(lin.V3(18, 0, 8), 7, 3); err != nil {
			return err
		}
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	return nil
}

func (g *game) Draw(ctx *engine.Context) error {
	gr := ctx.Gfx
	t := float32(ctx.Time)
	cam := gfx.OrbitCamera(lin.V3(0, 2, 0), g.yaw, g.pitch, g.dist)
	gr.SetCamera(cam)
	gr.SetLight(sunlight(cam.Position.Y))
	// Campfires flicker; the tower's searchlight sweeps.
	for i, f := range g.fires {
		flick := 0.8 + 0.2*float32(math.Sin(float64(t)*9+float64(i)))
		gr.AddPointLight(f.Add(lin.V3(0, 0.6, 0)), gfx.Color{R: 4 * flick, G: 2.2 * flick, B: 0.8 * flick, A: 1}, 12)
		gr.DrawMeshAt(g.ember, gfx.Material{BaseColor: gfx.Color{R: 1, G: 0.5, B: 0.15, A: 1}, Emissive: 3 * flick}, gfx.Transform{Position: f, Scale: lin.V3(0.35, 0.25, 0.35)})
	}
	beam := lin.V3(float32(math.Cos(float64(t)*0.6)), -0.45, float32(math.Sin(float64(t)*0.6)))
	top := g.towerAt.Add(lin.V3(0, 6.2, 0))
	gr.AddSpotLight(top, beam, gfx.Color{R: 9, G: 8.5, B: 6, A: 1}, 60, lin.Radians(14), lin.Radians(28))

	// The terrain queues one draw per chunk, each at the resolution its
	// distance deserves; the frustum culls the chunks behind the camera.
	gr.DrawTerrain(g.terrain)
	gr.DrawMesh(g.water, gfx.Material{BaseColor: gfx.Color{R: 0.1, G: 0.32, B: 0.6, A: 0.65}, Blend: true, Roughness: 0.12}, lin.Translate(lin.V3(0, 0, 0)).Mul(lin.Scale(lin.V3(200, 1, 200))))
	gr.DrawMeshAt(g.tower, gfx.Material{BaseColor: gfx.RGB(120, 100, 80), Roughness: 0.8}, gfx.Transform{Position: g.towerAt.Add(lin.V3(0, 3, 0)), Scale: lin.V3(0.9, 3, 0.9)})
	gr.DrawMeshAt(g.roof, gfx.Material{BaseColor: gfx.RGB(150, 50, 40), Roughness: 0.7}, gfx.Transform{Position: g.towerAt.Add(lin.V3(0, 7, 0)), Scale: lin.V3(1.5, 1, 1.5)})
	gr.DrawText3D(g.font, "Watchtower", g.towerAt.Add(lin.V3(0, 9, 0)), 0.05, gfx.White, false, gfx.TextOptions{})
	gr.DrawText3D(g.font, "Lake", lin.V3(-8, 1.2, 6), 0.06, gfx.Color{R: 0.8, G: 0.9, B: 1, A: 1}, false, gfx.TextOptions{})

	rock := gfx.Material{BaseColor: gfx.RGB(115, 110, 105), Roughness: 0.9}
	for _, at := range g.rockAt {
		gr.DrawLODAt(g.rocks, rock, at)
	}
	// Each pine is a model within thirty units and its baked impostor
	// beyond, chosen per tree by DrawModelImpostor.
	for _, at := range g.pineAt {
		gr.DrawModelImpostor(g.pine, g.pineFar, at)
	}
	// Billboard trees the camera cannot see are not even queued.
	fr := gr.Frustum()
	g.skipped = 0
	for _, p := range g.trees {
		if !fr.ContainsSphere(p.Add(lin.V3(0, 1.5, 0)), 2) {
			g.skipped++
			continue
		}
		gr.DrawBillboard(gfx.Billboard{Texture: g.tree, Position: p, Size: lin.V2(2, 3), Offset: lin.V2(0, 0.5), Upright: true, Lit: true, Cutout: true})
	}
	// A scout camera's view volume, drawn as lines.
	scout := gfx.Camera{Position: lin.V3(30, 10, 25), Target: lin.V3(10, 0, 5), FovY: lin.Radians(40), Far: 30}
	gr.DrawWireFrustum(scout, 16.0/9, gfx.Color{R: 1, G: 0.9, B: 0.2, A: 1})
	gr.DebugText3D(scout.Position, "scout")

	s := gr.Stats()
	gr.DebugText(10, 10, fmt.Sprintf("draws %d  instances %d  culled %d  trees skipped %d  near chunk level %d",
		s.Draws3D, s.Instances, s.Culled, g.skipped, g.terrain.ChunkLevel(g.terrain.Chunks()/2)))
	gr.DebugText(10, 28, "right-drag orbits, scroll zooms, click digs")
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := engine.Run(engine.Config{Title: "Bunyip terrain", Width: 1024, Height: 640, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
