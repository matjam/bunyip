// Command sprites bounces a few hundred textured sprites around the window
// with a coloured grid behind them. The panel in the bottom-right corner is
// the 2D lighting: a brick floor drawn with DrawLit under a moving lamp and
// a fixed blue light, with the crates registered as occluders so the lamp
// throws shadows from them. P runs the frame through the post pass, which
// a 2D game gets with PostSettings.Post2D: bloom, a vignette, a little
// chromatic aberration and film grain. Escape quits; -seconds and -shot
// make it self-verifying.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"os"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

type ball struct {
	pos, vel lin.Vec2
	tint     gfx.Color
	spin     float32
}

type game struct {
	seconds    float64
	shot       string
	fullscreen bool
	capture    bool
	tex        *gfx.Texture
	floor      *gfx.Texture
	floorNorm  *gfx.Texture
	balls      []ball
	post       bool // run the 2D frame through the post pass
	shotTaken  bool
}

// post2D is the grade P turns on. Post2D is what lets a frame with no 3D
// draws in it reach the composite at all; the rest are the settings that
// work without a depth buffer. Saturation and Contrast are 1 because
// their zero would drain the colour out of the frame.
func post2D() gfx.PostSettings {
	return gfx.PostSettings{
		Post2D: true, NoAntiAlias: true, Exposure: 1, Saturation: 1.1, Contrast: 1,
		Bloom: 0.3, BloomThreshold: 0.9, Vignette: 0.3, Aberration: 0.6, Grain: 0.03,
	}
}

// The lit panel: a floor a few bricks across with three crates on it.
const (
	roomX, roomY = 600, 320
	roomW, roomH = 340, 260
	brick        = 64 // the floor texture's size, tiled over the room
)

// crates are the shadow casters, in view units.
var crates = [][4]float32{
	{roomX + 90, roomY + 70, 46, 46},
	{roomX + 200, roomY + 150, 60, 34},
	{roomX + 40, roomY + 180, 34, 50},
}

func (g *game) Init(ctx *bunyip.Context) error {
	if g.fullscreen {
		ctx.SetFullscreen(true)
	}
	if g.capture {
		ctx.SetCursorCaptured(true)
	}
	var err error
	if g.tex, err = ctx.Gfx.NewTexture(discImage(48), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	colour, normal := brickImages(brick)
	if g.floor, err = ctx.Gfx.NewTexture(colour, gfx.TextureOptions{Linear: true, Repeat: true}); err != nil {
		return err
	}
	// A normal map is not colour, so it uploads as data: the shader wants
	// the bytes as they were painted.
	if g.floorNorm, err = ctx.Gfx.NewTexture(normal, gfx.TextureOptions{Linear: true, Repeat: true, Data: true}); err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 300 {
		g.balls = append(g.balls, ball{
			pos:  lin.V2(rng.Float32()*ctx.Width, rng.Float32()*ctx.Height),
			vel:  lin.V2(rng.Float32()*400-200, rng.Float32()*400-200),
			tint: gfx.RGB(uint8(80+rng.IntN(175)), uint8(80+rng.IntN(175)), uint8(80+rng.IntN(175))),
			spin: rng.Float32()*4 - 2,
		})
	}
	return nil
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if ctx.Input.KeyPressed(input.KeyF) {
		ctx.SetFullscreen(!ctx.Fullscreen())
	}
	if ctx.Input.KeyPressed(input.KeyC) {
		ctx.SetCursorCaptured(!ctx.CursorCaptured())
	}
	if ctx.Input.KeyPressed(input.KeyP) {
		g.post = !g.post
	}
	if dx, dy := ctx.Input.MouseDelta(); ctx.CursorCaptured() && (dx != 0 || dy != 0) {
		ctx.Log.Info("sprites: captured mouse", "dx", dx, "dy", dy)
	}
	if pad := ctx.Input.Gamepad(0); pad.Connected && (pad.Pressed(input.ButtonA) || pad.Axis(input.AxisLeftX) != 0) {
		ctx.Log.Info("sprites: gamepad", "name", pad.Name, "leftX", pad.Axis(input.AxisLeftX), "a", pad.Down(input.ButtonA))
	}
	if g.shot != "" && !g.shotTaken && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotTaken = true
	}
	dt := float32(ctx.Delta)
	for i := range g.balls {
		b := &g.balls[i]
		b.pos = b.pos.Add(b.vel.Mul(dt))
		if b.pos.X < 0 || b.pos.X > ctx.Width {
			b.vel.X = -b.vel.X
			b.pos.X = lin.Clamp(b.pos.X, 0, ctx.Width)
		}
		if b.pos.Y < 0 || b.pos.Y > ctx.Height {
			b.vel.Y = -b.vel.Y
			b.pos.Y = lin.Clamp(b.pos.Y, 0, ctx.Height)
		}
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	// Post settings are replaced every frame, like everything else drawn
	// here. With Post2D off the frame goes straight to the screen.
	if g.post {
		ctx.Gfx.SetPost(post2D())
	} else {
		ctx.Gfx.SetPost(gfx.PostSettings{NoAntiAlias: true})
	}
	const cell = 40
	for y := float32(0); y < ctx.Height; y += cell {
		for x := float32(0); x < ctx.Width; x += cell {
			shade := uint8(30 + 12*(int(x/cell+y/cell)%2))
			ctx.Gfx.FillRect(x, y, cell-1, cell-1, gfx.RGB(shade, shade, shade+8))
		}
	}
	mx, my := ctx.Input.Mouse()
	ctx.Gfx.FillRect(float32(mx)-4, float32(my)-4, 8, 8, gfx.RGB(255, 220, 0))
	for _, b := range g.balls {
		ctx.Gfx.Draw(g.tex, gfx.Sprite{
			Pos: b.pos, Size: lin.V2(48, 48), Origin: lin.V2(0.5, 0.5),
			Color: b.tint, Rotation: b.spin * float32(ctx.Time),
		})
	}
	g.drawLitRoom(ctx)
	ctx.Gfx.SetLayer(20)
	ctx.Gfx.Debugf(12, 12, "P: 2D post-processing %s", onOff(g.post))
	return nil
}

// onOff names a toggle for the overlay.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// drawLitRoom draws the lit panel: the lamp circles the room, the crates
// block it, and the floor picks up the light through its normal map. The
// lights and the occluders are set every frame, like the drawing itself.
func (g *game) drawLitRoom(ctx *bunyip.Context) {
	gr := ctx.Gfx
	gr.SetLayer(10) // over the bouncing sprites
	lamp := lin.V2(
		roomX+roomW/2+90*float32(math.Cos(ctx.Time*0.7)),
		roomY+roomH/2+55*float32(math.Sin(ctx.Time*0.9)),
	)
	gr.SetLights2D(gfx.RGB(26, 26, 34),
		gfx.Light2D{Pos: lamp, Height: 26, Radius: 300, Color: gfx.RGB(255, 198, 130), Shadows: true, Softness: 5},
		gfx.Light2D{Pos: lin.V2(roomX+24, roomY+24), Height: 40, Radius: 190, Color: gfx.RGB(90, 130, 255)},
	)
	for _, c := range crates {
		gr.AddOccluder2D(
			lin.V2(c[0], c[1]), lin.V2(c[0]+c[2], c[1]),
			lin.V2(c[0]+c[2], c[1]+c[3]), lin.V2(c[0], c[1]+c[3]),
		)
	}
	gr.FillRect(roomX-8, roomY-8, roomW+16, roomH+16, gfx.RGB(10, 10, 14))
	gr.DrawLit(g.floor, g.floorNorm, gfx.Sprite{
		Pos: lin.V2(roomX, roomY), Size: lin.V2(roomW, roomH),
		UV1: lin.V2(roomW/brick, roomH/brick), // the floor tiles, so the UVs run past 1
	})
	for _, c := range crates {
		gr.FillRect(c[0], c[1], c[2], c[3], gfx.RGB(96, 68, 44))
		gr.StrokeRect(c[0], c[1], c[2], c[3], 2, gfx.RGB(48, 34, 22))
	}
	gr.FillCircle(lamp.X, lamp.Y, 4, gfx.RGB(255, 236, 190))
	gr.SetLayer(0)
}

// discImage draws an anti-aliased disc with a highlight.
func discImage(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			d := math.Sqrt(dx*dx+dy*dy) / r
			a := lin.Clamp(float32((1-d)*r), 0, 1)
			light := 0.6 + 0.4*math.Max(0, 1-math.Hypot(dx+r*0.3, dy+r*0.3)/r)
			v := uint8(255 * light * float64(a))
			img.SetRGBA(x, y, color.RGBA{v, v, v, uint8(255 * a)})
		}
	}
	return img
}

// brickImages draws the floor: a colour texture of bricks with mortar
// between them, and the matching tangent-space normal map, where the
// mortar dips and the bricks are domed. Both tile, so the room repeats
// them rather than stretching one across it.
func brickImages(size int) (*image.RGBA, *image.RGBA) {
	colour := image.NewRGBA(image.Rect(0, 0, size, size))
	normal := image.NewRGBA(image.Rect(0, 0, size, size))
	// height is the surface at a point: flat across a brick, dropping
	// into the mortar around it.
	height := func(x, y int) float64 {
		bx := float64((x%size+size)%size) / float64(size) * 2 // two bricks across
		by := float64((y%size+size)%size) / float64(size) * 4 // four courses down
		row := math.Floor(by)
		bx += 0.5 * math.Mod(row, 2) // every other course is offset half a brick
		u := math.Abs(math.Mod(bx, 1)-0.5) * 2
		v := math.Abs(math.Mod(by, 1)-0.5) * 2
		edge := math.Max(u, v)
		return 1 - smoothstep(0.7, 1, edge)
	}
	for y := range size {
		for x := range size {
			h := height(x, y)
			// The normal comes from the slope, by central differences.
			dx := height(x+1, y) - height(x-1, y)
			dy := height(x, y+1) - height(x, y-1)
			nx, ny, nz := -dx*2, -dy*2, 1.0
			l := math.Sqrt(nx*nx + ny*ny + nz*nz)
			normal.SetRGBA(x, y, color.RGBA{
				R: uint8((nx/l*0.5 + 0.5) * 255), G: uint8((ny/l*0.5 + 0.5) * 255),
				B: uint8((nz/l*0.5 + 0.5) * 255), A: 255,
			})
			shade := 0.55 + 0.45*h
			tint := color.RGBA{R: uint8(190 * shade), G: uint8(150 * shade), B: uint8(130 * shade), A: 255}
			if h < 0.5 {
				tint = color.RGBA{R: uint8(120 * shade), G: uint8(116 * shade), B: uint8(110 * shade), A: 255}
			}
			colour.SetRGBA(x, y, tint)
		}
	}
	return colour, normal
}

func smoothstep(a, b, x float64) float64 {
	t := math.Min(math.Max((x-a)/(b-a), 0), 1)
	return t * t * (3 - 2*t)
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	fullscreen := flag.Bool("fullscreen", false, "start full screen (F toggles)")
	capture := flag.Bool("capture", false, "start with the cursor captured (C toggles)")
	post := flag.Bool("post", false, "start with 2D post-processing on (P toggles)")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip sprites", Width: 960, Height: 600, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, fullscreen: *fullscreen, capture: *capture, post: *post})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sprites:", err)
		os.Exit(1)
	}
}
