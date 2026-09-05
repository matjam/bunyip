package gfx

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Geometry2D is reusable triangle geometry in GPU memory. DrawGeometry
// places it in the ordinary 2D queue with the current transform, camera,
// layer, sort key, clip, blend and shader. Graphics owns its lifetime;
// Destroy releases it early. Its zero value cannot be drawn or updated.
type Geometry2D struct {
	g      *Graphics
	data   *geometry2DData
	bounds lin.Rect
}

// Each version remains immutable while queued draws hold it.
type geometry2DData struct {
	vertices, indices *render.Buffer
	count             uint32
	vertexCount       int
}

// NewGeometry2D uploads vertices and triangle indices once. Nil or empty
// indices use consecutive groups of three vertices. Incomplete triangles,
// out-of-range indices and non-finite positions return errors. Empty geometry
// is valid and draws nothing. The input slices may be reused after return.
func (g *Graphics) NewGeometry2D(vertices []Vertex2D, indices []uint32) (*Geometry2D, error) {
	packed := make([]vertex2D, len(vertices))
	for i, v := range vertices {
		c := v.Color
		if c == (Color{}) {
			c = White
		}
		packed[i] = vertex2D{pos: v.Pos, uv: v.UV, color: c.premultiplied()}
	}
	return g.newGeometry2D(packed, indices)
}

func (g *Graphics) newGeometry2D(vertices []vertex2D, indices []uint32) (*Geometry2D, error) {
	data, bounds, err := g.geometry2DData(vertices, indices)
	if err != nil {
		return nil, err
	}
	m := &Geometry2D{g: g, data: data, bounds: bounds}
	m.track()
	return m, nil
}

func (g *Graphics) geometry2DData(vertices []vertex2D, indices []uint32) (*geometry2DData, lin.Rect, error) {
	count := len(indices)
	if count == 0 {
		count = len(vertices)
	}
	if count%3 != 0 {
		return nil, lin.Rect{}, fmt.Errorf("gfx: geometry needs complete triangles, got %d elements", count)
	}
	if uint64(count) > math.MaxUint32 {
		return nil, lin.Rect{}, fmt.Errorf("gfx: geometry has too many elements")
	}
	for _, i := range indices {
		if uint64(i) >= uint64(len(vertices)) {
			return nil, lin.Rect{}, fmt.Errorf("gfx: geometry index %d out of range for %d vertices", i, len(vertices))
		}
	}
	var bounds lin.Rect
	if len(vertices) > 0 {
		lo, hi := vertices[0].pos, vertices[0].pos
		for _, v := range vertices {
			if math.IsNaN(float64(v.pos.X)) || math.IsNaN(float64(v.pos.Y)) || math.IsInf(float64(v.pos.X), 0) || math.IsInf(float64(v.pos.Y), 0) {
				return nil, lin.Rect{}, fmt.Errorf("gfx: geometry positions must be finite")
			}
			lo = lin.V2(min(lo.X, v.pos.X), min(lo.Y, v.pos.Y))
			hi = lin.V2(max(hi.X, v.pos.X), max(hi.Y, v.pos.Y))
		}
		bounds = lin.R(lo.X, lo.Y, hi.X-lo.X, hi.Y-lo.Y)
	}
	d := &geometry2DData{count: uint32(count), vertexCount: len(vertices)}
	if count == 0 {
		return d, bounds, nil
	}
	var err error
	d.vertices, err = g.uploadGeometry(unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*vertex2DSize), vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT)
	if err != nil {
		return nil, lin.Rect{}, err
	}
	if len(indices) > 0 {
		d.indices, err = g.uploadGeometry(unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4), vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT)
		if err != nil {
			// The vertex upload may already be recorded in the active frame.
			g.deferDestroy(d.vertices.Destroy)
			return nil, lin.Rect{}, err
		}
	}
	return d, bounds, nil
}

// Update replaces the geometry. Draws already queued keep the previous
// version until their GPU work finishes. Failure leaves the old data intact.
func (m *Geometry2D) Update(vertices []Vertex2D, indices []uint32) error {
	if m.g == nil || m.data == nil {
		return fmt.Errorf("gfx: update of destroyed geometry")
	}
	packed := make([]vertex2D, len(vertices))
	for i, v := range vertices {
		c := v.Color
		if c == (Color{}) {
			c = White
		}
		packed[i] = vertex2D{pos: v.Pos, uv: v.UV, color: c.premultiplied()}
	}
	d, bounds, err := m.g.geometry2DData(packed, indices)
	if err != nil {
		return err
	}
	m.retire()
	m.data, m.bounds = d, bounds
	m.track()
	return nil
}

// Bounds returns the local axis-aligned bounds of all uploaded vertices,
// including unused vertices. It excludes the graphics transform and camera.
func (m *Geometry2D) Bounds() lin.Rect { return m.bounds }

func (m *Geometry2D) track() {
	d := m.data
	indices := 0
	if d.indices != nil {
		indices = int(d.count)
	}
	m.g.track(m, Resource{Kind: ResourceGeometry2D, Vertices: d.vertexCount, Indices: indices, Bytes: d.vertexCount*vertex2DSize + indices*4})
}

func (m *Geometry2D) retire() {
	d := m.data
	m.g.deferDestroy(func() {
		if d.indices != nil {
			d.indices.Destroy()
		}
		if d.vertices != nil {
			d.vertices.Destroy()
		}
	})
}

// Destroy releases this geometry after queued and in-flight draws finish.
// Repeated calls do nothing. Later DrawGeometry calls with it draw nothing.
func (m *Geometry2D) Destroy() {
	if m.data == nil {
		return
	}
	m.g.forget(m)
	m.g.owned.remove(m)
	m.retire()
	m.data = nil
}

// DrawGeometry queues a GPU-resident geometry version without copying its
// vertices. A nil texture means white. Nil, empty or destroyed geometry draws
// nothing. Geometry and textures must belong to this Graphics context.
func (g *Graphics) DrawGeometry(tex *Texture, geometry *Geometry2D) {
	if geometry == nil || geometry.data == nil || geometry.data.count == 0 {
		return
	}
	if geometry.g != g {
		panic("gfx: geometry belongs to another graphics context")
	}
	q := g.cur
	st := g.state2D(tex, FilterDefault)
	st.transform = q.xform
	q.stream.addGeometry(st, q.layer, q.sortKey, geometry.data)
}
