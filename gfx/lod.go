package gfx

import "github.com/matjam/bunyip/lin"

// LODLevel is one mesh of a LOD and the camera distance up to which it
// is drawn.
type LODLevel struct {
	Mesh     *Mesh   // nil draws nothing at this level, for things that vanish far away
	Distance float32 // used while the camera is closer than this; zero means always
}

// LOD is a mesh at several levels of detail: a full model up close, a
// simpler one at a distance, a few triangles far away and nothing at all
// beyond. Levels are listed nearest first, each with the distance at
// which the next takes over; DrawLOD picks by the camera's distance to
// the model's origin.
type LOD struct {
	Levels []LODLevel
}

// NewLOD builds a LOD from meshes and the distances at which each hands
// over to the next; the last mesh serves beyond the last distance, so
// there is one fewer distance than meshes. Pass a nil mesh last to
// draw nothing far away.
func NewLOD(meshes []*Mesh, distances []float32) *LOD {
	l := &LOD{}
	for i, m := range meshes {
		lvl := LODLevel{Mesh: m}
		if i < len(distances) {
			lvl.Distance = distances[i]
		}
		l.Levels = append(l.Levels, lvl)
	}
	return l
}

// Pick returns the mesh for a camera distance, nil when the LOD draws
// nothing there.
func (l *LOD) Pick(distance float32) *Mesh {
	for _, lvl := range l.Levels {
		if lvl.Distance <= 0 || distance < lvl.Distance {
			return lvl.Mesh
		}
	}
	return nil
}

// DrawLOD draws the level of detail for the model's distance from the
// frame's camera, with a material and model matrix like DrawMesh.
func (g *Graphics) DrawLOD(l *LOD, mat Material, model lin.Mat4) {
	if l == nil {
		return
	}
	cam := g.cur.camera
	if !g.cur.hasCam {
		cam = Camera{Position: lin.V3(0, 0, 5)}
	}
	if m := l.Pick(model.Translation().Distance(cam.Position)); m != nil {
		g.DrawMesh(m, mat, model)
	}
}

// DrawLODAt is DrawLOD with a Transform.
func (g *Graphics) DrawLODAt(l *LOD, mat Material, t Transform) { g.DrawLOD(l, mat, t.Matrix()) }
