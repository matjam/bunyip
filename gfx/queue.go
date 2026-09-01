package gfx

import (
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/lin"
)

// drawQueue is everything queued for one output: the main frame or a
// render texture. Graphics always draws into its current queue.
type drawQueue struct {
	sprites    spriteBatch
	draws      []meshDraw
	camera     Camera
	light      Light
	hasCam     bool
	points     []pointLight
	uniforms   *render.UniformSets
	inst       instanceStream
	clear      Color
	viewW      float32
	viewH      float32
	proj       lin.Mat4 // screen-space projection
	spriteProj lin.Mat4 // projection for sprite draws right now
	cam2D      Camera2D
	hasCam2D   bool
	layer      int32
}

func (q *drawQueue) reset() {
	q.sprites.reset()
	q.draws = q.draws[:0]
	q.points = q.points[:0]
	q.hasCam = false
	q.hasCam2D = false
	q.spriteProj = q.proj
	q.layer = 0
}

func (q *drawQueue) destroy() {
	q.sprites.destroy()
	q.inst.destroy()
	if q.uniforms != nil {
		q.uniforms.Destroy()
		q.uniforms = nil
	}
}

func (g *Graphics) newQueue(w, h float32) (*drawQueue, error) {
	q := &drawQueue{light: defaultLight()}
	var err error
	if q.uniforms, err = g.R.Device.NewUniformSets(frameUniformsSize, meshStages); err != nil {
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
