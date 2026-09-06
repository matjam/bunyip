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

	_, _, near, far := cam.defaults()
	ratio := float32(math.Log2(float64(far / near)))
	c.scale = clusterZ / ratio
	c.bias = -clusterZ * float32(math.Log2(float64(near))) / ratio
	view := cam.viewMatrix()
	viewProj := cam.Projection(aspect).Mul(view)

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
		if r, ok := lightClusters(p, view, viewProj, near, far, c.scale, c.bias); ok {
			r.light = int32(i)
			c.ranges = append(c.ranges, r)
		}
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

// lightClusters is the block of clusters a light's sphere can reach. The
// depth range comes from the sphere in view space and the screen
// rectangle from the corners of its world box, which covers the sphere
// and projects with eight points. A sphere that crosses the camera plane
// spans the whole view, since its projection is no longer bounded by
// those corners.
func lightClusters(p pointLight, view, viewProj lin.Mat4, near, far, scale, bias float32) (clusterRange, bool) {
	r := max(p.rng, 1e-3)
	depth := -view.MulPoint(p.pos).Z
	lo, hi := depth-r, depth+r
	if hi < near || lo > far {
		return clusterRange{}, false
	}
	out := clusterRange{
		z0: clusterSlice(max(lo, near), scale, bias),
		z1: clusterSlice(min(hi, far), scale, bias),
		x1: clusterX - 1, y1: clusterY - 1,
	}
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for k := range 8 {
		corner := lin.V3(p.pos.X, p.pos.Y, p.pos.Z)
		corner = corner.Add(lin.V3(pick(k&1 == 0, -r, r), pick(k&2 == 0, -r, r), pick(k&4 == 0, -r, r)))
		clip := viewProj.MulVec4(corner.Vec4(1))
		if clip.W <= 1e-4 {
			return out, true // it wraps around the camera: every tile
		}
		x, y := clip.X/clip.W, clip.Y/clip.W
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, y), max(maxY, y)
	}
	if minX > 1 || maxX < -1 || minY > 1 || maxY < -1 {
		return clusterRange{}, false
	}
	// Clip space runs -1..1 left to right and top to bottom, the way the
	// viewport does, so the tiles count the same way as gl_FragCoord.
	out.x0 = clusterTile(minX, clusterX)
	out.x1 = clusterTile(maxX, clusterX)
	out.y0 = clusterTile(minY, clusterY)
	out.y1 = clusterTile(maxY, clusterY)
	return out, true
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
