package gfx

import (
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// drawQueue is everything queued for one output: the main frame or a
// render texture. Graphics always draws into its current queue.
type drawQueue struct {
	stream      stream2D
	draws       []meshDraw
	decals      []decal
	camera      Camera
	light       Light
	hasCam      bool
	points      []pointLight
	uniforms    *render.UniformSets
	inst        instanceStream
	joints      []lin.Mat4 // joint matrices for skinned draws this frame
	jointBuf    *render.StorageSets
	clear       Color
	viewW       float32
	viewH       float32
	pixelW      float32  // framebuffer width in pixels (render textures; the screen asks the swapchain)
	proj        lin.Mat4 // screen-space projection
	spriteProj  lin.Mat4 // projection for sprite draws right now
	cam2D       Camera2D
	hasCam2D    bool
	layer       int32
	clips       []lin.Rect // clip stack; the last entry applies
	shader      *Shader    // 2D shader in force, nil for the default
	blend       Blend
	colorMatrix *ColorMatrix // recolouring in force, nil for none
	lights      lights2D     // this frame's 2D lights
	lightsDirty bool
	xform       lin.Affine   // composed 2D transform in force
	xforms      []lin.Affine // transform stack below it
	skyCached   Sky          // the sky whose harmonics are in skySH
	skySH       [9]lin.Vec4
	lines       lineStream // debug lines drawn over the 3D scene
}

func (q *drawQueue) reset() {
	q.stream.reset()
	q.draws = q.draws[:0]
	q.decals = q.decals[:0]
	q.lines.reset()
	q.points = q.points[:0]
	q.joints = q.joints[:0]
	q.hasCam = false
	q.hasCam2D = false
	q.spriteProj = q.proj
	q.layer = 0
	q.clips = q.clips[:0]
	q.shader = nil
	q.blend = BlendAlpha
	q.colorMatrix = nil
	q.lights = lights2D{Ambient: lin.V4(1, 1, 1, 0)}
	q.lightsDirty = true
	q.xform = lin.Identity2()
	q.xforms = q.xforms[:0]
}

func (q *drawQueue) destroy() {
	q.stream.destroy()
	q.inst.destroy()
	q.lines.destroy()
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
	if q.uniforms, err = g.r.Device.NewUniformSets(frameUniformsSize, meshStages); err != nil {
		return nil, err
	}
	if q.jointBuf, err = g.r.Device.NewStorageSets(64*128, vk.VK_SHADER_STAGE_VERTEX_BIT); err != nil {
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
	} else {
		q.spriteProj = q.proj
	}
}

// subFrame is a render-texture pass queued for this frame.
type subFrame struct {
	rt    *RenderTexture
	queue *drawQueue
}
