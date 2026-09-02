package phys

import "github.com/matjam/bunyip/lin"

// scratch2 holds the working buffers of 2D contact generation. One is
// kept per use (the step, and the queries) on the world's physics state
// and reset to empty before each use, so generating contacts allocates
// nothing once a world has run a step.
//
// Each buffer is used at one depth of the call chain: primsA and primsB
// are the two shapes split into primitives with their world points in
// ptsA and ptsB, normA and normB are those polygons' edge normals, quad
// is a segment thickened into a rectangle, clip0 and clip1 are the
// clipping ping-pong, and tmp is the candidate list a merge dedupes.
type scratch2 struct {
	contacts []contact2
	primsA   []prim2
	primsB   []prim2
	ptsA     []lin.Vec2
	ptsB     []lin.Vec2
	normA    []lin.Vec2
	normB    []lin.Vec2
	quad     []lin.Vec2
	clip0    []lin.Vec2
	clip1    []lin.Vec2
	tmp      []contact2
	keep     []contact2
}
