package gfx

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// clusterCorners is the eight corners of a cluster in view space, corner
// k having bit 0 for the right side, bit 1 for the lower side and bit 2
// for the far end. The cluster is what the fragment prelude maps to it:
// the tile's slice of clip space across and down, and the view depths
// its slice holds, the first slice reaching back to the near plane and
// the last out to the far plane.
func clusterCorners(invProj, proj lin.Mat4, grid *clusterGrid, near, far float32, x, y, z int) [8]lin.Vec3 {
	depth := func(s int) float32 {
		switch s {
		case 0:
			return near
		case clusterZ:
			return far
		}
		return float32(math.Exp2(float64((float32(s) - grid.bias) / grid.scale)))
	}
	var out [8]lin.Vec3
	for k := range 8 {
		nx := -1 + 2*float32(x+k&1)/clusterX
		ny := -1 + 2*float32(y+k>>1&1)/clusterY
		d := depth(z + k>>2&1)
		clip := proj.MulVec4(lin.V4(0, 0, -d, 1))
		p := invProj.MulVec4(lin.V4(nx, ny, clip.Z/clip.W, 1))
		out[k] = p.Vec3().Mul(1 / p.W)
	}
	return out
}

// segmentDistance3 is how far p is from the segment a..b.
func segmentDistance3(p, a, b lin.Vec3) float32 {
	ab := b.Sub(a)
	t := float32(0)
	if l := ab.Dot(ab); l > 0 {
		t = min(max(p.Sub(a).Dot(ab)/l, 0), 1)
	}
	return p.Distance(a.Add(ab.Mul(t)))
}

// hexDistance is how far p is from the convex solid with the given
// corners, laid out as clusterCorners lays them out: zero inside, and
// otherwise the distance to the nearest face, edge or corner.
func hexDistance(p lin.Vec3, c [8]lin.Vec3) float32 {
	// Each face as four corners in order around it.
	faces := [6][4]int{
		{0, 1, 3, 2}, {4, 5, 7, 6}, // near, far
		{0, 1, 5, 4}, {2, 3, 7, 6}, // top, bottom
		{0, 2, 6, 4}, {1, 3, 7, 5}, // left, right
	}
	centre := lin.Vec3{}
	for _, v := range c {
		centre = centre.Add(v.Mul(1.0 / 8))
	}
	inside := true
	best := float32(math.Inf(1))
	for _, f := range faces {
		a, b, cc, d := c[f[0]], c[f[1]], c[f[2]], c[f[3]]
		n := cc.Sub(a).Cross(d.Sub(b)).Norm()
		if n.Dot(centre.Sub(a)) > 0 {
			n = n.Mul(-1) // outward
		}
		dist := n.Dot(p.Sub(a))
		if dist > 0 {
			inside = false
		}
		// The point's projection onto the face, if it lands inside it.
		q := p.Sub(n.Mul(dist))
		in := true
		quad := [4]lin.Vec3{a, b, cc, d}
		for e := range 4 {
			u, v := quad[e], quad[(e+1)%4]
			if v.Sub(u).Cross(q.Sub(u)).Dot(n) < 0 {
				in = false
			}
		}
		if !in {
			in = true
			for e := range 4 {
				u, v := quad[e], quad[(e+1)%4]
				if v.Sub(u).Cross(q.Sub(u)).Dot(n) > 0 {
					in = false
				}
			}
		}
		if in {
			best = min(best, abs32(dist))
		}
		for e := range 4 {
			best = min(best, segmentDistance3(p, quad[e], quad[(e+1)%4]))
		}
	}
	if inside {
		return 0
	}
	return best
}

// TestClusterConservative sorts random lights into the grid for several
// cameras and checks, for every light and every cluster, that a light
// whose sphere reaches into the cluster is on the cluster's list. The
// test is exact: it measures the distance from the light to the
// cluster's own solid, not to a box around it.
func TestClusterConservative(t *testing.T) {
	r := rand.New(rand.NewPCG(4, 5))
	cams := []Camera{
		{Position: lin.V3(0, 6, 30), Target: lin.V3(0, 0, -20)},
		{Position: lin.V3(3, 1, 2), Target: lin.V3(-4, 0.5, -3), FovY: lin.Radians(90), Near: 0.5, Far: 80},
		{Position: lin.V3(0, 40, 0.01), Target: lin.V3(0, 0, 0), Near: 1, Far: 300},
		{Position: lin.V3(0, 5, 20), Target: lin.V3(0, 0, 0), Ortho: 12, Near: 0.1, Far: 100},
	}
	for ci, cam := range cams {
		aspect := float32(16.0 / 9)
		if ci == 2 {
			aspect = 1
		}
		view := cam.viewMatrix()
		proj := cam.Projection(aspect)
		invProj := proj.Inverse()
		_, _, near, far := cam.defaults()
		// Few enough lights that no cluster fills and drops one.
		var lights []pointLight
		for i := range 48 {
			// Lights around the view, in front, behind, crossing the near
			// plane, small and large.
			fwd := cam.Target.Sub(cam.Position).Norm()
			along := (r.Float32()*1.2 - 0.1) * far * 0.5
			side := lin.V3(r.Float32()-0.5, r.Float32()-0.5, r.Float32()-0.5).Mul(along + 5)
			pos := cam.Position.Add(fwd.Mul(along)).Add(side)
			rng := 0.2 + r.Float32()*r.Float32()*25
			if i%12 == 0 {
				pos = cam.Position.Add(fwd.Mul(near * 0.5)) // across the near plane
			}
			lights = append(lights, pointLight{pos: pos, rng: rng, spot: i%3 == 0, dir: fwd})
		}
		var grid clusterGrid
		grid.build(lights, nil, nil, cam, aspect)
		listed := make([]map[uint32]bool, clusterCount)
		for c := range clusterCount {
			listed[c] = map[uint32]bool{}
			start, n := grid.table[2*c], grid.table[2*c+1]
			if n >= clusterLights {
				t.Fatalf("camera %d: cluster %d is full; the test needs fewer lights", ci, c)
			}
			for _, l := range grid.index[start : start+n] {
				listed[c][l] = true
			}
		}
		checked := 0
		for z := range clusterZ {
			for y := range clusterY {
				for x := range clusterX {
					corners := clusterCorners(invProj, proj, &grid, near, far, x, y, z)
					c := x + y*clusterX + z*clusterX*clusterY
					for li, l := range lights {
						p := view.MulPoint(l.pos)
						rad := max(l.rng, 1e-3)
						// A margin keeps rounding at a grazing contact out of it.
						if hexDistance(p, corners) >= rad*(1-1e-3)-1e-4 {
							continue
						}
						checked++
						if !listed[c][uint32(li)] {
							t.Fatalf("camera %d: light %d (view %v, range %v) reaches cluster (%d, %d, %d) but is not listed", ci, li, p, rad, x, y, z)
						}
					}
				}
			}
		}
		if checked == 0 {
			t.Fatalf("camera %d: no light reached any cluster", ci)
		}
	}
}
