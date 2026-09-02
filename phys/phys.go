// Package phys simulates rigid bodies in 2D and 3D on the entity
// component system: shapes attached to entities collide, bounce, slide
// and stack under gravity, and the game reads what touched what.
//
// A 2D entity carries a gfx.Transform2, a Body2 and a Collider2; a 3D
// entity a gfx.Transform, a Body3 and a Collider3. Register System2 or
// System3 (or both) on the world and set a Settings2 or Settings3
// resource for gravity and solver quality. Each update the system
// integrates velocities, finds overlapping shapes with a sweep over
// bounding boxes, generates contact points, resolves them with
// sequential impulses (restitution, friction, positional correction)
// and emits Collision and Trigger events.
//
//	ecs.SetResource(w, phys.Settings3{Gravity: lin.V3(0, -9.8, 0)})
//	w.SpawnWith(gfx.At(0, 5, 0), phys.Dynamic3(1), phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
//	w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(10, 0.5, 10)}}) // static floor
//	w.AddSystem("physics", phys.System3)
//
// For orbits and spaceflight, see the orbit package; it works with the
// same transforms at astronomical scale.
package phys

import "github.com/matjam/bunyip/ecs"

// Solver tuning shared by both dimensions.
const (
	slop      = 0.005 // penetration allowed before correction, in world units
	baumgarte = 0.2   // fraction of the remaining penetration corrected per step
	// Bounces slower than this are absorbed, so resting contacts settle.
	restitutionThreshold = 1.0
)

// Layers restrict which colliders meet: two colliders collide when each
// one's Layer bits appear in the other's Mask. Zero means "all".
type Layers struct {
	Layer uint32
	Mask  uint32
}

func (l Layers) collides(o Layers) bool {
	la, ma := l.Layer, l.Mask
	lb, mb := o.Layer, o.Mask
	if la == 0 {
		la = ^uint32(0)
	}
	if lb == 0 {
		lb = ^uint32(0)
	}
	if ma == 0 {
		ma = ^uint32(0)
	}
	if mb == 0 {
		mb = ^uint32(0)
	}
	return la&mb != 0 && lb&ma != 0
}

// pairKey orders two entities so a pair is reported once.
type pairKey struct{ a, b ecs.Entity }

func keyOf(a, b ecs.Entity) pairKey {
	if a.ID() < b.ID() {
		return pairKey{a, b}
	}
	return pairKey{b, a}
}

// sweepPairs runs a sort-and-sweep over intervals on one axis and calls
// fn for every overlapping pair (i < j). The caller checks the other
// axes.
func sweepPairs(n int, lo, hi func(i int) float32, fn func(i, j int)) {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sortInts(order, func(a, b int) bool { return lo(a) < lo(b) })
	for x := 0; x < n; x++ {
		i := order[x]
		end := hi(i)
		for y := x + 1; y < n; y++ {
			j := order[y]
			if lo(j) > end {
				break
			}
			fn(i, j)
		}
	}
}

// sortInts is an insertion sort, which is fast on the nearly sorted
// order the sweep sees frame after frame.
func sortInts(a []int, less func(a, b int) bool) {
	for i := 1; i < len(a); i++ {
		v := a[i]
		j := i - 1
		for j >= 0 && less(v, a[j]) {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = v
	}
}
