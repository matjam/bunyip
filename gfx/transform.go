package gfx

import "github.com/matjam/bunyip/lin"

// Transform is a position, rotation and scale in 3D; the zero value with
// Scale set is identity. Use it instead of building matrices by hand.
type Transform struct {
	Position lin.Vec3
	Rotation lin.Quat // zero means no rotation
	Scale    lin.Vec3 // zero means 1
}

// At makes a transform at a position.
func At(x, y, z float32) Transform { return Transform{Position: lin.V3(x, y, z)} }

// Rotated adds a rotation of angle radians about axis.
func (t Transform) Rotated(axis lin.Vec3, angle float32) Transform {
	t.Rotation = lin.AxisAngle(axis, angle).Mul(t.rotation())
	return t
}

// Scaled sets a uniform scale.
func (t Transform) Scaled(s float32) Transform {
	t.Scale = lin.V3(s, s, s)
	return t
}

// Moved returns the transform shifted by d.
func (t Transform) Moved(d lin.Vec3) Transform {
	t.Position = t.Position.Add(d)
	return t
}

func (t Transform) rotation() lin.Quat {
	if t.Rotation == (lin.Quat{}) {
		return lin.QuatIdentity()
	}
	return t.Rotation
}

// Matrix returns the model matrix.
func (t Transform) Matrix() lin.Mat4 {
	s := t.Scale
	if s == (lin.Vec3{}) {
		s = lin.V3(1, 1, 1)
	}
	return lin.TRS(t.Position, t.rotation(), s)
}

// Forward is the local -Z axis in world space.
func (t Transform) Forward() lin.Vec3 { return t.rotation().Rotate(lin.V3(0, 0, -1)) }

// DrawMeshAt draws a mesh at a transform.
func (g *Graphics) DrawMeshAt(m *Mesh, mat Material, t Transform) { g.DrawMesh(m, mat, t.Matrix()) }

// DrawModelAt draws a model at a transform.
func (g *Graphics) DrawModelAt(m *Model, t Transform) { g.DrawModel(m, t.Matrix()) }

// OrbitCamera makes a camera looking at target from yaw and pitch radians
// at a distance, the usual control scheme for strategy and viewer cameras.
func OrbitCamera(target lin.Vec3, yaw, pitch, distance float32) Camera {
	pitch = lin.Clamp(pitch, -1.55, 1.55)
	cp, sp := cos32(pitch), sin32(pitch)
	eye := target.Add(lin.V3(distance*cp*sin32(yaw), distance*sp, distance*cp*cos32(yaw)))
	return Camera{Position: eye, Target: target}
}

// Hex makes an opaque colour from 0xRRGGBB.
func Hex(rgb uint32) Color { return RGB(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb)) }
