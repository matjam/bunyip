package phys

import "github.com/matjam/bunyip/lin"

// scratch3 holds the working buffers of 3D contact generation. One is
// kept per use (the step, and the queries) on the world's physics state
// and reset to empty before each use, so generating contacts allocates
// nothing once a world has run a step.
//
// The buffers are used at fixed depths of the call chain and never at
// two depths at once: contacts is the caller's result, ends is the
// per-end list pairContacts merges, polyA and polyB are the clipping
// ping-pong, faceA and faceB are the two support faces a manifold
// clips, hullA and hullB hold the world points of a hull too large to
// sit inside a convex, and the epa buffers belong to one expansion.
type scratch3 struct {
	contacts []contact3
	ends     []contact3
	hullA    []lin.Vec3
	hullB    []lin.Vec3
	// The two placed shapes of one pair. They live here rather than on
	// the stack because a placed convex handed on by pointer escapes,
	// which would put one on the heap for every pair tested.
	convA, convB convex
	polyA        []lin.Vec3
	polyB        []lin.Vec3
	faceA        []lin.Vec3
	faceB        []lin.Vec3
	faceSort     []lin.Vec3
	angles       []float64
	idx          []int
	epaVerts     []gjkVert
	epaFaces     []epaFace
	horizon      []epaEdge
}
