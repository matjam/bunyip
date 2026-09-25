package gfx

import (
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// drawQueue is everything queued for one output: the main frame or a
// render texture. Graphics always draws into its current queue.
type drawQueue struct {
	stream stream2D
	draws  []meshDraw
	// mats is the frame's distinct materials, which draws name by index;
	// matSlots is the hash table over it, holding an index plus one, and
	// lastMat the index plus one of the material found last.
	mats     []frameMaterial
	matSlots []int32
	lastMat  int32
	prepGen  uint32 // counts preparations, so a material's sets are resolved once per preparation
	frame    uint64 // counts resets, so a static batch knows when its material indices are stale
	// expandedAt is one more than how many draws the queue held before
	// prepareDraws added the static batches' items, or zero before the
	// frame's first preparation.
	expandedAt  int
	prevs       []lin.Mat4  // the previous transforms of draws that moved
	morphs      []morphDraw // the morph blocks of draws with GPU morph targets
	order       []int32     // draws in draw order, as indices into draws
	keys        []uint64    // each draw's packed sort key, the sort's working set
	keyTmp      []uint64    // the radix sort's second buffer
	sortedKeys  []uint64    // the sorted keys, in keys or keyTmp, until shadowOrder reads them
	shadowKeys  []uint64    // the opaque draws' keys for the shadow order
	shadowIDs   []int32     // the opaque draws in the shadow order
	shaderIDs   idTable     // dense ids for the sort key
	uniformIDs  idTable
	setIDs      idTable
	meshIDs     idTable
	casters     shadowCasters // the opaque draws' spheres and each shadow map's draws
	cascadeMats [shadowCascades]lin.Mat4
	shadow      shadowLights // the frame's shadowed spot and point lights
	// volumes is what each of the frame's shadow maps can take casters
	// from, which static batches walk their hidden subtrees against.
	volumes    []shadowVolume
	volumesArr [shadowCascades + maxSpotShadows + maxPointShadows]shadowVolume
	// jitter is this frame's sub-pixel projection offset in clip units,
	// zero unless temporal anti-aliasing is on, and projJ, viewProjJ and
	// invViewProjJ are the matrices the scene pass rasterises with once it
	// is applied. prevViewProj is the previous frame's view-projection
	// without the jitter, which the velocity and resolve passes measure
	// motion against; hasPrevVP says a previous frame exists.
	jitter       lin.Vec2
	projJ        lin.Mat4
	viewProjJ    lin.Mat4
	invViewProjJ lin.Mat4
	prevViewProj lin.Mat4
	hasPrevVP    bool
	hasMoved     bool    // some draw this frame carries a previous transform
	depthClamp   bool    // the shadow pipelines clamp depth rather than clip
	hasCasters   bool    // casterAlong holds a value
	casterAlong  float32 // how far the furthest caster is against the light
	sorted       []int32 // scratch for partitionOIT: the draws that stay sorted
	visOpaque    int     // draws at the front of the opaque group the camera sees
	visOIT       int     // the same for the order-independent group
	visBlended   int     // the same for the blended group
	decals       []decal
	camera       Camera
	light        Light
	hasCam       bool
	points       []pointLight
	clusters     clusterGrid // this frame's lights, sorted into the view's clusters
	// clustersEmpty says which slots' cluster tables were last written
	// with no lights, so a frame without lights need not write them again.
	clustersEmpty [render.FramesInFlight]bool
	spotSlots     []int32 // each light's spot shadow map, or -1
	pointSlots    []int32 // each light's cube shadow map slot, or -1
	uniforms      *render.UniformSets
	inst          instanceStream
	joints        []lin.Mat4 // joint matrices for skinned draws this frame
	jointBuf      *render.StorageSets
	clear         Color
	// out is the attachment set of the pass this queue's composite and 2D
	// stream land in: the zero value for the screen, a render texture's
	// own colour format, depth and sample count otherwise.
	out          outKey
	viewW        float32
	viewH        float32
	rootW, rootH float32    // coordinate space of the whole target
	layout       lin.Affine // current local view coordinates to root view coordinates
	pixelW       float32    // framebuffer width in pixels (render textures; the screen asks the swapchain)
	proj         lin.Mat4   // screen-space projection
	spriteProj   lin.Mat4   // projection for sprite draws right now
	cam2D        Camera2D
	hasCam2D     bool
	visible      lin.Rect // the 2D camera's view in world units, for culling sprites
	layer        int32
	sortKey      float32    // order within the layer for later sprite draws
	clips        []lin.Rect // clip stack; the last entry applies
	shader       *Shader    // 2D shader in force, nil for the default
	blend        Blend
	customBlend  customBlend
	stencil2D    stencil2D
	maskDepth    uint8
	maskTests    uint8        // completed masks required by the current phase
	maskWrites   uint8        // mask bits the current phase's drawing constructs
	colorMatrix  *ColorMatrix // recolouring in force, nil for none
	lights       lights2D     // this frame's 2D lights
	lightsDirty  bool
	shadows      bool       // some light this frame casts shadows
	occluders    []lin.Vec2 // this frame's 2D occluder polygons, run after run
	occluderRuns []int32    // how many points each occluder has
	// meshOccluders are the meshes AddOccluder3D marked as blocking the
	// camera's view, rasterised into the software occlusion buffer.
	meshOccluders []meshOccluder
	// batches are the static batches DrawBatch queued, expanded into
	// draws once the frustum and the occluders are known.
	batches    []*StaticBatch
	shadowTex  *Texture     // the polar shadow maps, one row per light
	shadowPix  []byte       // the strip's pixels, filled each frame
	shadowDist []float32    // one light's distances, reused across lights
	xform      lin.Affine   // composed 2D transform in force
	xforms     []lin.Affine // transform stack below it
	skyCached  skyKey       // the sky whose harmonics are in skySH
	skySH      [9]lin.Vec4
	lines      lineStream         // debug lines drawn over the 3D scene
	parts      particleStream     // instanced particles, 2D and 3D
	probes     []*ReflectionProbe // this frame's reflection probes
	grid       *LightProbeGrid    // this frame's irradiance grid, nil for none
}

func (q *drawQueue) reset() {
	q.stream.reset()
	q.draws = q.draws[:0]
	q.resetMaterials()
	q.frame++
	q.expandedAt = 0
	q.prevs = q.prevs[:0]
	q.morphs = q.morphs[:0]
	q.decals = q.decals[:0]
	q.lines.reset()
	q.parts.reset()
	q.points = q.points[:0]
	q.probes = q.probes[:0]
	q.grid = nil
	q.joints = q.joints[:0]
	q.hasCam = false
	q.hasCam2D = false
	q.spriteProj = q.proj
	q.layer = 0
	q.sortKey = 0
	q.clips = q.clips[:0]
	q.shader = nil
	q.blend = BlendAlpha
	q.customBlend = customBlend{}
	q.stencil2D = stencil2D{}
	q.maskDepth = 0
	q.maskTests, q.maskWrites = 0, 0
	q.colorMatrix = nil
	q.lights = lights2D{Ambient: lin.V4(1, 1, 1, 0)}
	q.lightsDirty = true
	q.shadows = false
	q.occluders = q.occluders[:0]
	q.occluderRuns = q.occluderRuns[:0]
	q.meshOccluders = q.meshOccluders[:0]
	q.batches = q.batches[:0]
	q.xform = lin.Identity2()
	q.xforms = q.xforms[:0]
}

func (q *drawQueue) destroy() {
	q.stream.destroy()
	q.inst.destroy()
	q.lines.destroy()
	q.parts.destroy()
	if q.shadowTex != nil {
		q.shadowTex.Destroy()
		q.shadowTex = nil
	}
	if q.uniforms != nil {
		q.uniforms.Destroy()
		q.uniforms = nil
	}
	if q.jointBuf != nil {
		q.jointBuf.Destroy()
		q.jointBuf = nil
	}
}

func (g *Graphics) newQueue(w, h float32) (*drawQueue, error) {
	q := &drawQueue{light: defaultLight(), xform: lin.Identity2(), pixelW: w}
	var err error
	// Binding 1 of the same set is the light probe grid's harmonics, which
	// no uniform block is large enough to hold; bindings 2 and up are the
	// frame's light records and the cluster grid's tables.
	if q.uniforms, err = g.r.Device.NewFrameSets(frameUniformsSize, gridStorageSize, frameStorage(), meshStages); err != nil {
		return nil, err
	}
	if q.jointBuf, err = g.r.Device.NewStorageSets(64*128, vk.VK_SHADER_STAGE_VERTEX_BIT); err != nil {
		q.destroy()
		return nil, err
	}
	// The shadow strip is made with the queue rather than on the first
	// shadowed light, so a frame never creates a texture and its
	// descriptor set while its draws are being recorded.
	if q.shadowTex, err = g.newShadowTexture(); err != nil {
		q.destroy()
		return nil, err
	}
	q.setView(w, h)
	return q, nil
}

func (q *drawQueue) setView(w, h float32) {
	q.viewW, q.viewH = w, h
	q.rootW, q.rootH = w, h
	q.layout = lin.Identity2()
	q.proj = lin.Ortho2D(w, h)
	if q.hasCam2D {
		q.spriteProj = q.proj.Mul(q.cam2D.Matrix(w, h))
		q.visible = q.cam2D.VisibleRect(w, h)
	} else {
		q.spriteProj = q.proj
	}
}

// subFrame is a render-texture pass queued for this frame.
type subFrame struct {
	rt    *RenderTexture
	queue *drawQueue
}
