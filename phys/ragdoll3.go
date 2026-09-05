package phys

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// RagdollBone sizes one part of a ragdoll: the distance from one end
// of its capsule to the other and the capsule's radius.
type RagdollBone struct{ Length, Radius float32 }

// RagdollSpec describes a humanoid ragdoll for NewRagdoll3. Zero
// fields take the defaults of a figure 1.8 units tall.
type RagdollSpec struct {
	// Position is the point on the ground between the feet and Rotation
	// turns the whole figure, which stands facing -Z with +X on its
	// right.
	Position lin.Vec3
	Rotation lin.Quat
	// Height scales the default bone sizes; zero means 1.8.
	Height float32
	// Mass is the total mass, shared between the parts in human
	// proportions; zero means 70.
	Mass float32
	// Friction and Damping apply to every part; zero friction means 0.6
	// and zero damping means 0.5 per second.
	Friction float32
	Damping  float32
	// Layers apply to every part. Zero puts the ragdoll on a layer of
	// its own that meets everything except other ragdoll parts, so
	// neighbouring limbs do not fight their joints.
	Layers Layers
	// Bone sizes; a zero field takes the default scaled by Height.
	Pelvis, Spine, Head, UpperArm, Forearm, Thigh, Shin RagdollBone
}

// Ragdoll part names, in the order NewRagdoll3 spawns them.
const (
	RagdollPelvis     = "pelvis"
	RagdollSpine      = "spine"
	RagdollHead       = "head"
	RagdollUpperArmL  = "upper_arm_l"
	RagdollForearmL   = "forearm_l"
	RagdollUpperArmR  = "upper_arm_r"
	RagdollForearmR   = "forearm_r"
	RagdollThighL     = "thigh_l"
	RagdollShinL      = "shin_l"
	RagdollThighR     = "thigh_r"
	RagdollShinR      = "shin_r"
	ragdollLayer      = uint32(1) << 31
	ragdollHeight     = 1.8
	ragdollMass       = 70
	ragdollFriction   = 0.6
	ragdollDamping    = 0.5
	ragdollHipHalf    = 0.1  // half the distance between the hips
	ragdollShoulderY  = 0.08 // how far below the neck the shoulders sit
	ragdollPelvisLift = 0.1  // the pelvis centre above the hips
	ragdollWaistLift  = 0.12 // the waist joint above the pelvis centre
)

// RagdollParts lists the part names in the order NewRagdoll3 spawns
// them, the pelvis first.
var RagdollParts = []string{
	RagdollPelvis, RagdollSpine, RagdollHead,
	RagdollUpperArmL, RagdollForearmL, RagdollUpperArmR, RagdollForearmR,
	RagdollThighL, RagdollShinL, RagdollThighR, RagdollShinR,
}

// Ragdoll3 is a spawned humanoid: capsule bodies for the parts, joined
// by ball joints at the waist, neck, shoulders and hips and limited
// hinges at the elbows and knees. Every part's capsule runs along its
// local Y with the parent joint at the top, and every part starts with
// the spec's rotation, so a standing figure has all its parts upright.
type Ragdoll3 struct {
	// Parts maps a part name to its entity.
	Parts map[string]ecs.Entity
	// Joints maps a part name to the joint entity tying it to its
	// parent; the pelvis has none. Waist, neck, shoulder and hip joints
	// are BallJoint3, elbows and knees HingeJoint3.
	Joints map[string]ecs.Entity
	// Bones records each part's size as built, so a game can place the
	// centre of a part from the position of its joint.
	Bones map[string]RagdollBone
}

// ragdollPart is one part's layout in the figure's frame before it is
// placed: the centre and the share of the mass.
type ragdollPart struct {
	name   string
	bone   RagdollBone
	centre lin.Vec3
	share  float32
}

// NewRagdoll3 spawns the parts and joints of a humanoid ragdoll and
// returns them by name. Each part carries a gfx.Transform, a Body3 and
// a Collider3 with a Capsule.
func NewRagdoll3(w *ecs.World, spec RagdollSpec) *Ragdoll3 {
	scale := spec.Height / ragdollHeight
	if spec.Height == 0 {
		scale = 1
	}
	def := func(given RagdollBone, length, radius float32) RagdollBone {
		if given.Length == 0 {
			given.Length = length * scale
		}
		if given.Radius == 0 {
			given.Radius = radius * scale
		}
		return given
	}
	pelvis := def(spec.Pelvis, 0.3, 0.13)
	spine := def(spec.Spine, 0.44, 0.16)
	head := def(spec.Head, 0.24, 0.12)
	upperArm := def(spec.UpperArm, 0.3, 0.05)
	forearm := def(spec.Forearm, 0.28, 0.045)
	thigh := def(spec.Thigh, 0.45, 0.08)
	shin := def(spec.Shin, 0.45, 0.06)
	mass := spec.Mass
	if mass == 0 {
		mass = ragdollMass
	}
	friction, damping := spec.Friction, spec.Damping
	if friction == 0 {
		friction = ragdollFriction
	}
	if damping == 0 {
		damping = ragdollDamping
	}
	layers := spec.Layers
	if layers == (Layers{}) {
		layers = Layers{Layer: ragdollLayer, Mask: ^ragdollLayer}
	}
	rot := spec.Rotation
	if rot == (lin.Quat{}) {
		rot = lin.QuatIdentity()
	}

	// The figure's frame: standing on y 0, facing -Z, right side at +X.
	knee := shin.Length
	hip := knee + thigh.Length
	pelvisY := hip + ragdollPelvisLift*scale
	waist := pelvisY + ragdollWaistLift*scale
	neck := waist + spine.Length
	shoulderY := neck - ragdollShoulderY*scale
	shoulderX := spine.Radius + upperArm.Radius + 0.01*scale
	elbow := shoulderY - upperArm.Length
	hipX := ragdollHipHalf * scale
	parts := []ragdollPart{
		{RagdollPelvis, pelvis, lin.V3(0, pelvisY, 0), 0.15},
		{RagdollSpine, spine, lin.V3(0, waist+spine.Length/2, 0), 0.35},
		{RagdollHead, head, lin.V3(0, neck+head.Length/2, 0), 0.08},
		{RagdollUpperArmL, upperArm, lin.V3(-shoulderX, shoulderY-upperArm.Length/2, 0), 0.03},
		{RagdollForearmL, forearm, lin.V3(-shoulderX, elbow-forearm.Length/2, 0), 0.02},
		{RagdollUpperArmR, upperArm, lin.V3(shoulderX, shoulderY-upperArm.Length/2, 0), 0.03},
		{RagdollForearmR, forearm, lin.V3(shoulderX, elbow-forearm.Length/2, 0), 0.02},
		{RagdollThighL, thigh, lin.V3(-hipX, knee+thigh.Length/2, 0), 0.1},
		{RagdollShinL, shin, lin.V3(-hipX, shin.Length/2, 0), 0.06},
		{RagdollThighR, thigh, lin.V3(hipX, knee+thigh.Length/2, 0), 0.1},
		{RagdollShinR, shin, lin.V3(hipX, shin.Length/2, 0), 0.06},
	}
	r := &Ragdoll3{Parts: map[string]ecs.Entity{}, Joints: map[string]ecs.Entity{}, Bones: map[string]RagdollBone{}}
	centres := map[string]lin.Vec3{}
	for _, p := range parts {
		body := Dynamic3(mass * p.share)
		body.Friction, body.Restitution = friction, 0
		body.LinearDamping, body.AngularDamping = damping, damping
		shape := Capsule{Radius: p.bone.Radius, HalfHeight: max(p.bone.Length/2-p.bone.Radius, 0)}
		e := w.SpawnWith(
			gfx.Transform{Position: spec.Position.Add(rot.Rotate(p.centre)), Rotation: rot},
			body, Collider3{Shape: shape, Layers: layers},
		)
		r.Parts[p.name], r.Bones[p.name], centres[p.name] = e, p.bone, p.centre
	}
	// Joints, with anchors as offsets from each part's centre in the
	// figure's frame, which is every part's local frame at rest.
	ball := func(parent, child string, at, axisA lin.Vec3, cone, twist float32) {
		r.Joints[child] = w.SpawnWith(BallJoint3{
			A: r.Parts[parent], B: r.Parts[child],
			AnchorA: at.Sub(centres[parent]), AnchorB: at.Sub(centres[child]),
			AxisA: axisA, ConeAngle: cone, TwistAngle: twist,
		})
	}
	hinge := func(parent, child string, at lin.Vec3, lo, hi float32) {
		r.Joints[child] = w.SpawnWith(HingeJoint3{
			A: r.Parts[parent], B: r.Parts[child],
			AnchorA: at.Sub(centres[parent]), AnchorB: at.Sub(centres[child]),
			AxisA: lin.V3(1, 0, 0), AxisB: lin.V3(1, 0, 0), MinAngle: lo, MaxAngle: hi,
		})
	}
	const s45 = float32(math.Sqrt2 / 2)
	ball(RagdollPelvis, RagdollSpine, lin.V3(0, waist, 0), lin.V3(0, 1, 0), 0.4, 0.4)
	ball(RagdollSpine, RagdollHead, lin.V3(0, neck, 0), lin.V3(0, 1, 0), 0.7, 1)
	// A shoulder's cone is centred out and down from the shoulder, so
	// the arm hangs, swings and lifts to the side but not overhead; an
	// elbow only bends forward. A hip's cone leans forward, so the leg
	// kicks further forward than back; a knee only bends back.
	ball(RagdollSpine, RagdollUpperArmL, lin.V3(-shoulderX, shoulderY, 0), lin.V3(s45, s45, 0), 1.5, 0.8)
	hinge(RagdollUpperArmL, RagdollForearmL, lin.V3(-shoulderX, elbow, 0), 0, 2.6)
	ball(RagdollSpine, RagdollUpperArmR, lin.V3(shoulderX, shoulderY, 0), lin.V3(-s45, s45, 0), 1.5, 0.8)
	hinge(RagdollUpperArmR, RagdollForearmR, lin.V3(shoulderX, elbow, 0), 0, 2.6)
	ball(RagdollPelvis, RagdollThighL, lin.V3(-hipX, hip, 0), lin.V3(0, 0.866, 0.5), 1.05, 0.5)
	hinge(RagdollThighL, RagdollShinL, lin.V3(-hipX, knee, 0), -2.6, 0)
	ball(RagdollPelvis, RagdollThighR, lin.V3(hipX, hip, 0), lin.V3(0, 0.866, 0.5), 1.05, 0.5)
	hinge(RagdollThighR, RagdollShinR, lin.V3(hipX, knee, 0), -2.6, 0)
	return r
}

// Entities returns every part and joint entity, parts first.
func (r *Ragdoll3) Entities() []ecs.Entity {
	var out []ecs.Entity
	for _, name := range RagdollParts {
		if e, ok := r.Parts[name]; ok {
			out = append(out, e)
		}
	}
	for _, name := range RagdollParts {
		if e, ok := r.Joints[name]; ok {
			out = append(out, e)
		}
	}
	return out
}

// Despawn removes every part and joint.
func (r *Ragdoll3) Despawn(w *ecs.World) {
	for _, e := range r.Entities() {
		w.Despawn(e)
	}
}

// Pose places the parts at once and brings them to rest, to hand an
// animated character over to physics: positions are part centres and
// rotations part rotations in the world, by part name; a part missing
// from a map keeps what it has. A game with a bone's world position
// and rotation finds the centre of the part hanging from it as the
// bone position plus the rotation applied to (0, -Length/2, 0), with
// Length from Bones.
func (r *Ragdoll3) Pose(w *ecs.World, positions map[string]lin.Vec3, rotations map[string]lin.Quat) {
	for name, e := range r.Parts {
		t, ok := w.Get[gfx.Transform](e)
		if !ok {
			continue
		}
		if p, ok := positions[name]; ok {
			t.Position = p
		}
		if q, ok := rotations[name]; ok {
			t.Rotation = q.Norm()
		}
		if b, ok := w.Get[Body3](e); ok {
			b.Vel, b.AngVel = lin.Vec3{}, lin.Vec3{}
			b.Wake()
		}
	}
}
