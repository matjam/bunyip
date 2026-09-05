package gfx

import (
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// drawQueue is everything queued for one output: the main frame or a
// render texture. Graphics always draws into its current queue.
type drawQueue struct {
	stream       stream2D
	draws        []meshDraw
	order        []int32  // draws in draw order, as indices into draws
	keys         []uint64 // each draw's packed sort key, the sort's working set
	shaderIDs    idTable  // dense ids for the sort key
	uniformIDs   idTable
	setIDs       idTable
	meshIDs      idTable
	shadowVis    []bool // draws that reach the shadow map being recorded
	cascadeMats  [shadowCascades]lin.Mat4
	depthClamp   bool    // the shadow pipelines clamp depth rather than clip
	hasCasters   bool    // casterAlong holds a value
	casterAlong  float32 // how far the furthest caster is against the light
	visOpaque    int     // draws at the front of the opaque group the camera sees
	visBlended   int     // the same for the blended group
	decals       []decal
	camera       Camera
	light        Light
	hasCam       bool
	points       []pointLight
	clusters     clusterGrid // this frame's lights, sorted into the view's clusters
	spotSlots    []int32     // each light's spot shadow map, or -1
	pointSlots   []int32     // each light's cube shadow map slot, or -1
	uniforms     *render.UniformSets
	inst         instanceStream
	joints       []lin.Mat4 // joint matrices for skinned draws this frame
	jointBuf     *render.StorageSets
	clear        Color
	viewW        float32
	viewH        float32
	pixelW       float32  // framebuffer width in pixels (render textures; the screen asks the swapchain)
	proj         lin.Mat4 // screen-space projection
	spriteProj   lin.Mat4 // projection for sprite draws right now
	cam2D        Camera2D
	hasCam2D     bool
	visible      lin.Rect // the 2D camera's view in world units, for culling sprites
	layer        int32
	sortKey      float32    // order within the layer for later sprite draws
	clips        []lin.Rect // clip stack; the last entry applies
	shader       *Shader    // 2D shader in force, nil for the default
	blend        Blend
	colorMatrix  *ColorMatrix // recolouring in force, nil for none
	lights       lights2D     // this frame's 2D lights
	lightsDirty  bool
	shadows      bool         // some light this frame casts shadows
	occluders    []lin.Vec2   // this frame's occluder polygons, run after run
	occluderRuns []int32      // how many points each occluder has
	shadowTex    *Texture     // the polar shadow maps, one row per light
	shadowPix    []byte       // the strip's pixels, filled each frame
	shadowDist   []float32    // one light's distances, reused across lights
	xform        lin.Affine   // composed 2D transform in force
	xforms       []lin.Affine // transform stack below it
	skyCached    skyKey       // the sky whose harmonics are in skySH
	skySH        [9]lin.Vec4
	lines        lineStream         // debug lines drawn over the 3D scene
	parts        particleStream     // instanced particles, 2D and 3D
	probes       []*ReflectionProbe // this frame's reflection probes
	grid         *LightProbeGrid    // this frame's irradiance grid, nil for none
}

func (q *drawQueue) reset() {
	q.stream.reset()
	q.draws = q.draws[:0]
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
	q.colorMatrix = nil
	q.lights = lights2D{Ambient: lin.V4(1, 1, 1, 0)}
	q.lightsDirty = true
	q.shadows = false
	q.occluders = q.occluders[:0]
	q.occluderRuns = q.occluderRuns[:0]
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
