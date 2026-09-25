package gfx

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestShadowCastersSuperset checks each shadow map's caster list against
// the per-draw test it replaced, which tested every opaque draw's sphere
// against every map's frustum, near plane ignored for the cascades. A
// list may hold more, but it holds everything that test kept, except a
// caster that lies wholly outside the range sphere of a spot or point
// light, which cannot come between that light and anything it lights.
func TestShadowCastersSuperset(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	for scene := range 20 {
		var q drawQueue
		q.camera = Camera{Position: lin.V3(r.Float32()*20-10, 2+r.Float32()*8, 20), Target: lin.V3(0, 0, -10), Far: 150}
		q.hasCam = true
		q.light = Light{Direction: lin.V3(r.Float32()-0.5, -1, r.Float32()-0.5), Shadows: true, ShadowDistance: 20 + r.Float32()*60}
		for k := range 5 {
			pos := lin.V3(r.Float32()*60-30, 1+r.Float32()*10, r.Float32()*60-40)
			rng := r.Float32() * 25
			if k == 4 {
				rng = 0.2 // below the shadow projection's shortest reach
			}
			q.points = append(q.points,
				pointLight{pos: pos, rng: rng, spot: true, dir: lin.V3(r.Float32()-0.5, -1, r.Float32()-0.5).Norm(), outer: 0.3 + r.Float32()*2, shadow: true},
				pointLight{pos: pos.Add(lin.V3(3, 0, 0)), rng: rng, shadow: true})
		}
		q.findShadowLights()
		q.cascadeMats, _, _ = q.cascades(16.0 / 9)
		n := 3000
		for i := range n {
			d := meshDraw{
				centre:   lin.V3(r.Float32()*200-100, r.Float32()*30-5, r.Float32()*200-120),
				radius:   r.Float32() * 3,
				cullable: i%97 != 0,
			}
			if i%13 == 0 {
				d.radius = r.Float32() * 40 // a big one that reaches far
			}
			q.draws = append(q.draws, d)
			q.order = append(q.order, int32(i))
		}
		all := drawList{draws: q.draws, order: q.order}
		q.casters.setSpheres(q.draws, q.order) // the shadow order is the queue order here
		q.cullShadowMaps(true)

		checked := 0
		check := func(index int, f Frustum, cascade bool, light *pointLight) {
			got := q.casters.lists[index]
			if !slices.IsSorted(got) {
				t.Fatalf("scene %d map %d: the list is out of order", scene, index)
			}
			for i := range n {
				d := all.at(i)
				if d.cullable && !f.containsSphere(d.centre, d.radius, cascade) {
					continue // the old test left it out too
				}
				if _, found := slices.BinarySearch(got, int32(i)); found {
					checked++
					continue
				}
				if light != nil && d.cullable && d.centre.Distance(light.pos) > max(light.rng, 0.5)+d.radius {
					continue // outside the light's range sphere
				}
				t.Fatalf("scene %d map %d: draw %d (centre %v radius %v cullable %v) is missing", scene, index, i, d.centre, d.radius, d.cullable)
			}
		}
		for k := range shadowCascades {
			check(k, FrustumOf(q.cascadeMats[k]), true, nil)
		}
		for k, li := range q.shadow.spots {
			check(shadowCascades+k, FrustumOf(q.shadow.spotMats[k]), false, &q.points[li])
		}
		for k, li := range q.shadow.points {
			for face := range 6 {
				check(pointFaceBase+k*6+face, FrustumOf(q.shadow.pointMats[k*6+face]), false, &q.points[li])
			}
		}
		if checked == 0 {
			t.Fatalf("scene %d: no caster reached any map; the test checks nothing", scene)
		}
	}
}
