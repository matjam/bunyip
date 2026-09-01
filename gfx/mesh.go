package gfx

import (
	"fmt"
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Vertex is the mesh vertex layout: position, normal and one UV set.
type Vertex struct {
	Pos    lin.Vec3
	Normal lin.Vec3
	UV     lin.Vec2
}

const vertexSize = 32

// Mesh is indexed triangle geometry in device memory.
type Mesh struct {
	IndexCount uint32
	Min, Max   lin.Vec3 // axis-aligned bounds in mesh space
	vbuf, ibuf *render.Buffer
}

// NewMesh uploads vertices and triangle indices.
func (g *Graphics) NewMesh(verts []Vertex, indices []uint32) (*Mesh, error) {
	if len(verts) == 0 || len(indices) == 0 || len(indices)%3 != 0 {
		return nil, fmt.Errorf("gfx: mesh needs vertices and a whole number of triangles (got %d vertices, %d indices)", len(verts), len(indices))
	}
	for _, i := range indices {
		if int(i) >= len(verts) {
			return nil, fmt.Errorf("gfx: index %d out of range for %d vertices", i, len(verts))
		}
	}
	m := &Mesh{IndexCount: uint32(len(indices)), Min: verts[0].Pos, Max: verts[0].Pos}
	for _, v := range verts[1:] {
		m.Min = lin.V3(min(m.Min.X, v.Pos.X), min(m.Min.Y, v.Pos.Y), min(m.Min.Z, v.Pos.Z))
		m.Max = lin.V3(max(m.Max.X, v.Pos.X), max(m.Max.Y, v.Pos.Y), max(m.Max.Z, v.Pos.Z))
	}
	var err error
	vdata := unsafe.Slice((*byte)(unsafe.Pointer(&verts[0])), len(verts)*vertexSize)
	if m.vbuf, err = g.R.Device.NewDeviceLocalBuffer(vdata, vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT); err != nil {
		return nil, err
	}
	idata := unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4)
	if m.ibuf, err = g.R.Device.NewDeviceLocalBuffer(idata, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT); err != nil {
		m.vbuf.Destroy()
		return nil, err
	}
	return m, nil
}

// Destroy frees the mesh; it must not be in use by a frame in flight.
func (m *Mesh) Destroy() {
	if m.vbuf != nil {
		_ = m.vbuf.Dev().WaitIdle()
		m.vbuf.Destroy()
		m.ibuf.Destroy()
		m.vbuf, m.ibuf = nil, nil
	}
}

// Material is how a mesh is shaded: an albedo texture (nil for plain
// white) tinted by BaseColor.
type Material struct {
	Texture   *Texture
	BaseColor Color
}

// Camera is a perspective camera looking from Position at Target.
type Camera struct {
	Position lin.Vec3
	Target   lin.Vec3
	Up       lin.Vec3 // zero means +Y
	FovY     float32  // radians; zero means 60 degrees
	Near     float32  // zero means 0.1
	Far      float32  // zero means 1000
}

// ViewProj returns the combined matrix for the given aspect ratio.
func (c Camera) ViewProj(aspect float32) lin.Mat4 {
	up, fov, near, far := c.Up, c.FovY, c.Near, c.Far
	if up == (lin.Vec3{}) {
		up = lin.V3(0, 1, 0)
	}
	if fov == 0 {
		fov = lin.Radians(60)
	}
	if near == 0 {
		near = 0.1
	}
	if far == 0 {
		far = 1000
	}
	return lin.Perspective(fov, aspect, near, far).Mul(lin.LookAt(c.Position, c.Target, up))
}

// Light is one directional light plus ambient.
type Light struct {
	Direction lin.Vec3 // direction the light travels
	Color     Color
	Ambient   Color
}

// frameUniforms mirrors the Frame block in mesh.vert (std140).
type frameUniforms struct {
	viewProj   lin.Mat4
	camPos     lin.Vec4
	lightDir   lin.Vec4
	lightColor lin.Vec4
	ambient    lin.Vec4
}

// meshPush mirrors the PC block in mesh.vert.
type meshPush struct {
	model     lin.Mat4
	baseColor [4]float32
}

type meshDraw struct {
	mesh  *Mesh
	mat   Material
	model lin.Mat4
}
