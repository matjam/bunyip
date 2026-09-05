package gfx

import (
	"fmt"
	"math"
	"slices"
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

const (
	hdrFormat      = vk.VK_FORMAT_R16G16B16A16_SFLOAT
	shadowMapSize  = 2048
	shadowCascades = 3
	// maxPointLights is how many point and spot lights a frame keeps.
	// They live in a storage buffer and are found through the cluster
	// grid, so what a frame costs follows the lights each part of the
	// view holds rather than the number the scene added.
	maxPointLights  = 1024
	maxSpotShadows  = 4    // spot lights with shadow maps in one frame
	spotShadowSize  = 1024 // pixels across a spot light's shadow map
	maxPointShadows = 4    // point lights with cube shadow maps in one frame
	pointFaceSize   = 512  // pixels across one face of a point light's cube
	// pointFaceBase is the first map index of the cube faces, after the
	// cascades and the spot maps.
	pointFaceBase = shadowCascades + maxSpotShadows
	// shadowAtlasW and shadowAtlasH are one depth image holding every
	// shadow map, so every shadow costs one binding rather than one each:
	// the three cascades and the spot maps in the square top, the cube
	// faces in the strip below it, eight to a row.
	shadowAtlasW = 4096
	shadowAtlasH = 6144
	// pointFacesPerRow is how many cube faces fit across the strip; the
	// strip holds 32 and four lights use 24.
	pointFacesPerRow = shadowAtlasW / pointFaceSize
	// matImages is how many sampled images the material set holds:
	// five material textures, four shader images, the environment cube,
	// the thickness map, the scene copy for transmission and the
	// transmission map. Keeping this and the shadow atlas together at or
	// under thirty-one leaves the mesh pipelines inside the texture
	// limit MoltenVK reports on Intel Macs; Apple silicon allows 128.
	matImages = 13
	// matSamplerBinding is where the material set's shared sampler array
	// sits, after the images.
	matSamplerBinding = matImages
	// matSamplers is how many samplers that array holds: linear repeat,
	// linear clamp, nearest repeat, nearest clamp, in that order. They
	// are immutable in the layout, so no set ever writes them, and Metal
	// sees five samplers a stage counting the shadow atlas's, well under
	// its limit of sixteen.
	matSamplers = 4
)

// samplerIndex is where a texture's filtering and edge handling sit in
// the material set's sampler array: bit 1 is nearest, bit 0 is clamp.
func samplerIndex(linear, repeat bool) uint32 {
	i := uint32(0)
	if !linear {
		i |= 2
	}
	if !repeat {
		i |= 1
	}
	return i
}

// shadowRegion is where a shadow map lives in the atlas: cascades in
// three quadrants of the square top, the spot maps sharing the fourth,
// and each point light's six cube faces in the strip below, in slot
// order. The fragment prelude computes the same rectangles.
func shadowRegion(index int) vk.VkRect2D {
	if index < shadowCascades {
		return vk.VkRect2D{Offset: vk.VkOffset2D{X: int32(index%2) * shadowMapSize, Y: int32(index/2) * shadowMapSize}, Extent: vk.VkExtent2D{Width: shadowMapSize, Height: shadowMapSize}}
	}
	if index < pointFaceBase {
		k := index - shadowCascades
		return vk.VkRect2D{Offset: vk.VkOffset2D{X: shadowMapSize + int32(k%2)*spotShadowSize, Y: shadowMapSize + int32(k/2)*spotShadowSize}, Extent: vk.VkExtent2D{Width: spotShadowSize, Height: spotShadowSize}}
	}
	k := index - pointFaceBase
	return vk.VkRect2D{
		Offset: vk.VkOffset2D{X: int32(k%pointFacesPerRow) * pointFaceSize, Y: 2*shadowMapSize + int32(k/pointFacesPerRow)*pointFaceSize},
		Extent: vk.VkExtent2D{Width: pointFaceSize, Height: pointFaceSize},
	}
}

// meshPass owns the 3D pipelines, targets and per-frame uniforms.
type meshPass struct {
	defaultShader *Shader // the standard material, whose pipelines are its variants
	jointLayout   *render.StorageSets
	uniformLayout *render.UniformSets // owns the layout the pipelines were built against
	shadowAtlas   *render.Target      // every shadow map, see shadowRegion
	shadowFormat  vk.VkFormat         // the atlas's depth format, which the shadow pipelines render to
	shadowSet     vk.VkDescriptorSet
	shadowDesc    *render.DescriptorSets
	shadowSamp    vk.VkSampler
	// materials is set 0: thirteen sampled images (five material
	// textures, a shader's image0..3, the environment cube, the
	// thickness map, the scene copy, the transmission map) and the
	// shared sampler array, which is immutable in the layout.
	materials    *render.DescriptorSets
	matSets      map[materialKey]vk.VkDescriptorSet
	lastMatKey   materialKey // the key materialSet last resolved, to skip hashing matSets
	lastMatSet   vk.VkDescriptorSet
	lastMatOK    bool
	flatNormal   *Texture
	black        *Texture
	blackCube    *render.Image    // stands in for the environment when none is set
	skyPipe      *render.Pipeline // an image environment as the background
	skyParamPipe *render.Pipeline // the procedural sky as the background
	outlinePipe  *render.Pipeline // solid shell where the stencil is clear
	xrayPipe     *render.Pipeline // solid tint where depth says hidden
	decalPipe    *render.Pipeline // set up by the post pass, which owns the depth sampler layout
	decalMesh    *Mesh            // a unit cube
	quad         *Mesh            // the billboard quad, made on first use
}

// decal is a texture projected onto the scene inside a box.
type decal struct {
	tex  *Texture
	box  lin.Mat4
	tint Color
}

// DrawDecal projects a texture onto whatever geometry lies inside a box:
// bullet holes, blood, footprints, road markings. box maps the unit cube
// to the world; the texture is projected along the box's y axis, its
// x and z spanning the image, and fades on surfaces facing away from it.
func (g *Graphics) DrawDecal(tex *Texture, box lin.Mat4, tint Color) {
	if tex == nil {
		tex = g.white
	}
	if tint == (Color{}) {
		tint = White
	}
	g.cur.decals = append(g.cur.decals, decal{tex: tex, box: box, tint: tint})
}

// solidPush is the push block of the outline and x-ray pipelines.
type solidPush struct {
	color  lin.Vec4
	params lin.Vec4 // outline width in pixels, viewport width, height
}

// decalPush is the push block of the decal pipeline.
type decalPush struct {
	box    lin.Mat4
	invBox [3]lin.Vec4
	tint   lin.Vec4
}

const meshStages = vk.VK_SHADER_STAGE_VERTEX_BIT | vk.VK_SHADER_STAGE_FRAGMENT_BIT

var frameUniformsSize = vk.VkDeviceSize(unsafe.Sizeof(frameUniforms{}))

// frameStorage is the size of each storage buffer in the per-frame set,
// in binding order after the frame block: the light records, the cluster
// table and the light index list. They are fixed sizes, so a frame
// writes them without the device wait a resize would cost.
func frameStorage() []vk.VkDeviceSize {
	return []vk.VkDeviceSize{
		vk.VkDeviceSize(maxPointLights * lightRecordSize),
		vk.VkDeviceSize(2 * clusterCount * 4),
		vk.VkDeviceSize(clusterIndices * 4),
	}
}

func defaultLight() Light {
	return Light{Direction: lin.V3(-0.5, -1, -0.3), Color: Color{1, 1, 1, 1}, Ambient: Color{0.15, 0.15, 0.18, 1}}
}

type pointLight struct {
	pos   lin.Vec3
	color Color
	rng   float32
	// A spot light adds a direction and a cone: full inside cosInner,
	// gone outside cosOuter.
	spot               bool
	dir                lin.Vec3
	cosInner, cosOuter float32
	outer              float32 // the outer cone's full angle, for the shadow projection
	shadow             bool    // wants a shadow map; the first maxSpotShadows get one
}

// frameUniforms mirrors the Frame block in pbr.vert (std140).
type frameUniforms struct {
	viewProj      lin.Mat4
	view          lin.Mat4
	lightViewProj [shadowCascades]lin.Mat4
	camPos        lin.Vec4
	lightDir      lin.Vec4
	lightColor    lin.Vec4
	sky           lin.Vec4
	ground        lin.Vec4
	params        lin.Vec4
	splits        lin.Vec4
	radii         lin.Vec4
	sh            [9]lin.Vec4 // environment irradiance
	env           lin.Vec4    // intensity, mip count, kind (1 image, 2 procedural sky)
	invViewProj   lin.Mat4
	horizon       lin.Vec4                 // sky at the horizon, w = air (1 - vacuum)
	skyUp         lin.Vec4                 // up axis, w = stars
	sun           lin.Vec4                 // towards the sun, w = angular radius
	sunColor      lin.Vec4                 // the drawn disc's radiance
	fog           lin.Vec4                 // fog colour, w = exponential density
	fogRange      lin.Vec4                 // linear start, end; ground fog height, falloff
	spotViewProj  [maxSpotShadows]lin.Mat4 // each shadowed spot light's projection
	// pointViewProj holds the six face projections of each shadowed
	// point light, slot by slot, in the order of pointFaces.
	pointViewProj [maxPointShadows * 6]lin.Mat4
	// cluster is the cluster grid's mapping: tile width and height in
	// pixels, then the scale and bias from a view depth to a slice. The
	// lights themselves are in the storage buffers of the same set.
	cluster lin.Vec4
	// The global illumination block, at the end after everything the
	// older shaders read.
	probePos    [maxProbes]lin.Vec4 // xyz where the probe was captured, w = kind (1 box, 2 sphere)
	probeMin    [maxProbes]lin.Vec4 // xyz the box's minimum corner, w = the sphere's radius
	probeMax    [maxProbes]lin.Vec4 // xyz the box's maximum corner, w = the blend margin
	probeParams [maxProbes]lin.Vec4 // x intensity, y mip count, z box projection
	gridOrigin  lin.Vec4            // xyz the probe grid's origin, w = intensity (0 no grid)
	gridSpacing lin.Vec4            // xyz the distance between cells
	gridCounts  lin.Vec4            // xyz how many cells on each axis
	reflect     lin.Vec4            // x strength, y max roughness, z max distance, w steps
	// The atmosphere, appended after that.
	atmos lin.Vec4 // planet radius, air height, rayleigh and mie falloff heights
	betaR lin.Vec4 // rayleigh scattering per unit at the ground, w = sun intensity
	betaM lin.Vec4 // mie scattering, forward lobe, camera altitude, w = 1 with an atmosphere
}

// materialKey identifies a material descriptor set: its textures, the
// shader's images and the frame's environment map. The order of tex is
// the order the set binds them and the order the packed sampler indices
// are read in.
type materialKey struct {
	tex   [11]*Texture // five material textures, four shader images, the thickness and transmission maps
	env   *Environment
	scene *render.Image // the output's opaque scene copy, for transmission
}

func (g *Graphics) initMeshPass() error {
	mp := &g.meshes
	mp.matSets = map[materialKey]vk.VkDescriptorSet{}
	dev := g.r.Device
	var err error
	// The pipelines are built against this layout and bound with each
	// queue's own sets, so both are made the same way: the frame block at
	// binding 0, the light probe grid at binding 1 and the frame's lights
	// and cluster grid at bindings 2 and up.
	layout, err := dev.NewFrameSets(frameUniformsSize, gridStorageSize, frameStorage(), meshStages)
	if err != nil {
		return err
	}
	mp.uniformLayout = layout
	if mp.jointLayout, err = dev.NewStorageSets(64, vk.VK_SHADER_STAGE_VERTEX_BIT); err != nil {
		return err
	}
	// Set 0 keeps its images and its samplers apart: thirteen sampled
	// images, then one array of four samplers baked into the layout that
	// a shader indexes per texture slot. Sampling an array of samplers by
	// a dynamically uniform index needs this feature, which every desktop
	// driver and MoltenVK report.
	if !dev.ArrayIndexing() {
		return fmt.Errorf("gfx: this GPU does not support shaderSampledImageArrayDynamicIndexing, which mesh materials need")
	}
	matBindings := make([]render.DescriptorBinding, matSamplerBinding+1)
	for i := range matImages {
		matBindings[i] = render.DescriptorBinding{
			Type: vk.VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE,
			// A shader's vertex hook may read the same images, for
			// displacement maps.
			Stages: meshStages,
		}
	}
	matBindings[matSamplerBinding] = render.DescriptorBinding{
		Type: vk.VK_DESCRIPTOR_TYPE_SAMPLER, Count: matSamplers, Stages: meshStages,
		Immutable: []vk.VkSampler{g.linearRep, g.linear, g.nearestRep, g.nearest},
	}
	if mp.materials, err = dev.NewDescriptors(matBindings, 1024); err != nil {
		return err
	}
	var blackFace [6][]byte
	for i := range blackFace {
		blackFace[i] = make([]byte, 8)
	}
	if mp.blackCube, err = dev.NewCubemapImage(1, vk.VK_FORMAT_R16G16B16A16_SFLOAT, 8, [][6][]byte{blackFace}); err != nil {
		return err
	}
	if mp.skyPipe, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.SkyFrag,
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		PushConstantSize: push2DSize,
		SetLayouts:       []vk.VkDescriptorSetLayout{g.descriptors.Layout},
	}); err != nil {
		return err
	}
	if mp.skyParamPipe, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.SkyParamFrag,
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		PushConstantSize: push2DSize,
		SetLayouts:       []vk.VkDescriptorSetLayout{mp.uniformLayout.Layout},
	}); err != nil {
		return err
	}
	// Outlines: the shell where the mesh's own stencil mark is absent.
	// X-ray: the mesh's tint where something else is nearer.
	solidBindings, allAttrs := meshVertexLayout()
	var solidAttrs []vk.VkVertexInputAttributeDescription
	for _, a := range allAttrs { // outline.vert reads position, normal and the model rows
		if a.Location <= 1 || (a.Location >= 5 && a.Location <= 7) {
			solidAttrs = append(solidAttrs, a)
		}
	}
	solid := render.PipelineDesc{
		Vert: shaders.OutlineVert, Frag: shaders.SolidFrag,
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		Bindings: solidBindings, Attributes: solidAttrs,
		DepthTest: true, Stencil: render.StencilNotEqual(1),
		PushConstantSize: uint32(unsafe.Sizeof(solidPush{})),
		SetLayouts:       []vk.VkDescriptorSetLayout{layout.Layout},
	}
	if mp.outlinePipe, err = dev.NewPipeline(solid); err != nil {
		return err
	}
	xray := solid
	xray.Stencil = nil
	xray.DepthCompare = vk.VK_COMPARE_OP_GREATER
	xray.Blend = true
	if mp.xrayPipe, err = dev.NewPipeline(xray); err != nil {
		return err
	}
	cv, ci := CubeMesh()
	if mp.decalMesh, err = g.NewMesh(cv, ci); err != nil {
		return err
	}
	if mp.shadowSamp, err = dev.NewShadowSampler(); err != nil {
		return err
	}
	if mp.shadowDesc, err = dev.NewImmutableSamplerDescriptors(1, 4, mp.shadowSamp); err != nil {
		return err
	}
	// The atlas is only rendered to and sampled, so it takes a depth
	// format without a stencil aspect where the device has one, which
	// halves what the strip of cube faces costs.
	mp.shadowFormat = dev.ShadowFormat(g.r.DepthFormat)
	if mp.shadowAtlas, err = dev.NewTarget(vk.VkExtent2D{Width: shadowAtlasW, Height: shadowAtlasH}, vk.VK_FORMAT_UNDEFINED, mp.shadowFormat); err != nil {
		return err
	}
	atlasDepth := mp.shadowAtlas.Depth
	if err := g.setup(func(cb vk.VkCommandBuffer) { render.ClearDepthForSampling(cb, atlasDepth) }); err != nil {
		return err
	}
	if mp.shadowSet, err = mp.shadowDesc.AllocateMany([]render.SamplerBinding{{View: atlasDepth.View, Sampler: mp.shadowSamp}}); err != nil {
		return err
	}
	if mp.flatNormal, err = g.newTexture(1, 1, []byte{128, 128, 255, 255}, TextureOptions{Data: true}); err != nil {
		return err
	}
	if mp.black, err = g.newTexture(1, 1, []byte{0, 0, 0, 255}, TextureOptions{Data: true}); err != nil {
		return err
	}
	mp.defaultShader = &Shader{g: g, frag: shaders.PBRFrag, oitFrag: shaders.PBROITFrag, mesh: true, pipes: map[pipeKey]*render.Pipeline{}}
	for _, key := range []pipeKey{{blend: BlendReplace}, {blend: BlendAlpha}, {blend: BlendReplace, shadow: true}} {
		if _, err := mp.defaultShader.pipeline(key); err != nil {
			return err
		}
	}
	return nil
}

// pipelineDesc is the lit pass pipeline for static or skinned meshes,
// without programs: each mesh shader supplies its own.
func (mp *meshPass) pipelineDesc(skinned bool) render.PipelineDesc {
	g := mp.defaultShader.g
	bindings, attrs := meshVertexLayout()
	if skinned {
		bindings, attrs = skinVertexLayout()
	}
	return render.PipelineDesc{
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		Bindings: bindings, Attributes: attrs,
		CullMode: vk.VK_CULL_MODE_BACK_BIT, DepthTest: true, DepthWrite: true,
		SetLayouts: []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniformLayout.Layout, mp.shadowDesc.Layout, mp.jointLayout.Layout, g.uniforms.Layout},
	}
}

// shadowPipelineDesc is the depth-only shadow pass pipeline, without a
// vertex program. The vertex prelude declares the whole instance stream,
// so the layout matches the lit pass. Depth is clamped rather than
// clipped, so a caster above a cascade's near plane still casts into it;
// where the device has no depth clamping the cascade moves its near
// plane back instead.
func (mp *meshPass) shadowPipelineDesc(skinned bool) render.PipelineDesc {
	g := mp.defaultShader.g
	bindings, attrs := meshVertexLayout()
	if skinned {
		bindings, attrs = skinVertexLayout()
	}
	return render.PipelineDesc{
		Frag:    shaders.ShadowFrag,
		NoColor: true, DepthFormat: mp.shadowFormat,
		Bindings: bindings, Attributes: attrs,
		CullMode: vk.VK_CULL_MODE_NONE, DepthTest: true, DepthWrite: true,
		DepthBias: 1.5, DepthSlopeBias: 2.0, DepthClamp: true,
		PushConstantSize: 4, // the shadow map index: cascade, spot map or cube face
		SetLayouts:       []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniformLayout.Layout, mp.shadowDesc.Layout, mp.jointLayout.Layout, g.uniforms.Layout},
	}
}

// meshVertexLayout is the per-vertex binding 0 and per-instance binding 1.
func meshVertexLayout() ([]vk.VkVertexInputBindingDescription, []vk.VkVertexInputAttributeDescription) {
	bindings := []vk.VkVertexInputBindingDescription{
		{Binding: 0, Stride: vertexSize, InputRate: vk.VK_VERTEX_INPUT_RATE_VERTEX},
		{Binding: 1, Stride: meshInstanceSize, InputRate: vk.VK_VERTEX_INPUT_RATE_INSTANCE},
	}
	attrs := []vk.VkVertexInputAttributeDescription{
		{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 0},
		{Location: 1, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 12},
		{Location: 2, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 24},
		{Location: 3, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 32},
		{Location: 4, Binding: 0, Format: vk.VK_FORMAT_R8G8B8A8_UNORM, Offset: 40},
	}
	for i := range 12 { // the instance stream: twelve vec4s
		attrs = append(attrs, vk.VkVertexInputAttributeDescription{Location: uint32(5 + i), Binding: 1, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: uint32(16 * i)})
	}
	return bindings, attrs
}

// SetCamera sets the camera for this frame's meshes.
func (g *Graphics) SetCamera(c Camera) { g.cur.camera, g.cur.hasCam = c, true }

// ensureCamera gives a queue that drew a 3D scene without SetCamera the
// default camera, five units back down the z axis. It runs before the
// draws are prepared and again before the frame block is written, so
// culling, sorting and the shader see one view.
func (q *drawQueue) ensureCamera() {
	if !q.hasCam {
		q.camera, q.hasCam = Camera{Position: lin.V3(0, 0, 5)}, true
	}
}

// SetLight sets the directional light, ambient term and shadow settings.
func (g *Graphics) SetLight(l Light) { g.cur.light = l }

// AddPointLight adds a light shining from a point in every direction
// for this frame, fading to nothing rng units away: torches, muzzle
// flashes, glowing ore. A frame keeps its first 1024 point and spot
// lights (MaxLights); add the nearest ones first when a scene has more.
func (g *Graphics) AddPointLight(pos lin.Vec3, c Color, rng float32) {
	g.AddPoint(PointLight{Position: pos, Color: c, Range: rng})
}

// PointLight is a light shining from a point in every direction for
// AddPoint, with the option of a shadow map: a lamp in a room, a fire
// under a bridge. The first four shadowed point lights a frame get cube
// maps (MaxPointShadows), the rest shine without; add the nearest first.
type PointLight struct {
	Position lin.Vec3
	Color    Color
	Range    float32 // fades to nothing this far away
	Shadows  bool    // render a cube shadow map for this light
}

// MaxPointShadows is how many point lights cast shadows in one frame.
// Each one costs six depth passes, one for each face of its cube.
const MaxPointShadows = maxPointShadows

// AddPoint adds a point light for this frame.
func (g *Graphics) AddPoint(p PointLight) {
	if len(g.cur.points) >= maxPointLights {
		g.stats.LightsDropped++
		return
	}
	g.cur.points = append(g.cur.points, pointLight{pos: p.Position, color: p.Color, rng: p.Range, shadow: p.Shadows})
	g.stats.Lights++
}

// MaxLights is how many point and spot lights a frame keeps. The lights
// are sorted into a grid of clusters over the view, and a fragment is
// lit by its own cluster's lights alone, so a scene may add hundreds
// without every one costing every pixel. A cluster keeps 64 lights, and
// a light past that in a crowded part of the view does not light it.
const MaxLights = maxPointLights

// AddSpotLight adds a light shining from a point along dir in a cone,
// fading to nothing rng units away: flashlights, headlights, stage
// lights. The cone is full inside innerAngle and fades to nothing at
// outerAngle (both full angles in radians; a zero inner angle means a
// hard-edged cone, a zero outer angle means 45 degrees). Spot lights
// count against the same limit as point lights.
func (g *Graphics) AddSpotLight(pos, dir lin.Vec3, c Color, rng, innerAngle, outerAngle float32) {
	g.AddSpot(SpotLight{Position: pos, Direction: dir, Color: c, Range: rng, InnerAngle: innerAngle, OuterAngle: outerAngle})
}

// SpotLight is a cone of light for AddSpot, with the option of a shadow
// map: a flashlight that throws the bars' shadows, a lamp over a
// table. The first four shadowed spot lights a frame get maps
// (MaxSpotShadows), the rest shine without; add the nearest first.
type SpotLight struct {
	Position  lin.Vec3
	Direction lin.Vec3
	Color     Color
	Range     float32 // fades to nothing this far away
	// InnerAngle and OuterAngle are the cone's full angles in radians: full
	// inside the inner, fading to nothing at the outer; zero outer means
	// 45 degrees.
	InnerAngle, OuterAngle float32
	Shadows                bool // render a shadow map for this light
}

// MaxSpotShadows is how many spot lights cast shadows in one frame.
const MaxSpotShadows = maxSpotShadows

// AddSpot adds a spot light for this frame.
func (g *Graphics) AddSpot(s SpotLight) {
	if len(g.cur.points) >= maxPointLights {
		g.stats.LightsDropped++
		return
	}
	if s.OuterAngle <= 0 {
		s.OuterAngle = lin.Radians(45)
	}
	if s.InnerAngle > s.OuterAngle {
		s.InnerAngle = s.OuterAngle
	}
	g.cur.points = append(g.cur.points, pointLight{
		pos: s.Position, color: s.Color, rng: s.Range, spot: true, dir: s.Direction.Norm(),
		cosInner: float32(math.Cos(float64(s.InnerAngle) / 2)),
		cosOuter: float32(math.Cos(float64(s.OuterAngle) / 2)),
		outer:    s.OuterAngle, shadow: s.Shadows,
	})
	g.stats.Lights++
}

// spotShadows lists the lights that get shadow maps this frame, in map
// order, and each one's projection.
func (q *drawQueue) spotShadows() (lights []int, mats []lin.Mat4) {
	for i, p := range q.points {
		if !p.spot || !p.shadow || len(lights) >= maxSpotShadows {
			continue
		}
		up := lin.V3(0, 1, 0)
		if abs32(p.dir.Y) > 0.95 {
			up = lin.V3(0, 0, 1)
		}
		rng := max(p.rng, 0.5)
		// A little wider than the cone, so the soft edge has depth to read.
		proj := lin.Perspective(min(p.outer*1.1, lin.Radians(170)), 1, 0.05, rng)
		lights = append(lights, i)
		mats = append(mats, proj.Mul(lin.LookAt(p.pos, p.pos.Add(p.dir), up)))
	}
	return lights, mats
}

// pointFaces are the six directions of a cube shadow map and the up
// axis each one looks with, in the order the fragment prelude picks a
// face from the light-to-fragment direction: +x, -x, +y, -y, +z, -z.
var pointFaces = [6]struct{ dir, up lin.Vec3 }{
	{lin.V3(1, 0, 0), lin.V3(0, -1, 0)},
	{lin.V3(-1, 0, 0), lin.V3(0, -1, 0)},
	{lin.V3(0, 1, 0), lin.V3(0, 0, 1)},
	{lin.V3(0, -1, 0), lin.V3(0, 0, -1)},
	{lin.V3(0, 0, 1), lin.V3(0, -1, 0)},
	{lin.V3(0, 0, -1), lin.V3(0, -1, 0)},
}

// pointShadows lists the point lights that get cube maps this frame, in
// slot order, and the six face projections of each, slot by slot. Each
// face looks along its axis with a field of view a little over ninety
// degrees, so the fragment prelude can clamp its filter kernel inside
// the face and still cover the whole ninety-degree cone.
func (q *drawQueue) pointShadows() (lights []int, mats []lin.Mat4) {
	for i, p := range q.points {
		if p.spot || !p.shadow || len(lights) >= maxPointShadows {
			continue
		}
		rng := max(p.rng, 0.5)
		fov := 2 * float32(math.Atan(float64(1+4.0/pointFaceSize)))
		proj := lin.Perspective(fov, 1, 0.05, rng)
		lights = append(lights, i)
		for _, f := range pointFaces {
			mats = append(mats, proj.Mul(lin.LookAt(p.pos, p.pos.Add(f.dir), f.up)))
		}
	}
	return lights, mats
}

// DrawMesh queues a mesh with a material and a model matrix. Draws that
// share a mesh and material become one instanced draw call; blended
// materials draw after everything opaque, farthest first.
func (g *Graphics) DrawMesh(m *Mesh, mat Material, model lin.Mat4) {
	g.queueMesh(meshDraw{mesh: m, mat: mat, model: model})
}

// queueMesh fills a draw's defaults, captures its shader's uniforms, and
// adds it to the current queue.
func (g *Graphics) queueMesh(d meshDraw) {
	if d.mat.BaseColor == (Color{}) {
		d.mat.BaseColor = White
	}
	if d.mat.Roughness == 0 {
		d.mat.Roughness = 0.6
	}
	d.shader = d.mat.Shader
	if d.shader == nil {
		d.shader = g.meshes.defaultShader
	} else if !d.shader.mesh {
		panic("gfx: Material.Shader wants a mesh shader from NewMeshShader")
	}
	d.uniform = d.shader.uniformOffset()
	g.cur.draws = append(g.cur.draws, d)
}

// materialSet returns the descriptor set for a material's textures, its
// shader's images, the environment map in use and the output's scene
// copy, together with the sampler index of each texture slot packed two
// bits apiece, which the instance stream carries to the shader. A
// frame's draws are usually grouped by material, so the last key and set
// are remembered and compared before the map is hashed.
func (g *Graphics) materialSet(mat *Material, env *Environment, scene *render.Image) (vk.VkDescriptorSet, float32, error) {
	mp := &g.meshes
	key := materialKey{env: env, scene: scene}
	key.tex = [11]*Texture{orTex(mat.Texture, g.white), orTex(mat.MetalRoughTexture, g.white), orTex(mat.NormalTexture, mp.flatNormal), orTex(mat.EmissiveTexture, mp.black), orTex(mat.OcclusionTexture, g.white)}
	if mat.Shader != nil {
		for i, t := range mat.Shader.images {
			key.tex[5+i] = t
		}
	}
	thin := mp.black // no map: uniformly thin for subsurface...
	if mat.Thickness > 0 {
		thin = g.white // ...but the full Thickness for a volume
	}
	key.tex[9] = orTex(mat.ThicknessTexture, thin)
	key.tex[10] = orTex(mat.TransmissionTexture, g.white)
	// Each slot's sampler index rides with the draw rather than in the
	// set, so the four samplers are shared by every material. A texture
	// destroyed earlier this frame still has its image until the frame
	// retires it, so the draw keeps its pixels; the set it needs is not
	// cached, since a later frame must not find it.
	var bits uint32
	cache := true
	for i, t := range key.tex {
		if t == nil {
			t = g.white
		}
		bits |= samplerIndex(!t.nearest, t.repeat) << (2 * i)
		if t.destroyed {
			cache = false
		}
	}
	samplers := float32(bits)
	if cache {
		if mp.lastMatOK && key == mp.lastMatKey {
			return mp.lastMatSet, samplers, nil
		}
		if set, ok := mp.matSets[key]; ok {
			mp.lastMatKey, mp.lastMatSet, mp.lastMatOK = key, set, true
			return set, samplers, nil
		}
	}
	// Bindings 0..8 are the material and image textures, 9 the environment
	// cube, 10 the thickness map, 11 the scene copy, 12 the transmission
	// map. They are sampled images; binding 13 holds the samplers.
	bindings := make([]render.SamplerBinding, matImages)
	for i, t := range key.tex[:9] {
		if t == nil {
			t = g.white
		}
		bindings[i] = render.SamplerBinding{View: t.img.View}
	}
	cube := mp.blackCube
	if env != nil {
		cube = env.cube
	}
	bindings[9] = render.SamplerBinding{View: cube.View}
	bindings[10] = render.SamplerBinding{View: key.tex[9].img.View}
	bindings[11] = render.SamplerBinding{View: scene.View}
	bindings[12] = render.SamplerBinding{View: key.tex[10].img.View}
	set, err := mp.materials.AllocateMany(bindings)
	if err != nil {
		return 0, 0, err
	}
	if !cache {
		g.deferDestroy(func() { mp.materials.Free(set) })
		return set, samplers, nil
	}
	mp.matSets[key] = set
	mp.lastMatKey, mp.lastMatSet, mp.lastMatOK = key, set, true
	return set, samplers, nil
}

// forgetEnvironment drops cached material sets that reference a destroyed
// environment.
func (g *Graphics) forgetEnvironment(env *Environment) {
	g.forgetMaterialSets(func(key materialKey) bool { return key.env == env })
}

// forgetScene drops cached material sets that reference an output's
// destroyed scene copy.
func (g *Graphics) forgetScene(scene *render.Image) {
	g.forgetMaterialSets(func(key materialKey) bool { return key.scene == scene })
}

// forgetMaterialSets drops every cached material set matching a
// predicate. The sets themselves are freed once the frame that may still
// bind them has finished.
func (g *Graphics) forgetMaterialSets(match func(materialKey) bool) {
	mp := &g.meshes
	mp.lastMatOK = false
	var freed []vk.VkDescriptorSet
	for key, set := range mp.matSets {
		if match(key) {
			freed = append(freed, set)
			delete(mp.matSets, key)
		}
	}
	if len(freed) == 0 {
		return
	}
	g.deferDestroy(func() {
		for _, set := range freed {
			mp.materials.Free(set)
		}
	})
}

func orTex(t, fallback *Texture) *Texture {
	if t == nil {
		return fallback
	}
	return t
}

// forgetTexture drops cached descriptor sets that reference a destroyed texture.
func (g *Graphics) forgetTexture(t *Texture) {
	g.forgetMaterialSets(func(key materialKey) bool { return slices.Contains(key.tex[:], t) })
	var freed []vk.VkDescriptorSet
	for key, set := range g.imageSets {
		if slices.Contains(key[:], t) {
			freed = append(freed, set)
			delete(g.imageSets, key)
		}
	}
	if len(freed) == 0 {
		return
	}
	g.deferDestroy(func() {
		for _, set := range freed {
			g.descriptors.Free(set)
		}
	})
}

// cascades fits one orthographic light frustum to each slice of the
// camera frustum out to the shadow distance, returning the matrices and
// the view-space depth where each cascade ends. A cascade's near plane
// sits two slice radii above the slice, which a caster higher than that
// would fall in front of; the shadow pipelines clamp depth instead of
// clipping there, and where the device cannot, the near plane moves back
// to hold every caster the queue has (q.casterAlong).
func (q *drawQueue) cascades(aspect float32) ([shadowCascades]lin.Mat4, lin.Vec4, lin.Vec4) {
	var mats [shadowCascades]lin.Mat4
	var splits, radii lin.Vec4
	far := q.light.ShadowDistance
	if far <= 0 {
		far = 60
	}
	_, _, near, _ := q.camera.defaults()
	dir := q.light.Direction.Norm()
	lightUp := lin.V3(0, 1, 0)
	if abs32(dir.Y) > 0.95 {
		lightUp = lin.V3(0, 0, 1)
	}
	invVP := q.camera.Projection(aspect).Mul(q.camera.viewMatrix()).Inverse()
	// Practical split scheme, weighted towards logarithmic.
	ends := [shadowCascades]float32{}
	for i := range ends {
		f := float32(i+1) / shadowCascades
		logSplit := near * float32(math.Pow(float64(far/near), float64(f)))
		linSplit := near + (far-near)*f
		ends[i] = 0.7*logSplit + 0.3*linSplit
	}
	splits = lin.V4(ends[0], ends[1], ends[2], 0)
	prev := near
	proj := q.camera.Projection(aspect)
	for i := range mats {
		// Slice corners in world space: unproject the 8 corners at the
		// slice's near and far depths.
		var corners [8]lin.Vec3
		k := 0
		for _, z := range [2]float32{prev, ends[i]} {
			clipZ := proj.MulVec4(lin.V4(0, 0, -z, 1))
			ndcZ := clipZ.Z / clipZ.W
			for _, x := range [2]float32{-1, 1} {
				for _, y := range [2]float32{-1, 1} {
					p := invVP.MulVec4(lin.V4(x, y, ndcZ, 1))
					corners[k] = p.Vec3().Mul(1 / p.W)
					k++
				}
			}
		}
		var centre lin.Vec3
		for _, c := range corners {
			centre = centre.Add(c)
		}
		centre = centre.Mul(1.0 / 8)
		radius := float32(0)
		for _, c := range corners {
			radius = max(radius, c.Sub(centre).Len())
		}
		// Snap the centre to shadow-map texels so edges do not swim.
		texel := radius * 2 / shadowMapSize
		view := lin.LookAt(centre.Sub(dir.Mul(radius*2)), centre, lightUp)
		c := view.MulPoint(centre)
		c.X = float32(math.Floor(float64(c.X/texel))) * texel
		c.Y = float32(math.Floor(float64(c.Y/texel))) * texel
		centre = view.Inverse().MulPoint(c)
		// Without depth clamping the volume has to reach every caster, so
		// the eye moves back along the light until the nearest one is
		// inside it. The x and y extents, and the snapping, do not change.
		back := float32(0)
		if !q.depthClamp && q.hasCasters {
			// The eye sits 2 radii before the centre; a caster is at
			// -casterAlong along the light from the origin at the furthest.
			back = max(0, 0.1-(-q.casterAlong-centre.Dot(dir)+radius*2))
		}
		view = lin.LookAt(centre.Sub(dir.Mul(radius*2+back)), centre, lightUp)
		mats[i] = lin.Ortho(-radius, radius, -radius, radius, 0.1, radius*4+back).Mul(view)
		switch i {
		case 0:
			radii.X = radius
		case 1:
			radii.Y = radius
		default:
			radii.Z = radius
		}
		prev = ends[i]
	}
	return mats, splits, radii
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// writeUniforms fills the queue's frame block for the slot and uploads
// the frame's lights and cluster grid. It runs after prepareDraws, whose
// caster bounds the cascades need, and keeps the cascade matrices for
// the shadow pass to cull against.
func (q *drawQueue) writeUniforms(slot int, extent vk.VkExtent2D, time float32, refl lin.Vec4) error {
	q.ensureCamera()
	aspect := float32(extent.Width) / float32(extent.Height)
	l := q.light
	strength := l.ShadowStrength
	if strength == 0 {
		strength = 1
	}
	sky := l.Sky.resolved(l)
	mats, splits, radii := q.cascades(aspect)
	q.cascadeMats = mats
	u := frameUniforms{
		viewProj:      q.camera.ViewProj(aspect),
		view:          q.camera.viewMatrix(),
		lightViewProj: mats,
		camPos:        q.camera.Position.Vec4(1),
		lightDir:      l.Direction.Norm().Vec4(0),
		lightColor:    lin.V4(l.Color.R, l.Color.G, l.Color.B, strength),
		sky:           lin.V4(sky.Zenith.R, sky.Zenith.G, sky.Zenith.B, 1),
		ground:        lin.V4(sky.Ground.R, sky.Ground.G, sky.Ground.B, 1),
		params:        lin.V4(shadowMapSize, boolFloat(l.Shadows), float32(len(q.points)), time),
		splits:        splits,
		radii:         radii,
		horizon:       lin.V4(sky.Horizon.R, sky.Horizon.G, sky.Horizon.B, 1-sky.Vacuum),
		skyUp:         sky.Up.Vec4(sky.Stars),
		sun:           l.Direction.Norm().Mul(-1).Vec4(sky.SunSize),
		sunColor:      lin.V4(sky.Sun.R, sky.Sun.G, sky.Sun.B, 1),
		reflect:       refl,
	}
	for i, p := range q.probes {
		intensity := p.Intensity
		if intensity == 0 {
			intensity = 1
		}
		u.probePos[i] = p.Position.Vec4(p.kind())
		if p.kind() == 2 {
			u.probeMin[i] = p.Position.Vec4(p.Radius)
			u.probeMax[i] = p.Position.Vec4(p.Margin)
		} else {
			u.probeMin[i] = p.Position.Sub(p.Extent).Vec4(0)
			u.probeMax[i] = p.Position.Add(p.Extent).Vec4(p.Margin)
		}
		u.probeParams[i] = lin.V4(intensity, float32(p.env.mips), boolFloat(p.BoxProjection), 0)
	}
	u.gridOrigin, u.gridSpacing, u.gridCounts = q.grid.gridUniforms()
	lights, spotMats := q.spotShadows()
	copy(u.spotViewProj[:], spotMats)
	points, pointMats := q.pointShadows()
	copy(u.pointViewProj[:], pointMats)
	u.cluster = q.clusters.clusterParams(float32(extent.Width), float32(extent.Height))
	if f := l.Fog; f.End > f.Start || f.Density > 0 {
		u.fog = lin.V4(f.Color.R, f.Color.G, f.Color.B, f.Density)
		u.fogRange = lin.V4(f.Start, f.End, f.Height, f.HeightFalloff)
	}
	if a := sky.Atmosphere; a.Height > 0 {
		u.atmos = lin.V4(a.PlanetRadius, a.Height, a.Height/rayleighFalloff, a.Height/mieFalloff)
		u.betaR = lin.V4(a.Rayleigh.R, a.Rayleigh.G, a.Rayleigh.B, a.Intensity)
		u.betaM = lin.V4(a.Mie, a.Forward, a.Altitude, 1)
	}
	if env := l.Environment; env != nil && env.cube != nil {
		u.sh = env.sh
		u.env = lin.V4(env.scale, float32(env.mips), 1, 0)
	} else {
		// The harmonics depend on the sky's colours alone, so a sun that
		// follows an animated light does not reproject them every frame.
		if key := sky.key(); key != q.skyCached {
			q.skyCached, q.skySH = key, sky.sh()
		}
		u.sh = q.skySH
		u.env = lin.V4(1, 0, 2, 0)
	}
	u.invViewProj = u.viewProj.Inverse()
	if err := q.writeGrid(slot); err != nil {
		return err
	}
	if err := q.uniforms.Write(slot, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u))); err != nil {
		return err
	}
	return q.writeLights(slot, aspect, lights, points)
}

// writeLights builds the frame's light records and cluster grid and
// uploads them to the slot's storage buffers, which are part of the same
// per-frame set as the frame block. spots and cubes name the lights that
// got a spot map and a cube map, so a shadowed light's map travels with
// its record.
func (q *drawQueue) writeLights(slot int, aspect float32, spots, cubes []int) error {
	n := len(q.points)
	q.spotSlots = slices.Grow(q.spotSlots[:0], n)[:n]
	q.pointSlots = slices.Grow(q.pointSlots[:0], n)[:n]
	for i := range n {
		q.spotSlots[i], q.pointSlots[i] = -1, -1
	}
	for k, i := range spots {
		q.spotSlots[i] = int32(k)
	}
	for k, i := range cubes {
		q.pointSlots[i] = int32(k)
	}
	q.clusters.build(q.points, q.spotSlots, q.pointSlots, q.camera, aspect)
	if err := q.uniforms.WriteFixed(slot, 0, q.clusters.lightBytes()); err != nil {
		return err
	}
	if err := q.uniforms.WriteFixed(slot, 1, q.clusters.tableBytes()); err != nil {
		return err
	}
	return q.uniforms.WriteFixed(slot, 2, q.clusters.indexBytes())
}

func boolFloat(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// prepareDraws resolves material sets and bounds, culls draws outside
// the camera's view, sorts opaque draws for instancing and blended draws
// back to front, and uploads the instance stream. It returns three
// groups in the order they are recorded: the opaque draws, the blended
// draws the order-independent pass accumulates, and the blended draws
// that still draw in sorted order. Culled draws sort to the end of their
// group and stay in the lists for the shadow pass, which sees them from
// the light; q.visOpaque, q.visOIT and q.visBlended count the draws the
// camera sees at the front of each. It runs before writeUniforms and
// leaves each draw's bounding sphere on the draw, for the shadow pass to
// cull against, and the furthest caster's reach against the light in
// q.casterAlong, for the cascades.
func (g *Graphics) prepareDraws(q *drawQueue, slot int, scene *render.Image, aspect float32) (opaque, oit, blended drawList, err error) {
	q.ensureCamera() // culling, sorting and the frame block share one view
	view := q.camera.viewMatrix()
	frustum := FrustumOf(q.camera.ViewProj(aspect))
	env := q.light.Environment
	if env != nil && env.cube == nil {
		env = nil
	}
	culled := 0
	q.depthClamp = g.r.Device.DepthClamp()
	q.hasCasters, q.casterAlong = false, 0
	lightDir := q.light.Direction.Norm()
	// The order-independent pass needs a device that blends its two
	// attachments differently and a shader with the program that writes
	// them. Transmissive draws read the scene behind them, so they keep
	// the sorted path whatever the setting says.
	independent := g.post.settings.OrderIndependent && g.r.Device.IndependentBlend()
	for i := range q.draws {
		d := &q.draws[i]
		d.centre, d.radius, d.cullable = q.drawBounds(d)
		// The probe holding the draw's centre supplies its reflections, so
		// its cube map is what the material set binds.
		d.probe = q.probeFor(d.centre)
		if d.set, d.samplers, err = g.materialSet(&d.mat, q.probeEnv(d.probe, env), scene); err != nil {
			return drawList{}, drawList{}, drawList{}, err
		}
		d.depth = -view.MulPoint(d.centre).Z
		d.blended = d.mat.blended()
		d.oit = independent && d.blended && d.mat.Transmission == 0 && d.shader.orderIndependent()
		d.culled = d.cullable && !frustum.ContainsSphere(d.centre, d.radius)
		if d.culled {
			culled++
		}
		if !d.blended { // opaque draws are the shadow pass's casters
			if along := -lightDir.Dot(d.centre) + d.radius; !q.hasCasters || along > q.casterAlong {
				q.hasCasters, q.casterAlong = true, along
			}
		}
	}
	g.stats.Culled += culled
	all := q.sortDraws()
	// The blended group is split before the instance stream is built, so
	// that a draw's place in the order is still its place in the stream.
	blendedAt := all.len()
	for i := range all.len() {
		if all.at(i).blended {
			blendedAt = i
			break
		}
	}
	oitAt := blendedAt
	if independent {
		oitAt = q.partitionOIT(all, blendedAt)
	}
	q.inst.reset()
	for k := range all.len() {
		d := all.at(k)
		m := &d.mat
		flags := boolFloat(m.NormalTexture != nil) + 2*boolFloat(m.Unlit) + 4*boolFloat(m.OcclusionUV2) + 8*boolFloat(m.EmissiveTexture == nil)
		occlusion := float32(0)
		if m.OcclusionTexture != nil {
			occlusion = orOne(m.OcclusionStrength, true)
		}
		uv := m.UVTransform
		if uv == (lin.Affine{}) {
			uv = lin.Identity2()
		}
		ccRough := m.ClearcoatRoughness
		if m.Clearcoat > 0 && ccRough == 0 {
			ccRough = 0.03
		}
		sheenRough := m.SheenRoughness
		if m.Sheen != (Color{}) && sheenRough == 0 {
			sheenRough = 0.5
		}
		ior := m.IOR
		if ior == 0 {
			ior = 1.5
		}
		atten := m.AttenuationColor
		if atten == (Color{}) {
			atten = White
		}
		mm := d.model
		q.inst.add(meshInstance{
			model: [3]lin.Vec4{
				lin.V4(mm.At(0, 0), mm.At(0, 1), mm.At(0, 2), mm.At(0, 3)),
				lin.V4(mm.At(1, 0), mm.At(1, 1), mm.At(1, 2), mm.At(1, 3)),
				lin.V4(mm.At(2, 0), mm.At(2, 1), mm.At(2, 2), mm.At(2, 3)),
			},
			baseColor: [4]float32{m.BaseColor.R, m.BaseColor.G, m.BaseColor.B, m.BaseColor.A},
			material:  [4]float32{orOne(m.Metallic, m.MetalRoughTexture != nil), m.Roughness, m.Emissive, flags},
			extra:     [4]float32{float32(d.jointBase), m.AlphaCutoff, occlusion, m.Subsurface},
			uvT0:      [4]float32{uv.A, uv.B, uv.C, uv.D},
			uvT1:      [4]float32{uv.E, uv.F, m.Clearcoat, ccRough},
			sheen:     [4]float32{m.Sheen.R, m.Sheen.G, m.Sheen.B, sheenRough},
			volume:    [4]float32{m.Transmission, ior, m.Thickness, m.AttenuationDistance},
			atten:     [4]float32{atten.R, atten.G, atten.B, d.samplers},
			gi:        [4]float32{float32(d.probe), boolFloat(!d.blended), 0, 0},
		})
	}
	if err := q.inst.upload(g, slot); err != nil {
		return drawList{}, drawList{}, drawList{}, err
	}
	if len(q.joints) > 0 {
		data := unsafe.Slice((*byte)(unsafe.Pointer(&q.joints[0])), len(q.joints)*64)
		if err := q.jointBuf.Write(slot, data); err != nil {
			return drawList{}, drawList{}, drawList{}, err
		}
	}
	opaque = all.slice(0, blendedAt)
	oit = all.slice(blendedAt, oitAt)
	blended = all.slice(oitAt, all.len())
	q.visOpaque, q.visOIT, q.visBlended = visibleCount(opaque), visibleCount(oit), visibleCount(blended)
	return opaque, oit, blended, nil
}

// partitionOIT moves the draws the order-independent pass accumulates to
// the front of the blended range, which starts at lo, and returns where
// the draws that stay sorted begin. Each group keeps the order the sort
// gave it, so the camera's draws still come before the culled ones and
// the sorted group is still back to front.
func (q *drawQueue) partitionOIT(all drawList, lo int) int {
	q.sorted = q.sorted[:0]
	at := lo
	for i := lo; i < all.len(); i++ {
		k := all.order[i]
		if q.draws[k].oit {
			all.order[at] = k
			at++
		} else {
			q.sorted = append(q.sorted, k)
		}
	}
	copy(all.order[at:], q.sorted)
	return at
}

// visibleCount is how many draws at the front of a sorted group the
// camera sees.
func visibleCount(draws drawList) int {
	for i := range draws.len() {
		if draws.at(i).culled {
			return i
		}
	}
	return draws.len()
}

// transmissive reports whether any draw needs the opaque scene copy.
func transmissive(draws drawList) bool {
	for i := range draws.len() {
		if draws.at(i).mat.Transmission > 0 {
			return true
		}
	}
	return false
}

// orOne returns the metallic factor; with a metal-rough texture and a zero
// factor the texture drives it, matching glTF's default factor of 1.
func orOne(metallic float32, hasTexture bool) float32 {
	if hasTexture && metallic == 0 {
		return 1
	}
	return metallic
}

// drawRuns records draws as instanced runs of identical mesh, material
// and shader state. first is the index of draws[0] in the instance
// stream. In the shadow pass (cascade set) the depth-only pipelines are
// used; otherwise each draw's shader picks its lit pipeline. Skinned
// draws are never merged, since each has its own joint matrices.
func (g *Graphics) drawRuns(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue, draws drawList, first uint32, cascade *int32, mask []bool, oit bool) error {
	n := draws.len()
	if n == 0 {
		return nil // a sky-only frame has no instance buffer to bind
	}
	rec := &g.rec
	rec.offset = 0
	vk.CmdBindVertexBuffers(cb, 1, 1, &q.inst.buffers[q.inst.slot].Handle, &rec.offset)
	var bound *render.Pipeline
	boundUniform := int32(-2)
	for i := 0; i < n; {
		if mask != nil && !mask[i] { // this draw misses the shadow map
			i++
			continue
		}
		d := draws.at(i)
		run := 1
		if !d.skinned {
			runKey := meshKey(&d.mat, false)
			for i+run < n {
				e := draws.at(i + run)
				if mask != nil && !mask[i+run] {
					break
				}
				if e.skinned || e.mesh != d.mesh || e.set != d.set || e.shader != d.shader || e.uniform != d.uniform || meshKey(&e.mat, false) != runKey {
					break
				}
				run++
			}
		}
		key := meshKey(&d.mat, d.skinned)
		key.oit = oit
		if cascade != nil {
			key = pipeKey{shadow: true, skinned: d.skinned}
		}
		p, err := d.shader.pipeline(key)
		if err != nil {
			return err
		}
		if p != bound {
			bound = p
			boundUniform = -2
			vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Handle)
			vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 1, 1, &q.uniforms.Sets[fr.Slot], 0, nil)
			if cascade != nil {
				rec.cascade = *cascade
				vk.CmdPushConstants(cb, p.Layout, meshStages, 0, 4, unsafe.Pointer(&rec.cascade))
			} else {
				vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 2, 1, &g.meshes.shadowSet, 0, nil)
			}
			if d.skinned {
				vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 3, 1, &q.jointBuf.Sets[fr.Slot], 0, nil)
			}
		}
		vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 0, 1, &d.set, 0, nil)
		if d.uniform >= 0 && d.uniform != boundUniform {
			// Shader uniforms serve the vertex hook in the shadow pass too.
			boundUniform = d.uniform
			rec.dyn = uint32(d.uniform)
			vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 4, 1, &g.uniforms.Sets[fr.Slot], 1, &rec.dyn)
		}
		vk.CmdBindVertexBuffers(cb, 0, 1, &d.mesh.vbuf.Handle, &rec.offset)
		vk.CmdBindIndexBuffer(cb, d.mesh.ibuf.Handle, 0, vk.VK_INDEX_TYPE_UINT32)
		vk.CmdDrawIndexed(cb, d.mesh.IndexCount, uint32(run), 0, 0, first+uint32(i))
		g.stats.Draws3D++
		if cascade == nil {
			g.stats.Instances += run
		} else {
			g.stats.ShadowDraws += run
		}
		i += run
	}
	return nil
}

// renderScene runs the shadow and lit passes of a queue into the targets'
// HDR image.
func (g *Graphics) renderScene(fr *render.Frame, q *drawQueue, t *sceneTargets) error {
	mp := &g.meshes
	cb := fr.CB
	aspect := float32(t.extent.Width) / float32(t.extent.Height)
	// The draws are prepared first: the cascades' near planes need the
	// caster bounds, and the shadow pass culls against every light.
	opaque, oit, blended, err := g.prepareDraws(q, fr.Slot, t.scene, aspect)
	if err != nil {
		return err
	}
	if err := q.writeUniforms(fr.Slot, t.extent, g.time, g.reflectParams()); err != nil {
		return err
	}
	seen, seenOIT, seenBlended := opaque.slice(0, q.visOpaque), oit.slice(0, q.visOIT), blended.slice(0, q.visBlended)
	if seenOIT.len() > 0 {
		if err := g.orderIndependent(t); err != nil {
			return err
		}
	}
	// Every shadow map is a region of one atlas: the cascades, then the
	// spot maps, then each shadowed point light's six cube faces, each
	// drawn with its own viewport. The vertex program picks the
	// projection by index, spot maps past the cascades and cube faces
	// past those.
	spotLights, spotMats := q.spotShadows()
	pointLights, pointMats := q.pointShadows()
	if q.light.Shadows || len(spotLights) > 0 || len(pointLights) > 0 {
		g.timestamps.Begin(cb, "shadow")
		render.BeginTargetPass(cb, render.PassDesc{Target: mp.shadowAtlas, ClearDepth: 1})
		var maps []int
		if q.light.Shadows {
			maps = append(maps, 0, 1, 2)
		}
		for k := range spotLights {
			maps = append(maps, shadowCascades+k)
		}
		for k := range len(pointLights) * 6 {
			maps = append(maps, pointFaceBase+k)
		}
		for _, index := range maps {
			region := shadowRegion(index)
			render.SetViewportRect(cb, region)
			render.SetScissorRect(cb, region)
			pc := int32(index)
			if err := g.drawRuns(cb, fr, q, opaque, 0, &pc, q.shadowMask(opaque, index, spotMats, pointMats), false); err != nil {
				return err
			}
		}
		render.EndTargetPass(cb, mp.shadowAtlas)
		g.timestamps.End(cb)
	}
	c := q.clear.premultiplied()
	render.BeginTargetPass(cb, render.PassDesc{Target: t.hdr, ClearColor: c, ClearDepth: 1})
	g.timestamps.Begin(cb, "opaque")
	if q.light.Background {
		// The sky first, under everything: it neither tests nor writes
		// depth. An image environment is looked up; the procedural sky is
		// evaluated from the frame block.
		render.SetViewport(cb, t.extent)
		rec := &g.rec
		rec.push = push2D{proj: q.camera.ViewProj(aspect).Inverse()}
		pipe := mp.skyParamPipe
		rec.set = q.uniforms.Sets[fr.Slot]
		if env := q.light.Environment; env != nil && env.cube != nil {
			pipe, rec.set = mp.skyPipe, env.set
			rec.push.frame = lin.V4(env.scale, 0, 0, 0)
		}
		vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
		vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &rec.set, 0, nil)
		vk.CmdPushConstants(cb, pipe.Layout, meshStages, 0, push2DSize, unsafe.Pointer(&rec.push))
		vk.CmdDraw(cb, 3, 1, 0, 0)
	}
	if err := g.drawRuns(cb, fr, q, seen, 0, nil, nil, false); err != nil {
		return err
	}
	g.drawSolid(cb, fr, q, seen, 0, t.extent)
	g.timestamps.End(cb)
	reflections := g.reflections(seen)
	if reflections || transmissive(seenBlended) {
		// Glass reads what is behind it and a reflection ray reads what the
		// screen already shows: snapshot the opaque scene, with blurred
		// mips for rough glass, then carry on into the same images.
		render.EndTargetPass(cb, t.hdr)
		render.CopyColorForSampling(cb, t.hdr.Color, t.scene)
		if reflections {
			g.timestamps.Begin(cb, "reflections")
			g.drawReflections(cb, fr, q, t)
			g.timestamps.End(cb)
		}
		render.BeginTargetPass(cb, render.PassDesc{Target: t.hdr, LoadColor: true, LoadDepth: true})
	}
	if seenOIT.len() > 0 {
		// Order-independent transparency: the translucent draws go into
		// their own two images, depth-tested against the opaque scene but
		// not against each other, and one fullscreen pass resolves them
		// over the scene. It runs after the reflection pass, which reads
		// the opaque scene alone.
		g.timestamps.Begin(cb, "transparency")
		render.EndTargetPass(cb, t.hdr)
		pass := render.PassDesc{
			Target: t.accum, Depth: t.hdr.Depth, LoadDepth: true,
			Extra: []*render.Image{t.reveal.Color}, ExtraClear: [][4]float32{{1, 1, 1, 1}},
		}
		render.BeginTargetPass(cb, pass)
		if err := g.drawRuns(cb, fr, q, seenOIT, uint32(opaque.len()), nil, nil, true); err != nil {
			return err
		}
		render.EndTargetPassDesc(cb, pass)
		g.timestamps.End(cb)
		g.timestamps.Begin(cb, "transparency resolve")
		render.BeginTargetPass(cb, render.PassDesc{Target: t.hdr, LoadColor: true, LoadDepth: true})
		render.SetViewport(cb, t.extent)
		g.post.fullscreen(cb, g.post.oit, t.oitSet, postPush{})
		g.timestamps.End(cb)
	}
	g.timestamps.Begin(cb, "blended")
	if err := g.drawRuns(cb, fr, q, seenBlended, uint32(opaque.len()+oit.len()), nil, nil, false); err != nil {
		return err
	}
	if err := g.drawDebugLines(cb, fr, q, aspect); err != nil {
		return err
	}
	g.timestamps.End(cb)
	render.EndTargetPass(cb, t.hdr)
	if len(q.decals) > 0 {
		g.timestamps.Begin(cb, "decals")
		g.drawDecals(cb, fr, q, t)
		g.timestamps.End(cb)
	}
	return nil
}

// drawSolid draws the outline shells and x-ray tints of draws that ask
// for them, after the opaque pass has marked the stencil. Skinned meshes
// are skipped: the solid vertex program does not skin.
func (g *Graphics) drawSolid(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue, draws drawList, first uint32, extent vk.VkExtent2D) {
	if draws.len() == 0 {
		return
	}
	mp := &g.meshes
	rec := &g.rec
	rec.offset = 0
	for _, pass := range []struct {
		pipe    *render.Pipeline
		outline bool
	}{{mp.outlinePipe, true}, {mp.xrayPipe, false}} {
		bound := false
		for i := range draws.len() {
			d := draws.at(i)
			if d.skinned || (pass.outline && d.mat.Outline <= 0) || (!pass.outline && d.mat.XRay == (Color{})) {
				continue
			}
			if !bound {
				bound = true
				vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pass.pipe.Handle)
				vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pass.pipe.Layout, 0, 1, &q.uniforms.Sets[fr.Slot], 0, nil)
				vk.CmdBindVertexBuffers(cb, 1, 1, &q.inst.buffers[q.inst.slot].Handle, &rec.offset)
			}
			rec.solid = solidPush{params: lin.V4(0, float32(extent.Width), float32(extent.Height), 0)}
			if pass.outline {
				c := d.mat.OutlineColor
				if c == (Color{}) {
					c = Color{0, 0, 0, 1}
				}
				rec.solid.color = lin.V4(c.R, c.G, c.B, c.A)
				rec.solid.params.X = d.mat.Outline
			} else {
				c := d.mat.XRay.premultiplied()
				rec.solid.color = lin.V4(c[0], c[1], c[2], c[3])
			}
			vk.CmdPushConstants(cb, pass.pipe.Layout, meshStages, 0, uint32(unsafe.Sizeof(rec.solid)), unsafe.Pointer(&rec.solid))
			vk.CmdBindVertexBuffers(cb, 0, 1, &d.mesh.vbuf.Handle, &rec.offset)
			vk.CmdBindIndexBuffer(cb, d.mesh.ibuf.Handle, 0, vk.VK_INDEX_TYPE_UINT32)
			vk.CmdDrawIndexed(cb, d.mesh.IndexCount, 1, 0, 0, first+uint32(i))
		}
	}
}

// drawDecals projects the queue's decals onto the finished opaque scene,
// reading the depth image and blending over the colour.
func (g *Graphics) drawDecals(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue, t *sceneTargets) {
	mp := &g.meshes
	pass := render.PassDesc{Target: t.hdr, LoadColor: true, NoDepth: true}
	render.BeginTargetPass(cb, pass)
	vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.decalPipe.Handle)
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.decalPipe.Layout, 0, 1, &t.depthSet, 0, nil)
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.decalPipe.Layout, 1, 1, &q.uniforms.Sets[fr.Slot], 0, nil)
	rec := &g.rec
	rec.offset = 0
	vk.CmdBindVertexBuffers(cb, 0, 1, &mp.decalMesh.vbuf.Handle, &rec.offset)
	vk.CmdBindIndexBuffer(cb, mp.decalMesh.ibuf.Handle, 0, vk.VK_INDEX_TYPE_UINT32)
	for _, d := range q.decals {
		inv := d.box.Inverse()
		rec.decal = decalPush{box: d.box, tint: lin.V4(d.tint.R*d.tint.A, d.tint.G*d.tint.A, d.tint.B*d.tint.A, d.tint.A)}
		for r := range 3 {
			rec.decal.invBox[r] = lin.V4(inv.At(r, 0), inv.At(r, 1), inv.At(r, 2), inv.At(r, 3))
		}
		vk.CmdPushConstants(cb, mp.decalPipe.Layout, meshStages, 0, uint32(unsafe.Sizeof(rec.decal)), unsafe.Pointer(&rec.decal))
		rec.set = d.tex.set
		vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.decalPipe.Layout, 2, 1, &rec.set, 0, nil)
		vk.CmdDrawIndexed(cb, mp.decalMesh.IndexCount, 1, 0, 0, 0)
	}
	render.EndTargetPassDesc(cb, pass)
}

func (mp *meshPass) destroy(g *Graphics) {
	dev := g.r.Device.Handle
	if mp.defaultShader != nil {
		mp.defaultShader.Destroy()
	}
	for _, p := range []*render.Pipeline{mp.skyPipe, mp.skyParamPipe, mp.outlinePipe, mp.xrayPipe, mp.decalPipe} {
		if p != nil {
			p.Destroy()
		}
	}
	if mp.decalMesh != nil {
		mp.decalMesh.Destroy()
	}
	if mp.quad != nil {
		mp.quad.Destroy()
	}
	if mp.shadowAtlas != nil {
		mp.shadowAtlas.Destroy()
	}
	if mp.blackCube != nil {
		mp.blackCube.Destroy()
	}
	if mp.flatNormal != nil {
		mp.flatNormal.Destroy()
	}
	if mp.black != nil {
		mp.black.Destroy()
	}
	if mp.shadowSamp != 0 {
		vk.VkDestroySampler(dev, mp.shadowSamp, nil)
	}
	if mp.shadowDesc != nil {
		mp.shadowDesc.Destroy()
	}
	if mp.materials != nil {
		mp.materials.Destroy()
	}
	if mp.uniformLayout != nil {
		mp.uniformLayout.Destroy()
	}
	if mp.jointLayout != nil {
		mp.jointLayout.Destroy()
	}
}
