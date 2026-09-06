package engine

import (
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

// FlyCamera is a free-flying camera for looking around a scene while a
// game is being written: W, A, S and D move, Q and E go down and up,
// Shift goes faster, and the view turns while the right mouse button is
// held (or always, when AlwaysLook is set). Call Update each
// update and hand Camera to the renderer:
//
//	fly := &engine.FlyCamera{Position: lin.V3(0, 5, 10)}
//	// in Update:
//	fly.Update(ctx)
//	// in Draw:
//	ctx.Gfx.SetCamera(fly.Camera())
type FlyCamera struct {
	Position lin.Vec3
	Yaw      float32 // radians about +y; zero looks along -z
	Pitch    float32 // radians, positive looks up
	// Speed is units per second; zero means 10. Fast multiplies it while
	// Shift is held; zero means 4. Sensitivity is radians per view unit
	// of pointer travel; zero means 0.004.
	Speed, Fast, Sensitivity float32
	// FovY, Near and Far pass through to the camera; zero means the
	// camera's defaults.
	FovY, Near, Far float32
	// AlwaysLook turns the view with every pointer movement, not only
	// while the right button is held: set it when the cursor is captured.
	AlwaysLook bool
}

// Update moves and turns the camera from this update's input.
func (f *FlyCamera) Update(ctx *Context) {
	in := ctx.Input
	dt := float32(ctx.Delta)
	sens := f.Sensitivity
	if sens <= 0 {
		sens = 0.004
	}
	if f.AlwaysLook || in.MouseDown(input.MouseRight) {
		dx, dy := in.MouseDelta()
		f.Yaw -= dx * sens
		f.Pitch = lin.Clamp(f.Pitch-dy*sens, -1.55, 1.55)
	}
	speed := f.Speed
	if speed <= 0 {
		speed = 10
	}
	if in.KeyDown(input.KeyLeftShift) || in.KeyDown(input.KeyRightShift) {
		fast := f.Fast
		if fast <= 0 {
			fast = 4
		}
		speed *= fast
	}
	forward, right := f.axes()
	var move lin.Vec3
	if in.KeyDown(input.KeyW) {
		move = move.Add(forward)
	}
	if in.KeyDown(input.KeyS) {
		move = move.Sub(forward)
	}
	if in.KeyDown(input.KeyD) {
		move = move.Add(right)
	}
	if in.KeyDown(input.KeyA) {
		move = move.Sub(right)
	}
	if in.KeyDown(input.KeyE) {
		move = move.Add(lin.V3(0, 1, 0))
	}
	if in.KeyDown(input.KeyQ) {
		move = move.Sub(lin.V3(0, 1, 0))
	}
	if move.Len() > 0 {
		f.Position = f.Position.Add(move.Norm().Mul(speed * dt))
	}
}

// axes returns the camera's forward and right directions.
func (f *FlyCamera) axes() (forward, right lin.Vec3) {
	cy, sy := float32(math.Cos(float64(f.Yaw))), float32(math.Sin(float64(f.Yaw)))
	cp, sp := float32(math.Cos(float64(f.Pitch))), float32(math.Sin(float64(f.Pitch)))
	forward = lin.V3(-sy*cp, sp, -cy*cp)
	right = lin.V3(cy, 0, -sy)
	return forward, right
}

// Forward is the direction the camera looks along.
func (f *FlyCamera) Forward() lin.Vec3 {
	forward, _ := f.axes()
	return forward
}

// LookAt turns the camera towards a point.
func (f *FlyCamera) LookAt(target lin.Vec3) {
	d := target.Sub(f.Position)
	if d.Len() == 0 {
		return
	}
	d = d.Norm()
	f.Yaw = float32(math.Atan2(float64(-d.X), float64(-d.Z)))
	f.Pitch = float32(math.Asin(float64(lin.Clamp(d.Y, -1, 1))))
}

// Camera returns the camera to give the renderer.
func (f *FlyCamera) Camera() gfx.Camera {
	return gfx.Camera{Position: f.Position, Target: f.Position.Add(f.Forward()), FovY: f.FovY, Near: f.Near, Far: f.Far}
}
