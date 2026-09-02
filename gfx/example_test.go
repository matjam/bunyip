package gfx_test

import (
	"image"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// The Graphics value comes from bunyip.Context.Gfx inside a game's Init
// and Draw; these examples show the calls a game makes with it.
var (
	g    *gfx.Graphics
	img  image.Image
	font *gfx.Font
)

func ExampleGraphics_Draw() {
	tex, err := g.NewTexture(img, gfx.TextureOptions{Linear: true})
	if err != nil {
		return
	}
	defer tex.Destroy()
	// A sprite is a rectangle with a texture window and a tint.
	g.Draw(tex, gfx.Sprite{Pos: lin.V2(100, 80), Size: lin.V2(64, 64), UV1: lin.V2(1, 1), Color: gfx.White})
	g.DrawTexture(tex, 200, 80) // at the texture's own size
	g.FillRect(0, 0, 320, 4, gfx.RGB(255, 200, 40))
}

func ExampleGraphics_SetCamera2D() {
	// World-space sprites follow the camera; screen-space ones (HUD) do not.
	cam := gfx.Camera2D{Position: lin.V2(1000, 500), Zoom: 2}
	g.SetCamera2D(cam)
	g.FillRect(990, 490, 20, 20, gfx.White) // drawn at the view centre
	g.ScreenSpace()
	g.DrawText(font, "score 10", 8, 8, gfx.White)
}

func ExampleGraphics_DrawMesh() {
	verts, indices := gfx.CubeMesh()
	cube, err := g.NewMesh(verts, indices)
	if err != nil {
		return
	}
	defer cube.Destroy()
	g.SetCamera(gfx.OrbitCamera(lin.V3(0, 0, 0), 0.6, 0.4, 6))
	g.SetLight(gfx.Light{Direction: lin.V3(-0.4, -1, -0.5), Color: gfx.Color{R: 2, G: 2, B: 1.8, A: 1}, Shadows: true})
	g.DrawMeshAt(cube, gfx.Material{BaseColor: gfx.RGB(200, 80, 60), Roughness: 0.5}, gfx.At(0, 0, 0).Rotated(lin.V3(0, 1, 0), 0.7))
}

func ExampleGraphics_DrawTo() {
	rt, err := g.NewRenderTexture(256, 256)
	if err != nil {
		return
	}
	defer rt.Destroy()
	rt.SetView(256, 256)
	g.DrawTo(rt, gfx.Black, func() {
		g.FillRect(64, 64, 128, 128, gfx.RGB(90, 200, 255))
	})
	g.DrawTexture(rt.Texture(), 16, 16) // the render texture is an ordinary texture now
}

func ExampleGraphics_ScreenRay() {
	verts, indices := gfx.SphereMesh(16, 32)
	sphere, err := g.NewMesh(verts, indices)
	if err != nil {
		return
	}
	defer sphere.Destroy()
	world := lin.Translate(lin.V3(0, 1, -5))
	ray := g.ScreenRay(400, 300) // the pixel under the mouse
	if hit, ok := sphere.Intersect(world, ray); ok {
		_ = hit.Point // where the ray met the surface
	}
}
