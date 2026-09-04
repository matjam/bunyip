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
// clips, and the epa buffers belong to one expansion.
type scratch3 struct {
	contacts []contact3
	ends     []contact3
	polyA    []lin.Vec3
	polyB    []lin.Vec3
	faceA    []lin.Vec3
	faceB    []lin.Vec3
	faceSort []lin.Vec3
	angles   []float64
	idx      []int
	epaVerts []gjkVert
	epaFaces []epaFace
	horizon  []epaEdge
}
