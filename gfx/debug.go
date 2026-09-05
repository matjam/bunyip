package gfx

import (
	"fmt"
	"math"
	"unsafe"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// DebugText draws a line of text in the engine's own font with a dark
// shadow, so a value can go on screen without loading a font first.
func (g *Graphics) DebugText(x, y float32, text string) {
	f := g.debugFont()
	if f == nil {
		return
	}
	g.DrawText(f, text, x+1, y+1, Color{0, 0, 0, 0.8})
	g.DrawText(f, text, x, y, White)
}

// Debugf is DebugText with a format string.
func (g *Graphics) Debugf(x, y float32, format string, args ...any) {
	g.DebugText(x, y, fmt.Sprintf(format, args...))
}

// DebugFont is the engine's built-in font, 14 view units tall, for
// overlays and tools; nil when it could not be made.
func (g *Graphics) DebugFont() *Font { return g.debugFont() }

func (g *Graphics) debugFont() *Font {
	if g.dbgFont == nil && !g.dbgFontFailed {
		f, err := g.NewFont(goregular.TTF, 14, FontOptions{})
		if err != nil {
			g.dbgFontFailed = true
			return nil
		}
		g.dbgFont = f
	}
	return g.dbgFont
}

// lineVertex is one end of a debug line: position and premultiplied
// colour, 16 bytes.
type lineVertex struct {
	pos   lin.Vec3
	color uint32
}

const lineVertexSize = 16

// lineStream is the per-frame buffer of debug line vertices.
type lineStream struct {
	items    []lineVertex
	buffers  [render.FramesInFlight]*render.Buffer
	capacity int
	slot     int
}

func (s *lineStream) reset() { s.items = s.items[:0] }

func (s *lineStream) upload(g *Graphics, slot int) error {
	s.slot = slot
	if len(s.items) > s.capacity {
		newCap := max(s.capacity*2, 4096)
		for newCap < len(s.items) {
			newCap *= 2
		}
		if err := g.growStream(&s.buffers, vk.VkDeviceSize(newCap*lineVertexSize)); err != nil {
			return err
		}
		s.capacity = newCap
	}
	if len(s.items) == 0 {
		return nil
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(&s.items[0])), len(s.items)*lineVertexSize)
	return s.buffers[slot].Write(0, data)
}

func (s *lineStream) destroy() {
	for i := range s.buffers {
		if s.buffers[i] != nil {
			s.buffers[i].Destroy()
			s.buffers[i] = nil
		}
	}
}

// DrawLine3D draws a one-pixel line between two world points, on top of
// everything: for seeing colliders, paths, rays and bones while a game is
// being written. Lines ignore depth so nothing hides them.
func (g *Graphics) DrawLine3D(a, b lin.Vec3, c Color) {
	if c == (Color{}) {
		c = White
	}
	p := c.premultiplied()
	col := packColor(Color{p[0], p[1], p[2], p[3]})
	q := g.cur
	q.lines.items = append(q.lines.items, lineVertex{a, col}, lineVertex{b, col})
}

// DrawWireBox outlines the axis-aligned box between two corners.
func (g *Graphics) DrawWireBox(min, max lin.Vec3, c Color) {
	var p [8]lin.Vec3
	for i := range p {
		p[i] = lin.V3(pick(i&1 != 0, min.X, max.X), pick(i&2 != 0, min.Y, max.Y), pick(i&4 != 0, min.Z, max.Z))
	}
	for _, e := range [12][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {0, 2}, {1, 3}, {4, 6}, {5, 7}, {0, 4}, {1, 5}, {2, 6}, {3, 7}} {
		g.DrawLine3D(p[e[0]], p[e[1]], c)
	}
}

// DrawWireCube outlines the unit cube (corners at ±0.5) under a matrix:
// an oriented box, the shape of a Box3 collider.
func (g *Graphics) DrawWireCube(m lin.Mat4, c Color) {
	var p [8]lin.Vec3
	for i := range p {
		p[i] = m.MulPoint(lin.V3(pick(i&1 != 0, -0.5, 0.5), pick(i&2 != 0, -0.5, 0.5), pick(i&4 != 0, -0.5, 0.5)))
	}
	for _, e := range [12][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {0, 2}, {1, 3}, {4, 6}, {5, 7}, {0, 4}, {1, 5}, {2, 6}, {3, 7}} {
		g.DrawLine3D(p[e[0]], p[e[1]], c)
	}
}

// DrawWireSphere outlines a sphere as three great circles.
func (g *Graphics) DrawWireSphere(center lin.Vec3, radius float32, c Color) {
	const n = 32
	var prev [3]lin.Vec3
	for i := 0; i <= n; i++ {
		a := float64(i) / n * 2 * math.Pi
		s, co := float32(math.Sin(a))*radius, float32(math.Cos(a))*radius
		pts := [3]lin.Vec3{center.Add(lin.V3(co, s, 0)), center.Add(lin.V3(co, 0, s)), center.Add(lin.V3(0, co, s))}
		if i > 0 {
			for k := range 3 {
				g.DrawLine3D(prev[k], pts[k], c)
			}
		}
		prev = pts
	}
}

// DrawAxes draws a transform's x, y and z axes in red, green and blue,
// each size units long.
func (g *Graphics) DrawAxes(m lin.Mat4, size float32) {
	o := m.MulPoint(lin.Vec3{})
	g.DrawLine3D(o, m.MulPoint(lin.V3(size, 0, 0)), Color{1, 0.2, 0.2, 1})
	g.DrawLine3D(o, m.MulPoint(lin.V3(0, size, 0)), Color{0.2, 1, 0.2, 1})
	g.DrawLine3D(o, m.MulPoint(lin.V3(0, 0, size)), Color{0.3, 0.4, 1, 1})
}

// DebugText3D draws debug text at a world position, projected to the
// view: an entity's id over its head, a value beside a probe. Points
// behind the camera draw nothing.
func (g *Graphics) DebugText3D(p lin.Vec3, text string) {
	if x, y, ok := g.Project(p); ok {
		g.DebugText(x, y, text)
	}
}

func pick(b bool, no, yes float32) float32 {
	if b {
		return yes
	}
	return no
}

// initLines builds the debug line pipeline: line list, no depth test,
// blended, positions and colours from one vertex stream.
func (g *Graphics) initLines() error {
	var err error
	g.linePipe, err = newPipeCache(g.r.Device, render.PipelineDesc{
		Vert: shaders.LineVert, Frag: shaders.LineFrag,
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		Topology: vk.VK_PRIMITIVE_TOPOLOGY_LINE_LIST,
		Bindings: []vk.VkVertexInputBindingDescription{{Binding: 0, Stride: lineVertexSize, InputRate: vk.VK_VERTEX_INPUT_RATE_VERTEX}},
		Attributes: []vk.VkVertexInputAttributeDescription{
			{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 0},
			{Location: 1, Binding: 0, Format: vk.VK_FORMAT_R8G8B8A8_UNORM, Offset: 12},
		},
		Blend: true, PushConstantSize: 64,
	})
	return err
}

// drawDebugLines records the queue's debug lines inside the HDR pass.
func (g *Graphics) drawDebugLines(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue) error {
	if len(q.lines.items) == 0 {
		return nil
	}
	if err := q.lines.upload(g, fr.Slot); err != nil {
		return err
	}
	pipe, err := g.linePipe.at(g.sceneOut)
	if err != nil {
		return err
	}
	rec := &g.rec
	rec.push.proj = q.viewProjJ
	rec.offset = 0
	vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
	vk.CmdPushConstants(cb, pipe.Layout, meshStages, 0, 64, unsafe.Pointer(&rec.push.proj))
	vk.CmdBindVertexBuffers(cb, 0, 1, &q.lines.buffers[q.lines.slot].Handle, &rec.offset)
	vk.CmdDraw(cb, uint32(len(q.lines.items)), 1, 0, 0)
	return nil
}
