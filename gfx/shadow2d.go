package gfx

import (
	"image"
	"math"

	"github.com/matjam/bunyip/lin"
)

// The polar shadow maps behind DrawLit. Each shadowed 2D light gets one
// row of a small Data texture: for each of shadowAngles2D directions
// around the light, the distance to the nearest occluder edge, as a
// fraction of the light's radius in sixteen bits. The lit shader reads
// the row along the direction from the light to the fragment and darkens
// the fragment beyond that distance.
const (
	// shadowAngles2D is how many directions each light's map holds. It is
	// the width of the strip texture; a light's radius spread over 512
	// directions leaves the shadow edge under a degree wide.
	shadowAngles2D = 512
	// shadowBias2D is added to every recorded distance, in view units, so
	// an occluder placed along a wall does not shadow the wall itself.
	shadowBias2D = 0.5
)

// AddOccluder2D adds a shadow caster for this frame: a closed polygon in
// the same units as sprite positions, which is world units under a 2D
// camera. Lights given Shadows in SetLights2D are blocked by it, and
// every DrawLit sprite in the frame sees the same set. Two points make a
// single wall segment; fewer than two are ignored. Occluders are cleared
// at the start of each frame, like the lights, so a game adds them every
// frame. The cost is the occluder's edges times the shadowed lights,
// computed on the CPU, so a few hundred edges are free and tens of
// thousands are not.
func (g *Graphics) AddOccluder2D(points ...lin.Vec2) {
	if len(points) < 2 {
		return
	}
	q := g.cur
	q.occluders = append(q.occluders, points...)
	q.occluderRuns = append(q.occluderRuns, int32(len(points)))
}

// buildShadows2D fills and uploads the queue's shadow strip. It runs
// once a frame, before any pass is recorded, so every lit draw in the
// frame samples the same maps whatever order the occluders arrived in.
func (g *Graphics) buildShadows2D(q *drawQueue) error {
	if !q.shadows || q.shadowTex == nil {
		return nil
	}
	if q.shadowPix == nil {
		q.shadowPix = make([]byte, shadowAngles2D*maxLights2D*4)
		q.shadowDist = make([]float32, shadowAngles2D)
	}
	count := int(q.lights.Ambient.W)
	for i := range min(count, maxLights2D) {
		if q.lights.Shadow[i].X == 0 {
			continue // an unshadowed light never reads its row
		}
		light := lin.V2(q.lights.Pos[i].X, q.lights.Pos[i].Y)
		radius := max(q.lights.Pos[i].W, 1e-3)
		shadowRow(q.shadowPix[i*shadowAngles2D*4:(i+1)*shadowAngles2D*4], q.shadowDist,
			light, radius, q.occluders, q.occluderRuns)
	}
	img := &image.RGBA{Pix: q.shadowPix, Stride: shadowAngles2D * 4,
		Rect: image.Rect(0, 0, shadowAngles2D, maxLights2D)}
	return q.shadowTex.Write(0, 0, img)
}

// shadowRow fills one light's row: the distance to the nearest occluder
// along each direction, clamped to the light's radius and encoded as a
// fraction of it across the red and green bytes.
func shadowRow(row []byte, dist []float32, light lin.Vec2, radius float32, points []lin.Vec2, runs []int32) {
	shadowDistances(dist, light, radius, points, runs)
	for i, d := range dist {
		v := uint16(lin.Clamp((d+shadowBias2D)/radius, 0, 1) * 0xFFFF)
		row[4*i], row[4*i+1], row[4*i+2], row[4*i+3] = byte(v>>8), byte(v), 0, 0xFF
	}
}

// shadowDistances fills one light's distances: the light's radius where
// nothing blocks it, and the distance to the nearest occluder edge along
// the directions that are blocked.
func shadowDistances(dist []float32, light lin.Vec2, radius float32, points []lin.Vec2, runs []int32) {
	for i := range dist {
		dist[i] = radius
	}
	at := 0
	for _, n := range runs {
		if at+int(n) > len(points) {
			break
		}
		poly := points[at : at+int(n)]
		at += int(n)
		edges := len(poly)
		if edges == 2 {
			edges = 1 // a two-point occluder is one wall, not a there and back
		}
		for i := range edges {
			shadowSegment(dist, light, radius, poly[i], poly[(i+1)%len(poly)])
		}
	}
}

// shadowSegment lowers the distances of the directions one occluder edge
// covers. Only those directions are visited, so the cost follows the
// edge's angular width rather than the width of the whole map.
func shadowSegment(dist []float32, light lin.Vec2, radius float32, a, b lin.Vec2) {
	if segmentDistance(light, a, b) > radius {
		return // the whole edge is beyond the light's reach
	}
	ra, rb := a.Sub(light), b.Sub(light)
	if ra == (lin.Vec2{}) || rb == (lin.Vec2{}) {
		return // an edge through the light has no angular span
	}
	from := float32(math.Atan2(float64(ra.Y), float64(ra.X)))
	span := float32(math.Atan2(float64(rb.Y), float64(rb.X))) - from
	// The edge covers the shorter way round, which is under half a turn
	// for any edge that does not pass through the light.
	for span > math.Pi {
		span -= 2 * math.Pi
	}
	for span < -math.Pi {
		span += 2 * math.Pi
	}
	if span < 0 {
		from, span = from+span, -span
	}
	const n = shadowAngles2D
	// The fractional index whose texel centre sits at an angle, matching
	// the lookup the shader does.
	first := (from/(2*math.Pi)+0.5)*n - 0.5
	last := first + span/(2*math.Pi)*n
	for k := int(math.Ceil(float64(first))); k <= int(math.Floor(float64(last))); k++ {
		i := ((k % n) + n) % n
		angle := 2 * math.Pi * ((float64(i)+0.5)/n - 0.5)
		d := lin.V2(float32(math.Cos(angle)), float32(math.Sin(angle)))
		if t, ok := raySegment(light, d, a, b); ok && t < dist[i] {
			dist[i] = t
		}
	}
}

// raySegment returns how far along a unit direction from an origin a
// segment lies, and whether it is hit at all.
func raySegment(origin, dir, a, b lin.Vec2) (float32, bool) {
	e := b.Sub(a)
	den := dir.X*e.Y - dir.Y*e.X
	if den == 0 {
		return 0, false // parallel
	}
	oa := a.Sub(origin)
	t := (oa.X*e.Y - oa.Y*e.X) / den
	u := (oa.X*dir.Y - oa.Y*dir.X) / den
	if t < 0 || u < 0 || u > 1 {
		return 0, false
	}
	return t, true
}

// segmentDistance is the distance from a point to a segment, which says
// whether an edge can reach inside a light's radius.
func segmentDistance(p, a, b lin.Vec2) float32 {
	e := b.Sub(a)
	len2 := e.X*e.X + e.Y*e.Y
	if len2 == 0 {
		return p.Sub(a).Len()
	}
	t := lin.Clamp(((p.X-a.X)*e.X+(p.Y-a.Y)*e.Y)/len2, 0, 1)
	return p.Sub(lin.V2(a.X+t*e.X, a.Y+t*e.Y)).Len()
}

// shadowTexture makes the queue's strip: one row per light, one texel per
// direction, uploaded as data so the bytes arrive as they were written.
func (g *Graphics) newShadowTexture() (*Texture, error) {
	return g.NewBlankTexture(shadowAngles2D, maxLights2D, TextureOptions{Data: true})
}
