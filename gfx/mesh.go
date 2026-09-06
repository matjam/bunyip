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
	IndexCount  uint32
	Min, Max    lin.Vec3 // axis-aligned bounds in mesh space
	vbuf, ibuf  *render.Buffer
	verts       []Vertex // kept for picking
	indices     []uint32
	skinned     bool
	destroyed   bool // Destroy was called; the buffers live until the frame retires them
	boundsFixed bool // Min and Max came from SetBounds, not from the vertices
	// jointMin and jointMax are one box per joint of a skinned mesh, over
	// the vertices weighted to it, so a pose's bounds are their union
	// under its joint matrices.
	jointMin, jointMax []lin.Vec3
	g                  *Graphics
}

// Vertices returns the mesh's vertices as uploaded, for picking and
// physics; the slice is the mesh's own, so do not change it.
func (m *Mesh) Vertices() []Vertex { return m.verts }

// Indices returns the mesh's triangle indices, three per triangle; the
// slice is the mesh's own and must not be modified. Copy it to retain
// a snapshot across Update.
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
	if m.vbuf == nil || m.destroyed {
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
	m.retire()
	m.vbuf, m.ibuf = fresh.vbuf, fresh.ibuf
	m.IndexCount, m.verts, m.indices = fresh.IndexCount, fresh.verts, fresh.indices
	if !m.boundsFixed {
		m.Min, m.Max = fresh.Min, fresh.Max
	}
	m.g.trackMesh(m, vertexSize)
	return nil
}

// retire hands the mesh's current buffers to the frame slot's retire
// list, so draws already queued keep drawing them. The mesh keeps
// pointing at them until then; Update overwrites the fields with the
// fresh buffers, and Destroy leaves them for the retire to clear.
func (m *Mesh) retire() {
	vbuf, ibuf := m.vbuf, m.ibuf
	m.g.deferDestroy(func() {
		vbuf.Destroy()
		ibuf.Destroy()
		if m.vbuf == vbuf {
			m.vbuf, m.ibuf = nil, nil
		}
	})
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
	m, err := g.newMesh(verts, indices, unsafe.Slice((*byte)(unsafe.Pointer(&packed[0])), len(packed)*vertexSize))
	if err == nil {
		g.trackMesh(m, vertexSize)
	}
	return m, err
}

// trackMesh records a mesh in the live resource list, with the size one
// of its vertices takes on the GPU.
func (g *Graphics) trackMesh(m *Mesh, stride int) {
	g.track(m, Resource{Kind: ResourceMesh, Vertices: len(m.verts), Indices: len(m.indices),
		Bytes: len(m.verts)*stride + len(m.indices)*4})
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
	if m.vbuf, err = g.uploadGeometry(vdata, vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT); err != nil {
		return nil, err
	}
	idata := unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4)
	if m.ibuf, err = g.uploadGeometry(idata, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT); err != nil {
		m.vbuf.Destroy()
		return nil, err
	}
	return m, nil
}

// uploadGeometry puts vertex or index bytes in device-local memory.
// Inside a frame the copy is recorded into the frame's command buffer
// from the staging arena, with a barrier so a draw later in the same
// frame reads the new data; outside one it goes through a one-shot
// submission that waits.
func (g *Graphics) uploadGeometry(data []byte, usage vk.VkBufferUsageFlags) (*render.Buffer, error) {
	if g.frame == nil {
		return g.r.Device.NewDeviceLocalBuffer(data, usage)
	}
	buf, err := g.r.Device.NewDeviceBuffer(vk.VkDeviceSize(len(data)), usage)
	if err != nil {
		return nil, err
	}
	staging, offset, err := g.stage(data)
	if err != nil {
		buf.Destroy()
		return nil, err
	}
	render.RecordBufferUpload(g.frame.CB, buf, staging, offset, vk.VkDeviceSize(len(data)))
	return buf, nil
}

// Destroy frees the mesh. Called inside a frame it costs no wait: the
// buffers go on the frame slot's retire list and are freed once that
// frame has finished, so draws already queued this frame still draw.
func (m *Mesh) Destroy() {
	if m.vbuf == nil || m.destroyed {
		return
	}
	m.destroyed = true
	m.g.forget(m)
	m.g.owned.remove(m)
	m.retire()
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
	// Blend draws after opaque geometry, back to front or through the
	// order-independent transparency pass. BaseColor alpha, multiplied by
	// texture and vertex alpha, fades the entire shaded surface, including
	// lighting, emissive and fog. Pass straight colors; the engine
	// premultiplies the result before blending. Without Blend or
	// Transmission, alpha only controls AlphaCutoff.
	Blend       bool
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
	// TransmissionTexture scales Transmission by its red channel, so a
	// window frame can be opaque and its panes glass in one material;
	// nil is the factor everywhere. Data, not colour.
	TransmissionTexture *Texture

	// Specular scales a dielectric's reflection and SpecularColor tints
	// it, the KHR_materials_specular extension: zero means 1 and white,
	// the plain material. A small Specular such as 0.01 all but removes
	// the reflection, for chalk and unglazed clay. SpecularTexture
	// carries the tint in its RGB and the strength in its alpha. Metals
	// keep their own reflection, which is their base colour.
	Specular        float32
	SpecularColor   Color
	SpecularTexture *Texture

	// Iridescence (0..1) puts a thin film over the surface, whose
	// interference shifts the reflection's hue with the viewing angle:
	// soap bubbles, oil on water, beetle shells, tempered steel.
	// IridescenceIOR is the film's index of refraction (zero means 1.3)
	// and IridescenceThickness how thick it is in nanometres (zero means
	// 400, and 100 to 800 is the range that shows colour).
	// IridescenceTexture scales the strength by its red channel and mixes
	// the thickness from IridescenceThicknessMin to IridescenceThickness
	// by its green channel, the two maps glTF packs into one image.
	Iridescence             float32
	IridescenceIOR          float32
	IridescenceThickness    float32
	IridescenceThicknessMin float32
	IridescenceTexture      *Texture

	// Anisotropy (-1..1) stretches the specular highlight along the
	// surface rather than leaving it round: brushed metal, hair, satin,
	// vinyl records. AnisotropyRotation turns the direction it stretches
	// in, in radians, and AnisotropyTexture holds a direction of its own
	// in red and green (around a half, as glTF stores it) and a strength
	// in blue. The direction comes from the mesh's texture coordinates,
	// so an anisotropic mesh needs UVs but no tangents of its own.
	Anisotropy         float32
	AnisotropyRotation float32
	AnisotropyTexture  *Texture

	// Shells draws the mesh that many more times, each a little further
	// out along its normals, for fur, grass, moss and hair; zero means
	// none and eight to twenty-four look like fur. ShellLength is how far
	// the outermost shell stands off in world units (zero means 0.05).
	// FurTexture is the strand mask: a shell keeps a fragment where the
	// map's red channel is above that shell's height, so a tiled noise
	// image gives strands, and the material's UVTransform tiles it.
	// Without a map the shells are solid and only fade outwards. Shells
	// draw after the opaque scene, leave the depth buffer alone and cast
	// no shadow, and each one costs an instance of the mesh.
	Shells      int
	ShellLength float32
	FurTexture  *Texture

	// Stencil masks the material against the stencil buffer: it draws only
	// where the value already there compares to StencilRef the way the
	// test says. StencilAlways, the zero value, draws everywhere.
	// StencilWrite is what a drawn fragment stores, StencilKeep leaving
	// the buffer alone. One material marks a shape with StencilReplace
	// and another draws only inside it with StencilEqual: portals,
	// cutaways, magic windows. The buffer starts each frame at zero, and
	// materials that write it draw before those that do not, whatever
	// order they were queued in. A material with an Outline uses the
	// stencil buffer for the outline itself and ignores these three.
	Stencil      StencilTest
	StencilRef   uint8
	StencilWrite StencilOp

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
	mesh  *Mesh
	mat   Material
	model lin.Mat4
	// prev is the model matrix the draw had last frame and moved says the
	// game supplied one. The velocity pass draws only what moved.
	prev  lin.Mat4
	moved bool
	set   vk.VkDescriptorSet
	// samplers packs the sampler index of each of the material set's
	// eleven texture slots, two bits apiece, for the instance stream.
	samplers float32
	shader   *Shader // never nil once queued
	uniform  int32   // arena offset of the shader's uniforms, -1 for none
	depth    float32 // view-space distance for transparency sorting
	blended  bool    // mat.blended(), resolved once by prepareDraws for the sort
	oit      bool    // blended and drawn in the order-independent pass
	culled   bool    // outside the camera's view; drawn only into shadows
	skinned  bool
	// shell is 0 for the mesh itself and rises to 1 on the outermost fur
	// shell, which stands ShellLength world units off the surface.
	shell      float32
	jointBase  int // first joint matrix in the queue's joint list
	jointCount int // how many joint matrices the draw's pose uses
	// morph captures the targets and weights when queued, empty for a
	// processor blend; morphSet is its model's delta buffer, or zero for
	// the empty set the mesh pass keeps.
	morph    morphDraw
	morphSet vk.VkDescriptorSet
	// centre and radius are the draw's world bounding sphere, resolved by
	// prepareDraws; cullable is false for a shader that may move a vertex
	// anywhere. The shadow pass tests the sphere against each light.
	centre   lin.Vec3
	radius   float32
	cullable bool
	// probe is the reflection probe whose cube map the draw's material set
	// binds, as an index into the queue's probes plus one; zero means the
	// frame's own environment.
	probe int
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
	// atten is the attenuation colour, with the material set's packed
	// sampler indices in w: two bits per texture slot, in the order the
	// set binds them. The shader selects among four statically bound
	// samplers using these choices.
	atten [4]float32
	// gi is what global illumination the draw uses: x the reflection
	// probe's index plus one (0 for the frame's own environment), y 1 for
	// an opaque draw, whose alpha channel carries the screen-space
	// reflection weight instead of a coverage.
	gi   [4]float32
	spec [4]float32 // specular colour, specular strength
	irid [4]float32 // iridescence strength, film ior, thickness minimum and maximum in nm
	fur  [4]float32 // anisotropy strength, its rotation; a shell's offset in world units and its height
	// prevModel is the model matrix's three rows as they were last frame,
	// read by the velocity pass alone. The lit and shadow programs do not
	// declare it; it only has to be in the stride they share.
	prevModel [3]lin.Vec4
	// morph names the draw's morph targets in its model's delta buffer: x
	// the first element of the mesh's block, y the vertices in a target, z
	// how many of morphW and morphIdx are in use. A zero z is a draw with
	// no morph targets, or one the processor blended instead.
	morph [4]float32
	// morphW is the weight of each active target and morphIdx its number,
	// four target numbers packed a byte apiece into each word.
	morphW   [8]float32
	morphIdx [2]uint32
}

const meshInstanceSize = 344

// blended reports whether a material draws after the opaque scene. The
// receiver is a pointer because Material is large and this sits in the
// draw sort's comparator.
func (m *Material) blended() bool { return m.Blend || m.Transmission > 0 }

// marksStencil reports whether a material writes the stencil buffer, so
// the draws that do come first.
func (m *Material) marksStencil() bool { return m.StencilWrite != StencilKeep && m.Outline <= 0 }

// StencilTest is when a material's fragments pass the stencil test: how
// the value already in the stencil buffer must compare to the material's
// StencilRef. The zero value draws everywhere.
type StencilTest uint8

const (
	StencilAlways       StencilTest = iota // no test at all: the default
	StencilEqual                           // only where the buffer holds StencilRef
	StencilNotEqual                        // only where it holds anything else
	StencilLess                            // only where it holds less than StencilRef
	StencilGreater                         // only where it holds more than StencilRef
	StencilNever                           // reject every fragment; only its Fail operation can update stencil
	StencilLessEqual                       // only where it holds at most StencilRef
	StencilGreaterEqual                    // only where it holds at least StencilRef
	stencilTestCount
)

// String names the stencil test.
func (s StencilTest) String() string {
	names := [...]string{"always", "equal", "not-equal", "less", "greater", "never", "less-equal", "greater-equal"}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("StencilTest(%d)", int(s))
}

// compareOp is the Vulkan comparison for the test. Vulkan compares the
// reference against the stored value, so the sense is the other way
// round from the way a material reads.
func (s StencilTest) compareOp() vk.VkCompareOp {
	switch s {
	case StencilEqual:
		return vk.VK_COMPARE_OP_EQUAL
	case StencilNotEqual:
		return vk.VK_COMPARE_OP_NOT_EQUAL
	case StencilLess:
		return vk.VK_COMPARE_OP_GREATER
	case StencilGreater:
		return vk.VK_COMPARE_OP_LESS
	case StencilNever:
		return vk.VK_COMPARE_OP_NEVER
	case StencilLessEqual:
		return vk.VK_COMPARE_OP_GREATER_OR_EQUAL
	case StencilGreaterEqual:
		return vk.VK_COMPARE_OP_LESS_OR_EQUAL
	}
	return vk.VK_COMPARE_OP_ALWAYS
}

// StencilOp is what a fragment that passes both the stencil and the
// depth test does to the stencil buffer. The zero value leaves it alone.
type StencilOp uint8

const (
	StencilKeep          StencilOp = iota // leave the value alone: the default
	StencilReplace                        // store StencilRef
	StencilIncrement                      // add one, stopping at 255
	StencilDecrement                      // subtract one, stopping at 0
	StencilZero                           // store zero
	StencilInvert                         // invert all bits
	StencilIncrementWrap                  // add one, wrapping 255 to zero
	StencilDecrementWrap                  // subtract one, wrapping zero to 255
	stencilOpCount
)

// String names the stencil operation.
func (o StencilOp) String() string {
	names := [...]string{"keep", "replace", "increment", "decrement", "zero", "invert", "increment-wrap", "decrement-wrap"}
	if int(o) < len(names) {
		return names[o]
	}
	return fmt.Sprintf("StencilOp(%d)", int(o))
}

// vkOp is the Vulkan operation.
func (o StencilOp) vkOp() vk.VkStencilOp {
	switch o {
	case StencilReplace:
		return vk.VK_STENCIL_OP_REPLACE
	case StencilIncrement:
		return vk.VK_STENCIL_OP_INCREMENT_AND_CLAMP
	case StencilDecrement:
		return vk.VK_STENCIL_OP_DECREMENT_AND_CLAMP
	case StencilZero:
		return vk.VK_STENCIL_OP_ZERO
	case StencilInvert:
		return vk.VK_STENCIL_OP_INVERT
	case StencilIncrementWrap:
		return vk.VK_STENCIL_OP_INCREMENT_AND_WRAP
	case StencilDecrementWrap:
		return vk.VK_STENCIL_OP_DECREMENT_AND_WRAP
	}
	return vk.VK_STENCIL_OP_KEEP
}

// drawList is a queue's mesh draws in the order they are drawn, held as
// a permutation of the queue's draws. Ordering moves four-byte indices
// rather than whole meshDraw records, and a draw's position in the order
// is also its index in the instance stream, which is built in the same
// order.
type drawList struct {
	draws []meshDraw
	order []int32
}

// len is how many draws the list holds.
func (l drawList) len() int { return len(l.order) }

// at returns the i'th draw in order. The result points into the queue,
// so it stays valid only until the queue is reset.
func (l drawList) at(i int) *meshDraw { return &l.draws[l.order[i]] }

// slice narrows the list to the draws from lo up to hi in order.
func (l drawList) slice(lo, hi int) drawList {
	return drawList{draws: l.draws, order: l.order[lo:hi]}
}
