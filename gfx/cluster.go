package gfx

import (
	"math"
	"unsafe"

	"github.com/matjam/bunyip/lin"
)

const (
	// clusterX, clusterY and clusterZ are the cluster grid: tiles across
	// the view and slices into the distance, the slices spaced
	// exponentially so each one covers a constant fraction of the depth
	// range. The grid is fixed, so its buffers are allocated once with
	// the output and written without waiting for the device.
	clusterX = 16
	clusterY = 9
	clusterZ = 24
	// clusterCount is how many clusters a view holds.
	clusterCount = clusterX * clusterY * clusterZ
	// clusterLights is how many lights one cluster keeps. A fragment
	// loops over its own cluster's list, so this bounds the shading work
	// one pixel can be asked for; lights past it in the densest clusters
	// do not light them. A cluster is a long wedge, so a view down a
	// floor covered in lights fills one faster than the tile size
	// suggests.
	clusterLights = 64
	// clusterIndices is the light index list's capacity, which every
	// cluster can fill.
	clusterIndices = clusterCount * clusterLights
)

// lightRecord is one light in the frame's light buffer, matching
// LightData in prelude_mesh.wgsl's storage buffer.
type lightRecord struct {
	posRange lin.Vec4 // xyz position, w range
	color    lin.Vec4 // rgb colour, w = cos of a spot's inner cone, 2 for a point light
	dir      lin.Vec4 // xyz a spot's direction, w = cos of its outer cone, -2 for a point light
	info     lin.Vec4 // x = spot map index or -1, y = cube map slot or -1
}

const lightRecordSize = int(unsafe.Sizeof(lightRecord{}))

// clusterRange is the block of clusters one light reaches, inclusive.
type clusterRange struct {
	light                  int32
	x0, x1, y0, y1, z0, z1 int32
}

// clusterGrid holds a frame's lights and the grid that says which of
// them reach each part of the view: a table of two entries per cluster,
// where its light indices start in the index list and how many it has.
// The fragment prelude finds its cluster from gl_FragCoord and the view
// depth and loops over that cluster's lights alone, so a scene can light
// a frame with hundreds of them.
type clusterGrid struct {
	lights []lightRecord
	table  []uint32 // two per cluster: the first index and the count
	index  []uint32 // light indices, packed cluster by cluster
	used   int      // entries of index in use
	counts []uint32 // scratch: each cluster's count while the ranges are walked
	ranges []clusterRange
	// scale and bias map a view depth to a slice:
	// slice = log2(depth)*scale + bias.
	scale, bias float32
}

// clusterParams is what the shader needs to find a fragment's cluster:
// the tile size in pixels and the depth mapping.
func (c *clusterGrid) clusterParams(width, height float32) lin.Vec4 {
	return lin.V4(width/clusterX, height/clusterY, c.scale, c.bias)
}

// build fills the grid from a frame's lights for one camera and view
// aspect. spotSlots and pointSlots give each light its shadow map index
// or -1, so a shadowed light's map travels with its record. Every light
// is kept; a light is dropped only from the clusters that already hold
// clusterLights of them.
func (c *clusterGrid) build(points []pointLight, spotSlots, pointSlots []int32, cam Camera, aspect float32) {
	c.lights = c.lights[:0]
	c.ranges = c.ranges[:0]
	if cap(c.table) < 2*clusterCount {
		c.table = make([]uint32, 2*clusterCount)
		c.counts = make([]uint32, clusterCount)
		c.index = make([]uint32, clusterIndices)
	}
	c.table = c.table[:2*clusterCount]
	c.counts = c.counts[:clusterCount]
	clear(c.counts)
	c.used = 0

	_, _, near, far := c.setDepthMapping(cam)
	view := cam.viewMatrix()
	proj := cam.Projection(aspect)

	for i, p := range points {
		rec := lightRecord{
			posRange: p.pos.Vec4(p.rng),
			color:    lin.V4(p.color.R, p.color.G, p.color.B, 2),
			dir:      lin.V4(0, 0, 0, -2),
			info:     lin.V4(-1, -1, 0, 0),
		}
		if p.spot {
			rec.color.W = p.cosInner
			rec.dir = p.dir.Vec4(p.cosOuter)
		}
		if i < len(spotSlots) {
			rec.info.X = float32(spotSlots[i])
		}
		if i < len(pointSlots) {
			rec.info.Y = float32(pointSlots[i])
		}
		c.lights = append(c.lights, rec)
		c.ranges = c.lightClusters(c.ranges, int32(i), p, view, proj, near, far)
	}
	// One pass to count what each cluster holds, then the offsets, then a
	// pass to fill. Counting first keeps the index list packed, so a
	// cluster's lights are contiguous for the fragment to walk.
	for _, r := range c.ranges {
		c.eachCluster(r, func(ci int) {
			if c.counts[ci] < clusterLights {
				c.counts[ci]++
			}
		})
	}
	offset := uint32(0)
	for ci := range clusterCount {
		c.table[2*ci] = offset
		c.table[2*ci+1] = 0
		offset += c.counts[ci]
	}
	c.used = int(offset)
	for _, r := range c.ranges {
		c.eachCluster(r, func(ci int) {
			if n := c.table[2*ci+1]; n < c.counts[ci] {
				c.index[c.table[2*ci]+n] = uint32(r.light)
				c.table[2*ci+1] = n + 1
			}
		})
	}
}

// eachCluster runs f for every cluster in a light's block.
func (c *clusterGrid) eachCluster(r clusterRange, f func(ci int)) {
	for z := r.z0; z <= r.z1; z++ {
		for y := r.y0; y <= r.y1; y++ {
			row := int(y)*clusterX + int(z)*clusterX*clusterY
			for x := r.x0; x <= r.x1; x++ {
				f(row + int(x))
			}
		}
	}
}

// setDepthMapping works out the mapping from a view depth to a slice for
// a camera and returns the camera's defaults. The frame block carries the
// mapping, so it is set before the block is written, whether or not the
// frame has lights to sort.
func (c *clusterGrid) setDepthMapping(cam Camera) (up lin.Vec3, fov, near, far float32) {
	up, fov, near, far = cam.defaults()
	ratio := float32(math.Log2(float64(far / near)))
	c.scale = clusterZ / ratio
	c.bias = -clusterZ * float32(math.Log2(float64(near))) / ratio
	return up, fov, near, far
}

// sliceMargin widens each slice's depths by this fraction when a light
// is fitted to it, so a fragment the shader's rounding puts in the
// neighbouring slice still finds the light.
const sliceMargin = 1e-3

// sliceStart is the view depth where a slice begins.
func (c *clusterGrid) sliceStart(s int32) float32 {
	return float32(math.Exp2(float64((float32(s) - c.bias) / c.scale)))
}

// lightClusters appends the blocks of clusters a light's sphere can
// reach, one per depth slice. The slices come from the sphere's depth
// range in view space. In each slice the sphere is cut down to the part
// between the slice's near and far depths, whose widest cross-section is
// the sphere's own radius when the centre lies within them and shrinks
// towards the slice further from it, and the tiles are those the box
// around that part projects to. A slice far from the centre therefore
// lists the light in far fewer tiles than the one through it.
func (c *clusterGrid) lightClusters(dst []clusterRange, light int32, p pointLight, view, proj lin.Mat4, near, far float32) []clusterRange {
	r := max(p.rng, 1e-3)
	centre := view.MulPoint(p.pos)
	depth := -centre.Z
	lo, hi := depth-r, depth+r
	if hi < near || lo > far {
		return dst
	}
	z0 := clusterSlice(max(lo, near)*(1-sliceMargin), c.scale, c.bias)
	z1 := clusterSlice(min(hi, far)*(1+sliceMargin), c.scale, c.bias)
	for z := z0; z <= z1; z++ {
		// The slice's depths: the first reaches back to the near plane and
		// the last out to the far plane, as the shader clamps them.
		d0, d1 := near, far
		if z > 0 {
			d0 = c.sliceStart(z)
		}
		if z < clusterZ-1 {
			d1 = c.sliceStart(z + 1)
		}
		d0, d1 = max(d0*(1-sliceMargin), lo), min(d1*(1+sliceMargin), hi)
		if d0 > d1 {
			continue
		}
		// The widest circle of the sphere between d0 and d1.
		cut := r
		if gap := max(d0-depth, depth-d1, 0); gap > 0 {
			cut = float32(math.Sqrt(float64(max(r*r-gap*gap, 0))))
		}
		cut = cut*(1+1e-5) + 1e-5
		x0, x1, y0, y1, ok := projectSlab(proj, centre.X-cut, centre.X+cut, centre.Y-cut, centre.Y+cut, d0, d1)
		if !ok {
			continue
		}
		dst = append(dst, clusterRange{light: light, x0: x0, x1: x1, y0: y0, y1: y1, z0: z, z1: z})
	}
	return dst
}

// projectSlab returns the tiles a view-space box covers: x and y across,
// view depths d0 to d1. The box's corners bound its projection, since
// each clip coordinate over the depth is monotonic along every edge. It
// reports false for a box wholly outside the view. A box reaching behind
// the camera, which only a projection with the eye inside the slab can
// give, covers every tile.
func projectSlab(proj lin.Mat4, x0, x1, y0, y1, d0, d1 float32) (tx0, tx1, ty0, ty1 int32, ok bool) {
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for k := range 8 {
		x, y, z := pick(k&1 == 0, x0, x1), pick(k&2 == 0, y0, y1), -pick(k&4 == 0, d0, d1)
		cx := proj[0]*x + proj[4]*y + proj[8]*z + proj[12]
		cy := proj[1]*x + proj[5]*y + proj[9]*z + proj[13]
		cw := proj[3]*x + proj[7]*y + proj[11]*z + proj[15]
		if cw <= 1e-4 {
			return 0, clusterX - 1, 0, clusterY - 1, true
		}
		nx, ny := cx/cw, cy/cw
		minX, maxX = min(minX, nx), max(maxX, nx)
		minY, maxY = min(minY, ny), max(maxY, ny)
	}
	if minX > 1 || maxX < -1 || minY > 1 || maxY < -1 {
		return 0, 0, 0, 0, false
	}
	// Clip space runs -1..1 left to right and top to bottom, the way the
	// viewport does, so the tiles count the same way as the fragment
	// position.
	return clusterTile(minX, clusterX), clusterTile(maxX, clusterX), clusterTile(minY, clusterY), clusterTile(maxY, clusterY), true
}

// clusterTile is which tile of n a clip coordinate falls in, clamped to
// the view.
func clusterTile(v float32, n int32) int32 {
	t := int32(math.Floor(float64((v*0.5 + 0.5) * float32(n))))
	return min(max(t, 0), n-1)
}

// clusterSlice is which depth slice a view-space distance falls in.
func clusterSlice(depth, scale, bias float32) int32 {
	s := int32(math.Floor(float64(float32(math.Log2(float64(max(depth, 1e-4))))*scale + bias)))
	return min(max(s, 0), clusterZ-1)
}

// lightBytes views the light records for the upload.
func (c *clusterGrid) lightBytes() []byte {
	if len(c.lights) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&c.lights[0])), len(c.lights)*lightRecordSize)
}

// tableBytes views the cluster table, which is written whole every frame
// so a cluster that lost its lights reads a count of zero.
func (c *clusterGrid) tableBytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&c.table[0])), len(c.table)*4)
}

// indexBytes views the part of the light index list in use.
func (c *clusterGrid) indexBytes() []byte {
	if c.used == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&c.index[0])), c.used*4)
}
