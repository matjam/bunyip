package gfx

import (
	"reflect"
	"strings"
	"testing"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
	"golang.org/x/image/font/gofont/goregular"
)

type resourceUseSnapshot struct {
	queue     *drawQueue
	shader    *Shader
	camera    Camera
	hasCamera bool
	light     Light
	post      PostSettings
	counts    [12]int
}

func resourceUseState(g *Graphics) resourceUseSnapshot {
	q := g.cur
	return resourceUseSnapshot{q, q.shader, q.camera, q.hasCam, q.light, g.Post(), [12]int{len(q.draws), len(q.joints), len(q.batches), len(q.decals), len(q.probes), len(q.stream.items), len(q.parts.quads), len(q.parts.flat), len(q.parts.scene), len(g.subFrames), len(g.imageSets), len(g.arena.Bytes())}}
}

func TestForeignResourcesRejectBeforeMutationAndOutputsSurvive(t *testing.T) {
	g := newHeadless(t, 32, 32)
	r, err := g.r.NewOutput(render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface, vk.VkExtent2D{Width: 32, Height: 32}, true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := newGraphics(r)
	if err != nil {
		r.Destroy()
		t.Fatal(err)
	}
	t.Cleanup(func() { other.destroy(); r.Destroy() })
	if r.Device.Handle == g.r.Device.Handle {
		t.Fatal("outputs did not get independent devices")
	}
	tex, err := other.NewBlankTexture(2, 2, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mesh := facingQuad(t, other)
	localMesh := facingQuad(t, g)
	batch := other.NewStaticBatch([]BatchItem{{Mesh: mesh}})
	rt, err := other.NewRenderTexture(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	model, err := other.LoadModel(morphGridDoc(9))
	if err != nil {
		t.Fatal(err)
	}
	player := model.NewAnimPlayer()
	verts, ix := geometryQuad(White)
	geometry, err := g.NewGeometry2D(verts, ix)
	if err != nil {
		t.Fatal(err)
	}
	foreignGeometry, err := other.NewGeometry2D(verts, ix)
	if err != nil {
		t.Fatal(err)
	}
	// The environment can be a descriptor-free ownership sentinel: every
	// attempted use must reject it before touching its absent GPU image.
	env := &Environment{g: other}
	path := new(Path).MoveTo(0, 0).LineTo(10, 0).LineTo(10, 10).Close()
	if _, err := g.NewTerrain(TerrainOptions{Layers: [4]*Texture{tex}}); err == nil {
		t.Fatal("terrain accepted foreign layer")
	}
	if _, err := g.CompilePath(path, PathOptions{Fill: &FillOptions{Texture: tex}}); err == nil {
		t.Fatal("compiled path accepted foreign paint")
	}
	if _, err := g.BakeImpostor(model, ImpostorOptions{}); err == nil {
		t.Fatal("impostor accepted foreign model")
	}
	oldImages := g.spriteShader.images
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func()
	}{
		{"sprite", func() { g.Draw(tex, Sprite{}) }},
		{"texture", func() { g.DrawTexture(tex, 0, 0) }},
		{"triangles", func() { g.DrawTriangles(tex, verts) }},
		{"indexed", func() { g.DrawIndexed(tex, verts, ix) }},
		{"geometry texture", func() { g.DrawGeometry(tex, geometry) }},
		{"geometry", func() { g.DrawGeometry(nil, foreignGeometry) }},
		{"sprite shader", func() { g.SetShader(other.spriteShader) }},
		{"shader image", func() { g.spriteShader.SetImage(0, tex) }},
		{"mesh", func() { g.DrawMesh(mesh, Material{}, lin.Identity()) }},
		{"mesh shader", func() { g.DrawMesh(localMesh, Material{Shader: other.meshes.defaultShader}, lin.Identity()) }},
		{"material texture", func() { g.DrawMesh(localMesh, Material{NormalTexture: tex}, lin.Identity()) }},
		{"skin", func() { g.DrawSkinned(mesh, Material{}, lin.Identity(), []lin.Mat4{lin.Identity()}) }},
		{"batch", func() { g.DrawBatch(batch) }},
		{"batch construction", func() { g.NewStaticBatch([]BatchItem{{Mesh: localMesh}, {Mesh: mesh}}) }},
		{"model", func() { g.DrawModel(model, lin.Identity()) }},
		{"impostor", func() { g.DrawImpostor(&Impostor{tex: tex}, lin.Vec3{}, 0, White) }},
		{"model impostor", func() { g.DrawModelImpostor(model, nil, Transform{}) }},
		{"animated model", func() { g.DrawModelAnimated(model, Transform{}, player) }},
		{"manual model foreign part", func() { g.DrawModel(&Model{Parts: []ModelPart{{Mesh: localMesh}, {Mesh: mesh}}}, lin.Identity()) }},
		{"render target", func() { g.DrawTo(rt, White, func() { t.Error("foreign target callback ran") }) }},
		{"particles", func() { g.DrawParticles(tex, []ParticleQuad{{}}) }},
		{"3d particles", func() { g.DrawParticles3D(tex, []ParticleQuad{{}}, Particles3D{}) }},
		{"decal", func() { g.DrawDecal(tex, lin.Identity(), White) }},
		{"billboard", func() { g.DrawBillboard(Billboard{Texture: tex}) }},
		{"lit sprite", func() { g.DrawLit(nil, tex, Sprite{}) }},
		{"path fill", func() { g.FillPath(path, White, FillOptions{Texture: tex}) }},
		{"path stroke", func() { g.StrokePath(path, White, StrokeOptions{Gradient: &Gradient{tex: tex}}) }},
		{"environment", func() { g.SetLight(Light{Environment: env}) }},
		{"probe", func() { g.AddProbe(&ReflectionProbe{env: env, Radius: 1}) }},
		{"post LUT", func() { g.SetPost(PostSettings{LUT: tex}) }},
		{"post scope", func() { g.ConfigurePost(func(p *PostSettings) { p.LUT = tex }) }},
	}
	// Exercise every texture-valued material field, including fields added
	// later, so a new map cannot bypass the shared ownership check.
	materialType := reflect.TypeFor[Material]()
	for i := range materialType.NumField() {
		if materialType.Field(i).Type != reflect.TypeFor[*Texture]() {
			continue
		}
		var mat Material
		reflect.ValueOf(&mat).Elem().Field(i).Set(reflect.ValueOf(tex))
		tests = append(tests, struct {
			name string
			run  func()
		}{"material " + materialType.Field(i).Name, func() { g.DrawMesh(localMesh, mat, lin.Identity()) }})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before, foreignBefore := resourceUseState(g), resourceUseState(other)
			func() {
				defer func() {
					p := recover()
					if p == nil {
						t.Error("foreign resource accepted")
					} else if !strings.Contains(strings.ToLower(panicMessage(p)), "graphics") {
						t.Errorf("unrelated panic: %v", p)
					}
				}()
				tc.run()
			}()
			if !reflect.DeepEqual(before, resourceUseState(g)) || !reflect.DeepEqual(foreignBefore, resourceUseState(other)) || g.spriteShader.images != oldImages {
				t.Fatal("rejected resource changed queue, uniforms or state")
			}
		})
	}
	// A valid queued draw remains valid when its texture is destroyed before
	// submission; ownership checking must not replace frame retirement rules.
	local, err := g.NewBlankTexture(1, 1, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g.Draw(local, Sprite{Size: lin.V2(1, 1)})
	local.Destroy()
	g.FillRect(0, 0, 32, 32, RGB(255, 0, 0))
	img, err := g.end(true)
	if err != nil {
		t.Fatal(err)
	}
	if c := img.RGBAAt(16, 16); c.R < 200 || c.G > 10 {
		t.Fatalf("recipient failed after rejected resources: %v", c)
	}
	img = frame2D(t, other, func() { other.FillRect(0, 0, 32, 32, RGB(0, 255, 0)) })
	if c := img.RGBAAt(16, 16); c.G < 200 || c.R > 10 {
		t.Fatalf("owner failed after rejected resources: %v", c)
	}
}

func panicMessage(p any) string {
	if e, ok := p.(error); ok {
		return e.Error()
	}
	if s, ok := p.(string); ok {
		return s
	}
	return ""
}

func TestForeignFontDrawConveniencesDoNotShape(t *testing.T) {
	g := newHeadless(t, 32, 32)
	// The actual font belongs to this live Graphics; a second CPU-only
	// recipient must reject it before touching any shaping or atlas state.
	f, err := g.NewFont(goregular.TTF, 14, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	receiver := &Graphics{cur: &drawQueue{}}
	path := new(Path).MoveTo(0, 0).LineTo(30, 0)
	for name, draw := range map[string]func(){
		"text":  func() { receiver.DrawText(f, "new glyphs", 0, 0, White) },
		"block": func() { receiver.DrawTextBlock(f, "new glyphs", 0, 0, TextOptions{}, White) },
		"rich": func() {
			receiver.DrawRichText(RichFonts{Regular: f}, RichText{Runs: []RichRun{{Text: "new glyphs"}}}, 0, 0, TextOptions{}, White)
		},
		"3d":   func() { receiver.DrawText3D(f, "new glyphs", lin.Vec3{}, 1, White, false, TextOptions{}) },
		"path": func() { receiver.DrawTextOnPath(f, "new glyphs", path, 0, TextOptions{}, White) },
	} {
		t.Run(name, func(t *testing.T) {
			receiver.drawErr = nil
			before := len(f.glyphs)
			draw()
			if receiver.drawErr == nil || len(f.glyphs) != before || f.dirty {
				t.Fatal("foreign font was shaped or accepted")
			}
		})
	}
}
