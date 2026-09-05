package gfx

import (
	"fmt"
	"image"
	"math"
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Graphics is the drawing context for one window. The engine opens a
// frame, the Draw* calls queue work, and the engine submits it. Obtain
// Graphics from bunyip.Context; its zero value is not usable. Use it and
// its GPU resources only on the game goroutine.
type Graphics struct {
	r            *render.Renderer
	descriptors  *render.DescriptorSets // five samplers: a texture and a shader's image0..3
	uniforms     *render.DynamicUniforms
	arena        *render.Arena // this frame's shader uniform blocks
	imageSets    map[[5]*Texture]vk.VkDescriptorSet
	nearest      vk.VkSampler
	linear       vk.VkSampler
	nearestRep   vk.VkSampler
	linearRep    vk.VkSampler
	spriteShader *Shader            // the default 2D shader
	sdfShader    *Shader            // distance-field text
	matrixShader *Shader            // sprites through a colour matrix
	litShader    *Shader            // normal-mapped sprites under 2D lights
	staging      *render.Staging    // this frame's upload arena, one per frame slot
	timestamps   *render.Timestamps // GPU pass timings; nil where the device has no timestamp queries
	gpuSpans     []GPUSpan          // the newest GPU timings, refilled each frame
	waitBase     uint64             // the device's wait count when the frame began
	stats        FrameStats         // counts for the frame being recorded
	lastStats    FrameStats         // the last finished frame's counts
	meshes       meshPass
	post         postPass
	particles    particlePass
	white        *Texture
	frame        *render.Frame
	frameNo      uint64
	time         float32
	main         *drawQueue // the screen
	cur          *drawQueue // where Draw* calls land
	subFrames    []subFrame
	// retire holds what each frame slot destroyed or replaced, freed at
	// that slot's next begin, once its fence has been waited on.
	retire     [render.FramesInFlight][]func()
	scratch    []vertex2D
	pathSubs   []subpath  // flattened sub-paths, reused by FillPath and StrokePath
	pathFill   filler     // likewise the scanline filler
	pathStroke stroker    // and the stroke expander
	linePipe   *pipeCache // debug lines over the 3D scene, per sample count
	// sceneOut is the attachment set of the HDR pass being recorded: the
	// pipelines drawn into it are built per sample count, and one queue
	// is recorded at a time.
	sceneOut      outKey
	dbgFont       *Font // the built-in font, made on first use
	dbgFontFailed bool
	occ           occlusionBuffer // the software occlusion depth buffer, reused every frame
	rec           recordScratch   // long-lived arguments for the recording commands
	viewport      vk.VkRect2D     // the main output's pixel rectangle; zero means the whole window
	res           resources       // the live resources a debug view lists
}

// SetViewport limits the main output to a pixel rectangle: the 2D view
// maps onto it, the 3D scene renders at its size, and the window outside
// it stays black. The engine sets it from Config's view size and scaling
// policy; a zero rect means the whole window.
func (g *Graphics) SetViewport(r lin.Rect) error {
	vp := vk.VkRect2D{Offset: vk.VkOffset2D{X: int32(r.X), Y: int32(r.Y)}, Extent: vk.VkExtent2D{Width: uint32(r.W), Height: uint32(r.H)}}
	if r.W <= 0 || r.H <= 0 {
		vp = vk.VkRect2D{}
	}
	if vp == g.viewport {
		return nil
	}
	g.viewport = vp
	return g.rebuildMain()
}

// Viewport returns the main output's pixel rectangle.
func (g *Graphics) Viewport() lin.Rect {
	vp := g.viewport
	if vp.Extent.Width == 0 {
		vp.Extent = g.r.Swapchain.Extent
	}
	return lin.R(float32(vp.Offset.X), float32(vp.Offset.Y), float32(vp.Extent.Width), float32(vp.Extent.Height))
}

// mainExtent is the pixel size the main scene renders at.
func (g *Graphics) mainExtent() vk.VkExtent2D {
	if g.viewport.Extent.Width > 0 {
		return g.viewport.Extent
	}
	return g.r.Swapchain.Extent
}

// rebuildMain sizes the main scene targets to the viewport when it changed.
func (g *Graphics) rebuildMain() error {
	ext := g.mainExtent()
	samples := g.sceneSamples()
	if g.post.main != nil && g.post.main.extent == ext && g.post.main.samples == samples {
		return nil
	}
	if old := g.post.main; old != nil {
		g.post.main = nil
		g.deferDestroy(func() { old.destroy(g) })
	}
	var err error
	g.post.main, err = g.newSceneTargets(ext, samples)
	return err
}

// rebuildScene gives a render texture scene targets at the sample count
// the post settings now ask for. It runs at the start of a frame's
// rendering, where no pass is open yet and the old targets can go on the
// retire list rather than costing a wait.
func (g *Graphics) rebuildScene(rt *RenderTexture) error {
	samples := g.sceneSamples()
	if rt.scene != nil && rt.scene.samples == samples {
		return nil
	}
	if old := rt.scene; old != nil {
		rt.scene = nil
		g.deferDestroy(func() { old.destroy(g) })
	}
	var err error
	rt.scene, err = g.newSceneTargets(vk.VkExtent2D{Width: uint32(rt.Width), Height: uint32(rt.Height)}, samples)
	return err
}

// pixelRect maps a view-space clip rectangle into the viewport's pixels.
// Both edges round the same way, so a rectangle thinner than a pixel
// (which is how a disjoint clip is represented) covers no pixels rather
// than leaking a one-pixel sliver.
func pixelRect(vp vk.VkRect2D, clip lin.Rect, sx, sy float32) vk.VkRect2D {
	x0 := vp.Offset.X + clipCoord(clip.X*sx)
	y0 := vp.Offset.Y + clipCoord(clip.Y*sy)
	x1 := vp.Offset.X + clipCoord((clip.X+clip.W)*sx)
	y1 := vp.Offset.Y + clipCoord((clip.Y+clip.H)*sy)
	x0, y0 = max(x0, vp.Offset.X), max(y0, vp.Offset.Y)
	x1 = min(x1, vp.Offset.X+int32(vp.Extent.Width))
	y1 = min(y1, vp.Offset.Y+int32(vp.Extent.Height))
	return vk.VkRect2D{Offset: vk.VkOffset2D{X: x0, Y: y0}, Extent: vk.VkExtent2D{Width: uint32(max(x1-x0, 0)), Height: uint32(max(y1-y0, 0))}}
}

// clipCoord floors a pixel coordinate into an int32, clamping the huge
// values a "clip to nothing in particular" rectangle carries instead of
// letting the conversion wrap.
func clipCoord(v float32) int32 {
	const limit = 1 << 30
	return int32(math.Floor(float64(lin.Clamp(v, -limit, limit))))
}

// deferDestroy frees a GPU object once the frame that may still use it
// has finished. Inside a frame the work goes on that slot's retire list,
// run at the slot's next begin; outside one it runs at once, after
// waiting for the device, because the last frame submitted may still be
// reading the object.
func (g *Graphics) deferDestroy(free func()) {
	if fr := g.frame; fr != nil {
		g.retire[fr.Slot] = append(g.retire[fr.Slot], free)
		return
	}
	_ = g.r.Device.WaitIdle()
	free()
}

// freeRetired runs a slot's retire list. The slot's fence has been
// waited on by then, and the queue runs submissions in order, so every
// earlier frame has finished too.
func (g *Graphics) freeRetired(slot int) {
	// Freeing one object can retire another, and the frame is open by
	// now, so those land on this same list; the index walk picks them up.
	for i := 0; i < len(g.retire[slot]); i++ {
		g.retire[slot][i]()
	}
	g.retire[slot] = g.retire[slot][:0]
}

// stage copies data into this frame's upload arena. It returns the
// buffer and offset to record a copy from; the caller must record that
// copy into the open frame's command buffer.
func (g *Graphics) stage(data []byte) (*render.Buffer, vk.VkDeviceSize, error) {
	return g.staging.Alloc(g.frame.Slot, data)
}

// growStream gives every frame slot a fresh host-visible buffer of size
// bytes, for a per-frame vertex stream that outgrew the ones it had. The
// old buffers go on the retire list, since a frame in flight may still
// be drawing from them, so growing costs no wait.
func (g *Graphics) growStream(bufs *[render.FramesInFlight]*render.Buffer, size vk.VkDeviceSize) error {
	var fresh [render.FramesInFlight]*render.Buffer
	for i := range fresh {
		buf, err := g.r.Device.NewBuffer(size, vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			for _, b := range fresh {
				if b != nil {
					b.Destroy()
				}
			}
			return err
		}
		fresh[i] = buf
	}
	old := *bufs
	*bufs = fresh
	g.deferDestroy(func() {
		for _, b := range old {
			if b != nil {
				b.Destroy()
			}
		}
	})
	return nil
}

// setup records commands that prepare a resource. Inside a frame they go
// into the frame's command buffer, before any pass; outside one they are
// submitted on their own and waited for.
func (g *Graphics) setup(record func(cb vk.VkCommandBuffer)) error {
	if fr := g.frame; fr != nil {
		record(fr.CB)
		return nil
	}
	return g.r.Device.OneShot(record)
}

// newGraphics builds the drawing context over a renderer. The engine
// loop calls it through internal/hook.
func newGraphics(r *render.Renderer) (*Graphics, error) {
	g := &Graphics{r: r, imageSets: map[[5]*Texture]vk.VkDescriptorSet{}}
	g.staging = r.Device.NewStaging()
	// A device without timestamp queries leaves this nil, and every call
	// on it does nothing, so the frame records no timings and FrameStats
	// reports none.
	g.timestamps = r.Device.NewTimestamps()
	var err error
	if g.descriptors, err = r.Device.NewSamplerDescriptors(5, 2048); err != nil {
		return nil, err
	}
	if g.nearest, err = r.Device.NewSampler(false); err != nil {
		return nil, err
	}
	if g.linear, err = r.Device.NewSampler(true); err != nil {
		return nil, err
	}
	if g.nearestRep, err = r.Device.NewSamplerRepeat(false, true); err != nil {
		return nil, err
	}
	if g.linearRep, err = r.Device.NewSamplerRepeat(true, true); err != nil {
		return nil, err
	}
	if g.uniforms, err = r.Device.NewDynamicUniforms(maxUniformBlock, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT); err != nil {
		return nil, err
	}
	g.arena = g.uniforms.NewArena()
	if g.white, err = g.newTexture(1, 1, []byte{255, 255, 255, 255}, TextureOptions{}); err != nil {
		return nil, err
	}
	g.spriteShader = &Shader{g: g, frag: shaders.SpriteFrag, pipes: map[pipeKey]*render.Pipeline{}}
	g.sdfShader = &Shader{g: g, frag: shaders.SDFFrag, pipes: map[pipeKey]*render.Pipeline{}}
	g.matrixShader = &Shader{g: g, frag: shaders.MatrixFrag, pipes: map[pipeKey]*render.Pipeline{}}
	g.litShader = &Shader{g: g, frag: shaders.LitFrag, pipes: map[pipeKey]*render.Pipeline{}}
	for _, s := range []*Shader{g.spriteShader, g.sdfShader, g.matrixShader, g.litShader} {
		if _, err := s.pipeline(pipeKey{}); err != nil {
			return nil, err
		}
	}
	if err := g.initMeshPass(); err != nil {
		return nil, err
	}
	if err := g.initPost(); err != nil {
		return nil, err
	}
	if err := g.initReflections(); err != nil {
		return nil, err
	}
	if err := g.initLines(); err != nil {
		return nil, err
	}
	ext := r.Swapchain.Extent
	if g.main, err = g.newQueue(float32(ext.Width), float32(ext.Height)); err != nil {
		return nil, err
	}
	g.cur = g.main
	return g, nil
}

// spriteVert is the vertex program shared by every 2D pipeline.
func (g *Graphics) spriteVert() []byte { return shaders.SpriteVert }

// resize tells the renderer the framebuffer changed size, in pixels.
func (g *Graphics) resize(width, height int) { g.r.Resize(width, height) }

// SetView sets the 2D coordinate space: (0,0) top-left to (width,height)
// bottom-right, whatever the framebuffer's pixel size.
func (g *Graphics) SetView(width, height float32) { g.main.setView(width, height) }

// View returns the current 2D coordinate space size.
func (g *Graphics) View() (float32, float32) { return g.main.viewW, g.main.viewH }

// begin starts a frame cleared to clear. ok is false when the swapchain
// was rebuilt and the frame should be skipped.
func (g *Graphics) begin(clear Color) (ok bool, err error) {
	g.frame, ok, err = g.r.BeginFrame()
	if err != nil || !ok {
		return ok, err
	}
	g.frameNo++
	g.arena.Reset()
	// BeginFrame waited on this slot's fence, so everything the last
	// frame in this slot staged or retired is finished with.
	g.staging.Begin(g.frame.Slot)
	g.freeRetired(g.frame.Slot)
	g.waitBase = g.r.Device.Waits()
	// Resetting the slot's queries publishes the timings the frame that
	// used this slot recorded, which have landed because BeginFrame
	// waited on that slot's fence.
	g.timestamps.Reset(g.frame.CB, g.frame.Slot)
	g.stats = FrameStats{}
	g.gpuSpans = g.gpuSpans[:0]
	for _, s := range g.timestamps.Spans() {
		g.gpuSpans = append(g.gpuSpans, GPUSpan{Name: s.Name, MS: s.MS})
	}
	g.stats.GPU = g.gpuSpans
	g.stats.GPUFrameMS = g.timestamps.FrameMS()
	g.main.reset()
	g.main.clear = clear
	g.cur = g.main
	g.subFrames = g.subFrames[:0]
	return true, nil
}

// Draw queues a sprite. A nil texture draws with a 1x1 white texture, so a
// coloured rectangle is a tinted sprite.
func (g *Graphics) Draw(tex *Texture, s Sprite) {
	if tex == nil {
		tex = g.white
	}
	if s.UV1 == (lin.Vec2{}) {
		s.UV1 = lin.V2(1, 1)
	}
	if s.Color == (Color{}) {
		s.Color = White
	}
	if q := g.cur; q.hasCam2D && !spriteVisible(s, q.xform, q.visible) {
		g.stats.Culled2D++
		return
	}
	if s.FlipX {
		s.UV0.X, s.UV1.X = s.UV1.X, s.UV0.X
	}
	if s.FlipY {
		s.UV0.Y, s.UV1.Y = s.UV1.Y, s.UV0.Y
	}
	g.scratch = spriteVertices(s, g.scratch[:0])
	g.emitFiltered(tex, g.scratch, s.Filter)
}

// spriteVisible reports whether any of a sprite can lie inside a
// world-space view. It tests the sprite's four corners, mapped through
// the 2D transform in force, against the view rectangle by separating
// axes, so a long thin rotated sprite is culled as soon as its own quad
// clears the view rather than when the circle around it does.
func spriteVisible(s Sprite, xform lin.Affine, view lin.Rect) bool {
	p := spriteCorners(s)
	if !xform.IsIdentity() {
		for i := range p {
			p[i] = xform.Apply(p[i])
		}
	}
	// The view's own axes first: they reject everything well clear of it.
	lo, hi := p[0], p[0]
	for _, c := range p[1:] {
		lo = lin.V2(min(lo.X, c.X), min(lo.Y, c.Y))
		hi = lin.V2(max(hi.X, c.X), max(hi.Y, c.Y))
	}
	if hi.X < view.X || lo.X > view.X+view.W || hi.Y < view.Y || lo.Y > view.Y+view.H {
		return false
	}
	// Then the quad's two edge normals, which separate a rotated or
	// sheared sprite that the axis-aligned test alone keeps.
	corners := [4]lin.Vec2{
		lin.V2(view.X, view.Y), lin.V2(view.X+view.W, view.Y),
		lin.V2(view.X+view.W, view.Y+view.H), lin.V2(view.X, view.Y+view.H),
	}
	for _, e := range [2]lin.Vec2{p[1].Sub(p[0]), p[3].Sub(p[0])} {
		axis := lin.V2(-e.Y, e.X)
		if axis == (lin.Vec2{}) {
			continue // a degenerate sprite has no such axis
		}
		qlo, qhi := projectPoints(axis, p[:])
		vlo, vhi := projectPoints(axis, corners[:])
		if qhi < vlo || qlo > vhi {
			return false
		}
	}
	return true
}

// projectPoints returns the range points cover along an axis.
func projectPoints(axis lin.Vec2, points []lin.Vec2) (lo, hi float32) {
	lo = axis.X*points[0].X + axis.Y*points[0].Y
	hi = lo
	for _, p := range points[1:] {
		d := axis.X*p.X + axis.Y*p.Y
		lo, hi = min(lo, d), max(hi, d)
	}
	return lo, hi
}

// DrawTriangles queues textured triangles: three vertices each, with
// positions in view units, texture coordinates in 0..1 and a tint. It is
// the primitive under sprites and paths, for games that build their own
// geometry.
func (g *Graphics) DrawTriangles(tex *Texture, verts []Vertex2D) {
	g.scratch = g.scratch[:0]
	for _, v := range verts {
		c := v.Color
		if c == (Color{}) {
			c = White
		}
		g.scratch = append(g.scratch, vertex2D{pos: v.Pos, uv: v.UV, color: c.premultiplied()})
	}
	g.scratch = g.scratch[:len(g.scratch)/3*3]
	g.emit(tex, g.scratch)
}

// Vertex2D is one corner of a triangle for DrawTriangles.
type Vertex2D struct {
	Pos   lin.Vec2
	UV    lin.Vec2
	Color Color // zero means white
}

// DrawIndexed queues textured triangles from vertices and indices,
// three indices per triangle, for meshes whose vertices are shared.
func (g *Graphics) DrawIndexed(tex *Texture, verts []Vertex2D, indices []uint32) {
	g.scratch = g.scratch[:0]
	// A triangle with an index out of range is dropped whole, so the
	// ones after it keep their vertices.
	for t := 0; t+2 < len(indices); t += 3 {
		i0, i1, i2 := indices[t], indices[t+1], indices[t+2]
		if int(i0) >= len(verts) || int(i1) >= len(verts) || int(i2) >= len(verts) {
			continue
		}
		for _, i := range [3]uint32{i0, i1, i2} {
			v := verts[i]
			c := v.Color
			if c == (Color{}) {
				c = White
			}
			g.scratch = append(g.scratch, vertex2D{pos: v.Pos, uv: v.UV, color: c.premultiplied()})
		}
	}
	g.emit(tex, g.scratch)
}

// FrameStats counts what a frame cost the GPU, for the debug overlay and
// a draw-call budget.
type FrameStats struct {
	Draws2D    int // 2D draw calls after batching
	Vertices2D int // 2D vertices drawn
	Draws3D    int // mesh draw calls after instancing, all passes
	Instances  int // mesh instances drawn in the main pass
	Culled     int // mesh draws outside the camera's view, skipped in the main pass
	// Occluded counts the mesh draws inside the camera's view that the
	// software occlusion buffer found behind an occluder, which is a
	// subset of Culled. It is zero in a frame with no AddOccluder3D.
	Occluded int
	// CullTests counts the bounding volume tests culling ran: one per
	// queued draw, plus one per hierarchy node a static batch visited.
	// A batch shows up as far fewer tests than it holds items.
	CullTests int
	// ShadowDraws counts the mesh instances recorded into the shadow maps,
	// summed over the cascades, the shadowed spot lights and the cube
	// faces of the shadowed point lights. A caster is only recorded into
	// the maps its bounds reach, so this falls as lights and casters
	// spread out.
	ShadowDraws int
	// Culled2D counts sprites outside the 2D camera's view that were
	// dropped before reaching the vertex stream.
	Culled2D int
	// LightsDropped counts point and spot lights added past MaxLights,
	// which a frame keeps none of; a nonzero count means the scene should
	// add its nearest lights first.
	LightsDropped int
	// Waits counts the times the frame stopped and waited for the GPU to
	// go idle. Uploads and destroys inside a frame go through the staging
	// arena and the retire ring, and every per-frame buffer grows through
	// the retire ring too, so a running game reports zero; a nonzero
	// count means something stalled the whole pipeline, such as a
	// Texture.Read or a resource destroyed outside a frame.
	Waits int
	// Lights counts the point and spot lights the frame kept, out of
	// MaxLights, whatever part of the view each one reaches. The
	// directional light is not counted: every frame has one.
	Lights int
	// Particles counts the instances drawn by DrawParticles and
	// DrawParticles3D. Each batch is one draw call however many
	// instances it holds, counted in Draws2D or Draws3D.
	Particles int
	// ProbesDropped counts reflection probes added past MaxProbes, which
	// a frame keeps none of.
	ProbesDropped int
	// GPU is how long the GPU spent in each pass, in the order the passes
	// were recorded: the shadow atlas, the opaque and blended scene, the
	// reflections, the decals, bloom, ambient occlusion, the composite,
	// the anti-alias resolve and the 2D stream. A pass that runs for a
	// render texture as well as the screen is summed into one entry. It is
	// empty on a device without timestamp queries or before results arrive.
	// Available queries can still report zero durations when the device's
	// timestamp resolution cannot distinguish the pass endpoints. MoltenVK
	// without Metal counter sampling can report zero for every pass. The
	// figures come from queries read back without waiting, so they describe
	// a frame two frames back, and the slice is reused every frame; copy it
	// to keep it.
	GPU []GPUSpan
	// GPUFrameMS is the GPU time from the frame's first pass to the end
	// of its last, so it covers the gaps between passes as well. It is zero
	// when no results are available or the timestamps cannot resolve a
	// duration, including on some devices that emulate timestamp queries.
	GPUFrameMS float64
}

// GPUSpan is one pass of a frame and the milliseconds the GPU spent in
// it, as FrameStats.GPU reports it.
type GPUSpan struct {
	Name string
	MS   float64
}

// Stats returns the last finished frame's counts.
func (g *Graphics) Stats() FrameStats { return g.lastStats }

// PushClip limits later sprite drawing to a view-space rectangle,
// intersected with any enclosing clip. Pair with PopClip.
func (g *Graphics) PushClip(r lin.Rect) {
	q := g.cur
	if n := len(q.clips); n > 0 {
		r = intersectClip(q.clips[n-1], r)
	}
	q.clips = append(q.clips, r)
}

// Clip runs draw with sprites clipped to the rectangle, the closure form
// of PushClip and PopClip.
func (g *Graphics) Clip(r lin.Rect, draw func()) {
	g.PushClip(r)
	draw()
	g.PopClip()
}

// PopClip restores the clip rectangle in force before the matching PushClip.
func (g *Graphics) PopClip() {
	q := g.cur
	if len(q.clips) > 0 {
		q.clips = q.clips[:len(q.clips)-1]
	}
}

// intersectClip narrows a clip by another; a disjoint pair clips
// everything, which must stay distinct from the zero "no clip" rect.
func intersectClip(a, b lin.Rect) lin.Rect {
	r := a.Intersect(b)
	if r.Empty() {
		return lin.Rect{X: r.X, Y: r.Y, W: 0.001, H: 0.001}
	}
	return r
}

// FillRect queues a solid rectangle.
func (g *Graphics) FillRect(x, y, w, h float32, c Color) {
	g.Draw(nil, Sprite{Pos: lin.V2(x, y), Size: lin.V2(w, h), Color: c})
}

// DrawTexture queues a texture at its own size.
func (g *Graphics) DrawTexture(tex *Texture, x, y float32) {
	g.Draw(tex, Sprite{Pos: lin.V2(x, y), Size: lin.V2(float32(tex.Width), float32(tex.Height))})
}

// end flushes queued work, submits and presents. With capture it returns the frame.
func (g *Graphics) end(capture bool) (*image.RGBA, error) {
	if g.frame == nil {
		return nil, fmt.Errorf("gfx: end without begin")
	}
	// The 2D shadow maps are built and uploaded before any pass is
	// recorded, so every lit draw in the frame samples the same maps.
	// This is also where a changed sample count takes effect, for the
	// same reason: no pass is open yet.
	for _, sf := range g.subFrames {
		if err := g.rebuildScene(sf.rt); err != nil {
			return nil, err
		}
		if err := g.buildShadows2D(sf.queue); err != nil {
			return nil, err
		}
	}
	if err := g.rebuildMain(); err != nil {
		return nil, err
	}
	if err := g.buildShadows2D(g.main); err != nil {
		return nil, err
	}
	fr := g.frame
	// The frame stays open while the passes are recorded, so anything
	// retired or staged while recording still lands on this slot.
	defer func() { g.frame = nil }()
	g.cur = g.main
	if err := g.uniforms.Write(fr.Slot, g.arena.Bytes()); err != nil {
		return nil, err
	}
	for _, sf := range g.subFrames {
		if err := g.renderQueue(fr, sf.queue, sf.rt.scene, sf.rt.target); err != nil {
			return nil, err
		}
	}
	if err := g.renderQueue(fr, g.main, g.post.main, nil); err != nil {
		return nil, err
	}
	img, err := g.r.EndFrame(fr, capture)
	g.stats.Waits = int(g.r.Device.Waits() - g.waitBase)
	g.lastStats = g.stats
	return img, err
}

// renderQueue draws one queue: the 3D scene through the post chain, then
// the 2D stream, into target (a render texture) or the swapchain when nil.
func (g *Graphics) renderQueue(fr *render.Frame, q *drawQueue, t *sceneTargets, target *render.Target) error {
	cb := fr.CB
	s := g.post.settings
	has3D := len(q.draws) > 0 || len(q.batches) > 0 || q.light.Background || len(q.lines.items) > 0 || len(q.parts.scene) > 0
	// A frame with nothing 3D in it can still go through the composite,
	// so bloom, the grade, the LUT and the lens effects reach a 2D game.
	// A render texture keeps the direct path whatever Post2D says: the
	// composite writes an opaque image, and a render texture's alpha is
	// what a game draws it back with.
	flat := !has3D && s.Post2D && target == nil && len(q.stream.items) > 0
	bloom := (has3D || flat) && s.Bloom > 0
	ao := has3D && s.AmbientOcclusion > 0
	rays := false
	if has3D {
		if err := g.renderScene(fr, q, t); err != nil {
			return err
		}
		if err := g.postChain(cb, q, t); err != nil {
			return err
		}
		if s.GodRays > 0 {
			if sun, ok := sunScreen(q); ok {
				if err := t.needRays(g); err != nil {
					return err
				}
				g.timestamps.Begin(cb, "godrays")
				g.renderRays(cb, q, t, sun)
				g.timestamps.End(cb)
				rays = true
			}
		}
		if bloom {
			g.timestamps.Begin(cb, "bloom")
			g.renderBloom(cb, t, t.hdrSet)
			g.timestamps.End(cb)
		}
		if ao {
			g.timestamps.Begin(cb, "occlusion")
			g.renderAO(cb, q, t)
			g.timestamps.End(cb)
		}
	}
	clear := q.clear.premultiplied()
	vp := vk.VkRect2D{Extent: g.r.Swapchain.Extent}
	switch {
	case target != nil:
		vp.Extent = target.Extent
	case g.viewport.Extent.Width > 0:
		vp = g.viewport
	}
	if flat {
		// The 2D stream draws into the LDR image and the composite reads
		// it back, so no 2D pipeline needs an HDR-format variant.
		g.timestamps.Begin(cb, "2d")
		render.BeginTargetPass(cb, render.PassDesc{Target: t.ldr, ClearColor: clear, ClearDepth: 1})
		err := g.flush2D(fr, q, vk.VkRect2D{Extent: t.extent})
		render.EndTargetPass(cb, t.ldr)
		g.timestamps.End(cb)
		if err != nil {
			return err
		}
		if bloom {
			g.timestamps.Begin(cb, "bloom")
			g.renderBloom(cb, t, t.ldrSet)
			g.timestamps.End(cb)
		}
	}
	// Temporal anti-aliasing has already resolved the edges, so FXAA
	// would only soften them again.
	aa := (has3D || flat) && !s.NoAntiAlias && !(has3D && s.TemporalAA) && target == nil
	aaSet := t.ldrSet
	if aa {
		// Composite into an LDR image, then resolve with FXAA on screen.
		dst := t.ldr
		if flat {
			if err := t.needLDR2(g); err != nil {
				return err
			}
			dst, aaSet = t.ldr2, t.ldr2Set
		}
		// The LDR images are always single-sample in the swapchain's
		// format, so the composite into one takes the zero output key.
		g.timestamps.Begin(cb, "composite")
		render.BeginTargetPass(cb, render.PassDesc{Target: dst, ClearColor: clear, ClearDepth: 1})
		err := g.composite(cb, t, outKey{}, bloom, ao, rays, flat)
		render.EndTargetPass(cb, dst)
		g.timestamps.End(cb)
		if err != nil {
			return err
		}
	}
	switch {
	case target != nil:
		render.BeginTargetPass(cb, render.PassDesc{Target: target, ClearColor: clear, ClearDepth: 1})
	case g.viewport.Extent.Width > 0:
		// A fixed view inside the window: black outside the viewport, the
		// clear colour within it.
		g.r.BeginSwapchainPass(fr, [4]float32{0, 0, 0, 1})
		render.SetViewportRect(cb, vp)
		render.SetScissorRect(cb, vp)
		render.ClearRect(cb, vp, clear)
	default:
		g.r.BeginSwapchainPass(fr, clear)
	}
	switch {
	case aa:
		g.timestamps.Begin(cb, "antialias")
		g.antiAlias(cb, t, aaSet)
		g.timestamps.End(cb)
	case has3D || flat:
		g.timestamps.Begin(cb, "composite")
		err := g.composite(cb, t, q.out, bloom, ao, rays, flat)
		g.timestamps.End(cb)
		if err != nil {
			return err
		}
	}
	if !flat { // a 2D post frame has already drawn its stream
		g.timestamps.Begin(cb, "2d")
		err := g.flush2D(fr, q, vp)
		g.timestamps.End(cb)
		if err != nil {
			return err
		}
	}
	if target != nil {
		render.EndTargetPass(cb, target)
	}
	if has3D {
		// What the next frame reprojects against, without the jitter.
		aspect := float32(t.extent.Width) / float32(t.extent.Height)
		q.prevViewProj, q.hasPrevVP = q.camera.ViewProj(aspect), true
	}
	return nil
}

// recordScratch holds the arguments the recording commands take by
// pointer. The Vulkan calls force what a pointer argument refers to onto
// the heap, so a fresh local for each draw would allocate once per draw
// run; these fields live as long as the Graphics and are filled in place.
type recordScratch struct {
	push    push2D             // the 2D push-constant block, and the sky's
	solid   solidPush          // the outline and x-ray block
	decal   decalPush          // the decal block
	set     vk.VkDescriptorSet // the run's material set
	morph   vk.VkDescriptorSet // the run's morph target deltas
	dyn     uint32             // the run's dynamic uniform offset
	cascade int32              // the shadow pass's cascade index
	offset  vk.VkDeviceSize    // always zero: the vertex buffer bind offset
}

// flush2D records the queue's 2D stream: one draw per run of equal state.
func (g *Graphics) flush2D(fr *render.Frame, q *drawQueue, vp vk.VkRect2D) error {
	st := &q.stream
	if err := g.prepareParticles(q, fr.Slot); err != nil {
		return err
	}
	if len(st.items) == 0 {
		// Instanced particles can still be the only 2D drawing there is.
		if len(q.parts.flat) > 0 {
			render.SetViewportRect(fr.CB, vp)
			g.particles.push = push2D{frame: lin.V4(g.time, q.viewW, q.viewH, float32(vp.Extent.Width)/q.viewW)}
			_, err := g.drawFlatParticles(fr.CB, q, 0, draw2D{}, true, vp)
			return err
		}
		return nil
	}
	st.build()
	if err := st.upload(g, fr.Slot); err != nil {
		return err
	}
	g.stats.Draws2D += len(st.draws)
	g.stats.Vertices2D += len(st.ordered)
	cb := fr.CB
	render.SetViewportRect(cb, vp)
	rec := &g.rec
	rec.offset = 0
	vk.CmdBindVertexBuffers(cb, 0, 1, &st.buffers[fr.Slot].Handle, &rec.offset)
	var bound *render.Pipeline
	var boundProj *lin.Mat4
	var boundClip lin.Rect
	boundUniform := int32(-2)
	scaleX, scaleY := float32(vp.Extent.Width)/q.viewW, float32(vp.Extent.Height)/q.viewH
	rec.push = push2D{frame: lin.V4(g.time, q.viewW, q.viewH, scaleX)}
	g.particles.push = push2D{frame: rec.push.frame}
	part := 0 // the next instanced particle batch to record
	for _, d := range st.draws {
		if part < len(q.parts.flat) {
			// Particles submitted before this run, by layer and then by
			// order, go under it. They bind their own pipeline and
			// buffer, so what was bound for the stream has to be bound
			// again afterwards.
			next, err := g.drawFlatParticles(cb, q, part, d, false, vp)
			if err != nil {
				return err
			}
			if next != part {
				part = next
				bound, boundProj, boundUniform = nil, nil, -2
				boundClip = lin.Rect{} // the particles restored the full scissor
				rec.offset = 0
				vk.CmdBindVertexBuffers(cb, 0, 1, &st.buffers[fr.Slot].Handle, &rec.offset)
			}
		}
		s := d.state
		if s.clip != boundClip {
			boundClip = s.clip
			if s.clip == (lin.Rect{}) {
				render.SetScissorRect(cb, vp)
			} else {
				render.SetScissorRect(cb, pixelRect(vp, s.clip, scaleX, scaleY))
			}
		}
		pipe, err := s.shader.pipeline(pipeKey{blend: s.blend, out: q.out})
		if err != nil {
			return err
		}
		if pipe != bound {
			bound = pipe
			vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
			boundProj, boundUniform = nil, -2
		}
		if s.proj != boundProj {
			boundProj = s.proj
			rec.push.proj = *s.proj
			vk.CmdPushConstants(cb, pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT,
				0, push2DSize, unsafe.Pointer(&rec.push))
		}
		if s.uniform >= 0 && s.uniform != boundUniform {
			boundUniform = s.uniform
			rec.dyn = uint32(s.uniform)
			vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 1, 1, &g.uniforms.Sets[fr.Slot], 1, &rec.dyn)
		}
		rec.set = s.set
		vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &rec.set, 0, nil)
		vk.CmdDraw(cb, d.count, 1, d.first, 0)
	}
	// Then whatever was submitted after the last sprite run.
	if _, err := g.drawFlatParticles(cb, q, part, draw2D{}, true, vp); err != nil {
		return err
	}
	return nil
}

// destroy releases everything the context created. Textures made from it
// must be destroyed first or are leaked with the device.
func (g *Graphics) destroy() {
	_ = g.r.Device.WaitIdle()
	for slot := range g.retire {
		g.freeRetired(slot)
		g.retire[slot] = nil
	}
	if g.main != nil {
		g.main.destroy()
	}
	if g.dbgFont != nil {
		g.dbgFont.Destroy()
		g.dbgFont = nil
	}
	g.linePipe.destroy()
	g.particles.destroy()
	g.post.destroy(g)
	g.meshes.destroy(g)
	if g.white != nil {
		g.white.Destroy()
	}
	g.spriteShader.Destroy()
	g.sdfShader.Destroy()
	g.matrixShader.Destroy()
	g.litShader.Destroy()
	if g.staging != nil {
		g.staging.Destroy()
	}
	g.timestamps.Destroy()
	if g.uniforms != nil {
		g.uniforms.Destroy()
	}
	dev := g.r.Device.Handle
	vk.VkDestroySampler(dev, g.nearest, nil)
	vk.VkDestroySampler(dev, g.linear, nil)
	vk.VkDestroySampler(dev, g.nearestRep, nil)
	vk.VkDestroySampler(dev, g.linearRep, nil)
	g.descriptors.Destroy()
}

// sampler picks the shared sampler for a filtering and edge choice.
func (g *Graphics) sampler(linear, repeat bool) vk.VkSampler {
	switch {
	case linear && repeat:
		return g.linearRep
	case linear:
		return g.linear
	case repeat:
		return g.nearestRep
	}
	return g.nearest
}
