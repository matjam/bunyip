package gfx

import (
	"fmt"
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

var _ = vk.VK_TRUE

// Vertex is a mesh vertex: position, normal, a texture coordinate, an
// optional second set (UV2, for lightmaps and occlusion) and an optional
// colour that multiplies the material's base colour (zero means white).
type Vertex struct {
	Pos    lin.Vec3
	Normal lin.Vec3
	UV     lin.Vec2
	UV2    lin.Vec2
	Color  Color
}

// gpuVertex is the vertex as the GPU reads it: the colour packed to
// four bytes.
type gpuVertex struct {
	pos    lin.Vec3
	normal lin.Vec3
	uv     lin.Vec2
	uv2    lin.Vec2
	color  uint32
}

const vertexSize = 44

func packColor(c Color) uint32 {
	if c == (Color{}) {
		c = White
	}
	to := func(v float32) uint32 { return uint32(lin.Clamp(v, 0, 1)*255 + 0.5) }
	return to(c.R) | to(c.G)<<8 | to(c.B)<<16 | to(c.A)<<24
}

func (v Vertex) gpu() gpuVertex {
	return gpuVertex{pos: v.Pos, normal: v.Normal, uv: v.UV, uv2: v.UV2, color: packColor(v.Color)}
}

// Mesh is indexed triangle geometry in device memory. Build one from
// vertices with NewMesh, from the shapes in this package (CubeMesh,
// SphereMesh, PlaneMesh, HeightfieldMesh and the rest), or by loading a
// glTF Model. Meshes that change, such as voxel chunks and procedural
// terrain, take new geometry through Update.
type Mesh struct {
	IndexCount uint32
	Min, Max   lin.Vec3 // axis-aligned bounds in mesh space
	vbuf, ibuf *render.Buffer
	verts      []Vertex // kept for picking
	indices    []uint32
	skinned    bool
	g          *Graphics
}

// Vertices returns the mesh's vertices as uploaded, for picking and
// physics; the slice is the mesh's own, so do not change it.
func (m *Mesh) Vertices() []Vertex { return m.verts }

// Indices returns the mesh's triangle indices, three per triangle; the
// slice is the mesh's own.
func (m *Mesh) Indices() []uint32 { return m.indices }

// Update replaces the mesh's geometry: a voxel chunk after a block is
// broken, terrain after an edit, a procedural mesh that grows. Draws
// already queued this frame keep the old geometry, which is freed once
// the frame is done, so Update is safe at any point of a frame. Skinned
// meshes cannot be updated.
func (m *Mesh) Update(verts []Vertex, indices []uint32) error {
	if m.skinned {
		return fmt.Errorf("gfx: a skinned mesh cannot be updated")
	}
	if m.vbuf == nil {
		return fmt.Errorf("gfx: update of a destroyed mesh")
	}
	if len(verts) == 0 {
		return fmt.Errorf("gfx: mesh needs vertices")
	}
	packed := make([]gpuVertex, len(verts))
	for i, v := range verts {
		packed[i] = v.gpu()
	}
	fresh, err := m.g.newMesh(verts, indices, unsafe.Slice((*byte)(unsafe.Pointer(&packed[0])), len(packed)*vertexSize))
	if err != nil {
		return err
	}
	m.g.retireBuffers(m.vbuf, m.ibuf)
	m.vbuf, m.ibuf = fresh.vbuf, fresh.ibuf
	m.IndexCount, m.Min, m.Max, m.verts, m.indices = fresh.IndexCount, fresh.Min, fresh.Max, fresh.verts, fresh.indices
	return nil
}

// boundingSphere is the mesh's bounds under a model matrix as a sphere:
// the box's centre moved, its half-diagonal scaled by the matrix's
// largest axis.
func (m *Mesh) boundingSphere(model lin.Mat4) (centre lin.Vec3, radius float32) {
	centre = model.MulPoint(m.Min.Add(m.Max).Mul(0.5))
	scale := float32(0)
	for c := range 3 {
		axis := lin.V3(model.At(0, c), model.At(1, c), model.At(2, c)).Len()
		scale = max(scale, axis)
	}
	return centre, m.Max.Sub(m.Min).Len() * 0.5 * scale
}

// NewMesh uploads vertices and triangle indices.
func (g *Graphics) NewMesh(verts []Vertex, indices []uint32) (*Mesh, error) {
	if len(verts) == 0 {
		return nil, fmt.Errorf("gfx: mesh needs vertices")
	}
	packed := make([]gpuVertex, len(verts))
	for i, v := range verts {
		packed[i] = v.gpu()
	}
	return g.newMesh(verts, indices, unsafe.Slice((*byte)(unsafe.Pointer(&packed[0])), len(packed)*vertexSize))
}

// newMesh uploads the GPU vertex bytes (plain or skinned layout) and keeps
// the plain vertices for picking and bounds.
func (g *Graphics) newMesh(verts []Vertex, indices []uint32, vdata []byte) (*Mesh, error) {
	if len(verts) == 0 || len(indices) == 0 || len(indices)%3 != 0 {
		return nil, fmt.Errorf("gfx: mesh needs vertices and a whole number of triangles (got %d vertices, %d indices)", len(verts), len(indices))
	}
	for _, i := range indices {
		if int(i) >= len(verts) {
			return nil, fmt.Errorf("gfx: index %d out of range for %d vertices", i, len(verts))
		}
	}
	m := &Mesh{IndexCount: uint32(len(indices)), Min: verts[0].Pos, Max: verts[0].Pos, verts: verts, indices: indices, g: g}
	for _, v := range verts[1:] {
		m.Min = lin.V3(min(m.Min.X, v.Pos.X), min(m.Min.Y, v.Pos.Y), min(m.Min.Z, v.Pos.Z))
		m.Max = lin.V3(max(m.Max.X, v.Pos.X), max(m.Max.Y, v.Pos.Y), max(m.Max.Z, v.Pos.Z))
	}
	var err error
	if m.vbuf, err = g.r.Device.NewDeviceLocalBuffer(vdata, vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT); err != nil {
		return nil, err
	}
	idata := unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4)
	if m.ibuf, err = g.r.Device.NewDeviceLocalBuffer(idata, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT); err != nil {
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

// Material is how a mesh is shaded, in the metallic-roughness model. Every
// texture is optional: nil albedo is white, nil metal-rough is the factors
// alone, nil normal map is the geometric normal, nil emissive is black.
type Material struct {
	Texture   *Texture // albedo, sRGB
	BaseColor Color    // multiplies the albedo; zero means white
	Metallic  float32  // 0 dielectric .. 1 metal; with a texture, a factor (0 means 1)
	Roughness float32  // 0.04 .. 1; zero means 0.6

	MetalRoughTexture *Texture // glTF layout: G roughness, B metallic; data, not colour
	NormalTexture     *Texture // tangent-space normal map; data, not colour
	EmissiveTexture   *Texture // sRGB, scaled by Emissive
	Emissive          float32  // glow strength; without a texture the mesh glows in its base colour
	// OcclusionTexture darkens ambient light by its red channel, for baked
	// crevice shadows; OcclusionStrength scales it (zero means 1).
	OcclusionTexture  *Texture
	OcclusionStrength float32

	// AlphaCutoff discards fragments whose alpha is below it, in both the
	// lit and shadow passes: leaves, fences, decals with hard edges. Zero
	// means no cutout.
	AlphaCutoff float32
	Blend       bool // alpha-blended, drawn after opaque geometry back to front
	DoubleSided bool // no back-face culling; back faces are lit with a flipped normal
	Unlit       bool // the base colour and emissive as they are, ignoring lights

	NoDepthTest  bool // draw over everything already drawn: overlays, highlights through walls
	NoDepthWrite bool // leave the depth buffer alone: ghosts, additive effects

	// UVTransform maps texture coordinates before sampling the material's
	// textures: scrolling, tiling, rotation. Zero means identity.
	UVTransform lin.Affine
	// OcclusionUV2 samples the occlusion map with the vertices' second
	// texture coordinates, the lightmap convention.
	OcclusionUV2 bool

	// Clearcoat adds a glossy varnish layer of that strength (0..1) with
	// its own roughness: car paint, lacquer, wet surfaces.
	Clearcoat          float32
	ClearcoatRoughness float32
	// Sheen adds soft back-scattered light at grazing angles in that colour,
	// the look of velvet and cloth; zero means none.
	Sheen          Color
	SheenRoughness float32 // zero means 0.5
	// Subsurface (0..1) lets light through thin parts, tinted by the base
	// colour: leaves, wax, skin. ThicknessTexture (red channel, 1 = thick)
	// shapes it; nil is uniformly thin.
	Subsurface       float32
	ThicknessTexture *Texture

	// Transmission (0..1) is how much light passes through the surface:
	// glass, water, ice. The scene behind shows through, refracted by IOR
	// (zero means 1.5) across Thickness world units of material, blurred
	// by the roughness and tinted by the base colour. AttenuationColor is
	// what white light becomes after AttenuationDistance units inside the
	// volume; a zero distance means no absorption. ThicknessTexture scales
	// Thickness; with a Thickness and no map the mesh is uniformly thick.
	// Transmissive meshes draw after the opaque ones, like Blend.
	Transmission        float32
	IOR                 float32
	Thickness           float32
	AttenuationColor    Color
	AttenuationDistance float32

	// Outline draws a line of that many pixels around the mesh's
	// silhouette in OutlineColor (zero means black): selection rings,
	// cartoon edges. It needs a depth format with stencil, which every
	// desktop GPU has.
	Outline      float32
	OutlineColor Color
	// XRay tints the parts of the mesh hidden behind other geometry, so a
	// unit shows through walls; zero means none.
	XRay Color

	// Shader is a mesh shader from NewMeshShader that adjusts the surface
	// before lighting; nil is the standard material.
	Shader *Shader
}

// Camera looks from Position at Target: perspective with FovY, or
// orthographic when Ortho is set, for isometric and strategy views where
// distance does not shrink things.
type Camera struct {
	Position lin.Vec3
	Target   lin.Vec3
	Up       lin.Vec3 // zero means +Y
	FovY     float32  // radians; zero means 60 degrees
	Near     float32  // zero means 0.1
	Far      float32  // zero means 1000
	// Ortho is half the view's height in world units for an orthographic
	// camera; zero means perspective.
	Ortho float32
}

func (c Camera) defaults() (up lin.Vec3, fov, near, far float32) {
	up, fov, near, far = c.Up, c.FovY, c.Near, c.Far
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
	return
}

// ViewProj returns the combined matrix for the given aspect ratio.
func (c Camera) ViewProj(aspect float32) lin.Mat4 {
	return c.Projection(aspect).Mul(c.viewMatrix())
}

// Projection returns the projection matrix alone.
func (c Camera) Projection(aspect float32) lin.Mat4 {
	_, fov, near, far := c.defaults()
	if c.Ortho > 0 {
		// Ortho puts its bottom at the top of the screen, so a y-up world
		// hands it the top first.
		return lin.Ortho(-c.Ortho*aspect, c.Ortho*aspect, c.Ortho, -c.Ortho, near, far)
	}
	return lin.Perspective(fov, aspect, near, far)
}

func (c Camera) viewMatrix() lin.Mat4 {
	up, _, _, _ := c.defaults()
	return lin.LookAt(c.Position, c.Target, up)
}

// Light is the directional light plus ambient, with optional shadows.
type Light struct {
	Direction lin.Vec3 // direction the light travels
	Color     Color
	Ambient   Color // light from every direction when the Sky leaves a colour unset
	// Sky is the procedural environment: sky and ground colours around an
	// up axis, thinning to space, with a drawn sun and stars.
	Sky Sky

	Shadows        bool    // render cascaded shadow maps for the directional light
	ShadowDistance float32 // how far from the camera shadows reach; default 60
	ShadowStrength float32 // 0..1 how dark shadows are; zero means 1

	// Environment lights the scene from every direction with an image:
	// reflections in metals, tinted ambient on everything. It replaces
	// Ambient and Sky when set.
	Environment *Environment
	// Background draws the environment, or the Sky, behind the scene.
	Background bool
	// Fog fades distant geometry into a colour; the zero value is none.
	Fog Fog
}

// Fog fades geometry into a colour with distance from the camera: the
// cheapest way to give a scene depth and to hide the far plane. Linear
// fog ramps from Start to full at End; exponential fog thickens with
// Density (1 - exp(-(distance * Density)^2)); when both are set the
// denser wins. Height and HeightFalloff make ground fog: full at and
// below Height, thinning above it by exp(-(y - Height) * HeightFalloff),
// along the world's y axis. A zero End and Density means no fog. The
// sky is not fogged, so pick a colour near the horizon's for outdoor
// scenes.
type Fog struct {
	Color         Color
	Start, End    float32
	Density       float32
	Height        float32
	HeightFalloff float32
}

type meshDraw struct {
	mesh      *Mesh
	mat       Material
	model     lin.Mat4
	set       vk.VkDescriptorSet
	shader    *Shader // never nil once queued
	uniform   int32   // arena offset of the shader's uniforms, -1 for none
	depth     float32 // view-space distance for transparency sorting
	culled    bool    // outside the camera's view; drawn only into shadows
	skinned   bool
	jointBase int // first joint matrix in the queue's joint list
}

// meshInstance is the per-instance vertex stream: see pbr.vert.
type meshInstance struct {
	model     [3]lin.Vec4 // the model matrix's three rows (it is affine)
	baseColor [4]float32
	material  [4]float32 // metallic, roughness, emissive, flags (1 normal map, 2 unlit, 4 occlusion on UV2, 8 emissive from the base colour)
	extra     [4]float32 // joint base index, alpha cutoff, occlusion strength, subsurface
	uvT0      [4]float32 // texture transform a, b, c, d
	uvT1      [4]float32 // texture transform e, f; clearcoat, clearcoat roughness
	sheen     [4]float32 // sheen colour, sheen roughness
	volume    [4]float32 // transmission, ior, thickness, attenuation distance
	atten     [4]float32 // attenuation colour
}

const meshInstanceSize = 176

// blended reports whether a material draws after the opaque scene.
func (m Material) blended() bool { return m.Blend || m.Transmission > 0 }
