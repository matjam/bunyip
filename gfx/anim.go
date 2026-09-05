package gfx

import (
	"math"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

// AnimPlayer plays a model's animation clips over its node hierarchy. One
// player per animated instance; it holds the current pose. Play and
// CrossFade choose the main clip, SetBlend mixes several clips in its
// place, Layer plays more clips over parts of the skeleton, AddEvent
// marks moments to be told about, SetRootMotion hands the root's
// movement to the game, and PostPose with the node setters adjusts the
// pose before it is drawn.
type AnimPlayer struct {
	// OnEvent, when set, is called from Advance for every event playback
	// crosses; Events lists the same after Advance returns.
	OnEvent func(AnimEvent)
	// PostPose, when set, runs at the end of every Advance with the pose
	// built, before joint matrices are made: the place for inverse
	// kinematics, look-at and any other node override.
	PostPose func(p *AnimPlayer)

	model  *Model
	speed  float64
	cur    animTrack   // the main clip
	prev   animTrack   // the clip fading out under a crossfade
	fade   float64     // seconds of fade left
	fadeD  float64     // the fade's length
	blend  []animBlend // the weighted clips set by SetBlend, in place of cur
	layers []*AnimLayer

	rest  animPose // the rest pose and default morph weights
	pose  animPose // the blended pose
	tmp   animPose // scratch for sampling one clip
	world []lin.Mat4
	dirty bool // world is stale after a node override

	events []animEvent
	fired  []AnimEvent

	rootNode  int // -1 when root motion is off
	rootDelta lin.Vec3
	rootYaw   float32
}

// AnimEvent is a moment in a clip that playback crossed: a footstep, a
// hit frame, a spawn point.
type AnimEvent struct {
	Clip string
	Time float32
	Name string
}

type animEvent struct {
	clip int
	time float32
	name string
}

// animTrack is one playing clip.
type animTrack struct {
	clip  int // -1 for none
	time  float64
	loop  bool
	fresh bool // just started: events at the start time fire on the next Advance
}

// AnimBlend is one clip's share of a blended pose, for SetBlend: the
// clip, its weight against the others and the time to sample it at.
type AnimBlend struct {
	Clip   string
	Weight float32
	Time   float64
}

// animBlend is a clip in the blend set by SetBlend.
type animBlend struct {
	track  animTrack
	weight float32
	last   float64 // the clip time at the previous Advance
}

// AnimLayer plays a clip over part of the skeleton on top of the main
// clip. Get one from Layer.
type AnimLayer struct {
	Weight float32 // how much of the layer shows, 0..1
	// Additive adds the clip's difference from the rest pose to the pose
	// underneath (a breathing motion over anything, a recoil); off, the
	// layer replaces the pose of the nodes it covers (a wave over a walk).
	Additive bool
	// Loop starts the clip over at its end; off, the layer holds the last
	// frame. Layer sets it.
	Loop bool
	// Mask is the set of nodes the layer affects; nil means every node.
	Mask AnimMask

	name     string
	duration float32
	track    animTrack
}

// animPose is a pose in local terms: a transform per node and the morph
// weights of nodes that have targets.
type animPose struct {
	local []nodeTRS
	morph [][]float32 // per node; nil for nodes without morph targets
}

type nodeTRS struct {
	t lin.Vec3
	r lin.Quat
	s lin.Vec3
}

func (a *animPose) init(n int) {
	a.local = make([]nodeTRS, n)
	a.morph = make([][]float32, n)
}

// copyFrom makes a the same pose as b; both must be sized alike.
func (a *animPose) copyFrom(b *animPose) {
	copy(a.local, b.local)
	for i, w := range b.morph {
		if w == nil {
			a.morph[i] = nil
			continue
		}
		a.morph[i] = append(a.morph[i][:0], w...)
	}
}

// NewAnimPlayer makes a player for the model in its rest pose.
func (m *Model) NewAnimPlayer() *AnimPlayer {
	p := &AnimPlayer{model: m, speed: 1, rootNode: -1}
	p.cur.clip, p.prev.clip = -1, -1
	n := len(m.nodes)
	p.rest.init(n)
	p.pose.init(n)
	p.tmp.init(n)
	for i, node := range m.nodes {
		p.rest.local[i] = nodeTRS{node.Translation, node.Rotation, node.Scale}
	}
	for _, mm := range m.morphs {
		if mm.node >= 0 && mm.node < n && p.rest.morph[mm.node] == nil {
			p.rest.morph[mm.node] = append([]float32(nil), mm.rest...)
		}
	}
	p.pose.copyFrom(&p.rest)
	p.world = make([]lin.Mat4, n)
	p.evaluate()
	return p
}

// Clips lists the model's animation names.
func (m *Model) Clips() []string {
	names := make([]string, len(m.clips))
	for i, c := range m.clips {
		names[i] = c.Name
	}
	return names
}

// ClipDuration returns a clip's length in seconds; unknown names give 0.
func (m *Model) ClipDuration(name string) float32 {
	if i := m.clipIndex(name); i >= 0 {
		return m.clips[i].Duration
	}
	return 0
}

func (m *Model) clipIndex(name string) int {
	for i, c := range m.clips {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// Model is the model the player animates.
func (p *AnimPlayer) Model() *Model { return p.model }

// Play starts a clip by name from its beginning, dropping any crossfade
// or blend; unknown names return false.
func (p *AnimPlayer) Play(name string, loop bool) bool {
	i := p.model.clipIndex(name)
	if i < 0 {
		return false
	}
	p.PlayIndex(i, loop)
	return true
}

// PlayIndex starts a clip by index.
func (p *AnimPlayer) PlayIndex(i int, loop bool) {
	if i < 0 || i >= len(p.model.clips) {
		return
	}
	p.cur = animTrack{clip: i, loop: loop, fresh: true}
	p.prev.clip = -1
	p.fade, p.fadeD = 0, 0
	p.blend = p.blend[:0]
	p.Advance(0)
}

// CrossFade starts a clip while the current one blends out over the
// given seconds, so a run does not snap out of a walk. With nothing
// playing, a blend playing, or a zero fade, it is Play.
func (p *AnimPlayer) CrossFade(name string, loop bool, seconds float64) bool {
	i := p.model.clipIndex(name)
	if i < 0 {
		return false
	}
	if p.cur.clip < 0 || seconds <= 0 {
		p.PlayIndex(i, loop)
		return true
	}
	p.prev = p.cur
	p.cur = animTrack{clip: i, loop: loop, fresh: true}
	p.fade, p.fadeD = seconds, seconds
	p.Advance(0)
	return true
}

// Stop drops the main clip or blend, leaving the pose where it is;
// layers keep playing.
func (p *AnimPlayer) Stop() {
	p.cur.clip, p.prev.clip = -1, -1
	p.blend = p.blend[:0]
}

// SetBlend plays a weighted mix of clips in place of the main clip, each
// sampled at its own time: what a blend space produces. The weights are
// scaled to sum to 1; entries with no weight, or an unknown clip, are
// skipped, and an empty list stops the blend. The caller owns the times
// and sets them again before every Advance; a time that moved backwards
// counts as having looped. Events fire and root motion accrues for
// every clip in the blend by its weight. Play, CrossFade and Stop drop
// the blend; layers play over it as they do over a clip.
func (p *AnimPlayer) SetBlend(clips []AnimBlend) {
	old := p.blend
	p.blend = make([]animBlend, 0, len(clips))
	for _, c := range clips {
		i := p.model.clipIndex(c.Clip)
		if i < 0 || c.Weight <= 0 {
			continue
		}
		b := animBlend{track: animTrack{clip: i, loop: true, fresh: true, time: c.Time}, weight: c.Weight, last: c.Time}
		for _, o := range old {
			if o.track.clip == i {
				b.track.fresh, b.last = o.track.fresh, o.track.time
				break
			}
		}
		p.blend = append(p.blend, b)
	}
	if len(p.blend) > 0 {
		p.cur.clip, p.prev.clip = -1, -1
		p.fade, p.fadeD = 0, 0
	}
}

// Blend lists the clips SetBlend is playing with their weights as given
// and their times, in a new slice; nil when no blend plays.
func (p *AnimPlayer) Blend() []AnimBlend {
	if len(p.blend) == 0 {
		return nil
	}
	out := make([]AnimBlend, len(p.blend))
	for i, b := range p.blend {
		out[i] = AnimBlend{Clip: p.model.clips[b.track.clip].Name, Weight: b.weight, Time: b.track.time}
	}
	return out
}

// Clip is the name of the main clip, or "" when none plays.
func (p *AnimPlayer) Clip() string {
	if p.cur.clip < 0 {
		return ""
	}
	return p.model.clips[p.cur.clip].Name
}

// Finished reports whether a non-looping main clip has reached its end.
func (p *AnimPlayer) Finished() bool {
	if p.cur.clip < 0 || p.cur.loop {
		return false
	}
	return p.cur.time >= float64(p.model.clips[p.cur.clip].Duration)
}

// SetSpeed scales playback; 1 is normal, negative runs clips backwards.
func (p *AnimPlayer) SetSpeed(s float64) { p.speed = s }

// Speed is the playback scale set by SetSpeed; 1 by default.
func (p *AnimPlayer) Speed() float64 { return p.speed }

// Time is the main clip's current time in seconds.
func (p *AnimPlayer) Time() float64 { return p.cur.time }

// SetTime moves the main clip to a time in seconds, as scrubbing does;
// events between the old and new times do not fire.
func (p *AnimPlayer) SetTime(t float64) {
	if p.cur.clip < 0 {
		return
	}
	p.cur.time = t
	p.step(&p.cur, 0)
	p.Advance(0)
}

// Layer plays a clip on top of the main one over the nodes in the mask
// (nil for all of them), looping, at the given weight: a wave over the
// arms while the legs walk. Change the returned layer's fields at any
// time; RemoveLayer takes it off. Unknown clips return nil.
func (p *AnimPlayer) Layer(clip string, weight float32, mask AnimMask) *AnimLayer {
	i := p.model.clipIndex(clip)
	if i < 0 {
		return nil
	}
	l := &AnimLayer{Weight: weight, Loop: true, Mask: mask, name: clip, duration: p.model.clips[i].Duration,
		track: animTrack{clip: i, loop: true, fresh: true}}
	p.layers = append(p.layers, l)
	return l
}

// Layers lists the playing layers in the order they blend, first to last.
func (p *AnimPlayer) Layers() []*AnimLayer { return p.layers }

// RemoveLayer stops a layer.
func (p *AnimPlayer) RemoveLayer(l *AnimLayer) {
	for i, x := range p.layers {
		if x == l {
			p.layers = append(p.layers[:i], p.layers[i+1:]...)
			return
		}
	}
}

// Clip is the layer's clip name.
func (l *AnimLayer) Clip() string { return l.name }

// Time is the layer's clip time in seconds.
func (l *AnimLayer) Time() float64 { return l.track.time }

// Finished reports whether a non-looping layer has reached its clip's end.
func (l *AnimLayer) Finished() bool {
	return !l.Loop && l.track.time >= float64(l.duration)
}

// AddEvent marks a time in a clip; Advance reports crossing it through
// OnEvent and Events, on every loop and while the clip blends in, out or
// on a layer. Unknown clips return false.
func (p *AnimPlayer) AddEvent(clip string, time float32, name string) bool {
	i := p.model.clipIndex(clip)
	if i < 0 {
		return false
	}
	p.events = append(p.events, animEvent{clip: i, time: time, name: name})
	return true
}

// Events lists the events the last Advance crossed, in clip order; the
// slice is reused by the next Advance.
func (p *AnimPlayer) Events() []AnimEvent { return p.fired }

// SetRootMotion names the node whose movement the game applies to the
// entity instead of the animation sliding it in place: usually the
// skeleton's root or hips. From then on the node's translation and its
// yaw (rotation about +Y) are held at the rest pose and their change per
// Advance is reported by RootMotion. "" turns root motion off; an unknown
// name returns false.
func (p *AnimPlayer) SetRootMotion(node string) bool {
	if node == "" {
		p.rootNode = -1
		return true
	}
	i := p.model.NodeIndex(node)
	if i < 0 {
		return false
	}
	p.rootNode = i
	return true
}

// RootMotion returns how far the root node moved during the last
// Advance, in model space, and how much it turned about +Y in radians.
// Apply them to the entity's transform: position += rotation.Rotate(delta),
// then turn by yaw. Both are zero unless SetRootMotion is on.
func (p *AnimPlayer) RootMotion() (delta lin.Vec3, yaw float32) {
	return p.rootDelta, p.rootYaw
}

// Advance moves playback forward by dt seconds and rebuilds the pose:
// the main clip or blend, its crossfade, the layers, root motion,
// events and PostPose, in that order.
func (p *AnimPlayer) Advance(dt float64) {
	p.fired = p.fired[:0]
	p.rootDelta, p.rootYaw = lin.Vec3{}, 0
	if p.cur.clip < 0 && p.prev.clip < 0 && len(p.blend) == 0 && len(p.layers) == 0 {
		// Nothing plays: the pose, and any overrides on it, stay.
		if p.PostPose != nil {
			p.PostPose(p)
		}
		p.ensure()
		return
	}
	step := dt * p.speed
	forward := step >= 0
	p.pose.copyFrom(&p.rest)
	var rootDelta lin.Vec3
	var rootYaw float32
	weight := float32(1)
	if p.prev.clip >= 0 {
		from, to, wrapped := p.step(&p.prev, step)
		p.fire(&p.prev, from, to, wrapped, forward)
		p.fade -= dt
		if p.fade <= 0 {
			p.prev.clip = -1
		} else {
			weight = float32(1 - p.fade/p.fadeD)
			p.sample(&p.prev, &p.pose)
			d, y := p.rootMotionOf(&p.prev, from, to, wrapped, forward)
			rootDelta, rootYaw = d.Mul(1-weight), y*(1-weight)
		}
	}
	if p.cur.clip >= 0 {
		from, to, wrapped := p.step(&p.cur, step)
		p.fire(&p.cur, from, to, wrapped, forward)
		p.sample(&p.cur, &p.tmp)
		p.pose.blend(&p.tmp, &p.rest, weight, nil, false)
		d, y := p.rootMotionOf(&p.cur, from, to, wrapped, forward)
		rootDelta, rootYaw = rootDelta.Add(d.Mul(weight)), rootYaw+y*weight
	}
	if len(p.blend) > 0 {
		var total float32
		for _, b := range p.blend {
			total += b.weight
		}
		// Each clip blends over the ones before it by its share of the
		// weight so far, which leaves the pose the weighted mean.
		var sofar float32
		for i := range p.blend {
			b := &p.blend[i]
			from, to := b.last, b.track.time
			wrapped := (forward && to < from) || (!forward && to > from)
			p.fire(&b.track, from, to, wrapped, forward)
			w := b.weight / total
			sofar += w
			p.sample(&b.track, &p.tmp)
			p.pose.blend(&p.tmp, &p.rest, w/sofar, nil, false)
			d, y := p.rootMotionOf(&b.track, from, to, wrapped, forward)
			rootDelta, rootYaw = rootDelta.Add(d.Mul(w)), rootYaw+y*w
			b.last = to
		}
	}
	for _, l := range p.layers {
		l.track.loop = l.Loop
		from, to, wrapped := p.step(&l.track, step)
		p.fire(&l.track, from, to, wrapped, forward)
		if l.Weight <= 0 {
			continue
		}
		p.sample(&l.track, &p.tmp)
		p.pose.blend(&p.tmp, &p.rest, min(l.Weight, 1), l.Mask, l.Additive)
	}
	if r := p.rootNode; r >= 0 && r < len(p.pose.local) {
		n := &p.pose.local[r]
		n.t = p.rest.local[r].t
		n.r = lin.AxisAngle(lin.V3(0, 1, 0), -yawOf(n.r)).Mul(n.r).Norm()
	}
	p.evaluate()
	if r := p.rootNode; r >= 0 && r < len(p.model.nodes) {
		if parent := p.model.nodes[r].Parent; parent >= 0 {
			rootDelta = p.world[parent].MulVec4(rootDelta.Vec4(0)).Vec3()
		}
		p.rootDelta, p.rootYaw = rootDelta, rootYaw
	}
	if p.PostPose != nil {
		p.PostPose(p)
		p.ensure()
	}
}

// step moves a track by step seconds, looping or clamping, and reports
// the times it moved between and whether it wrapped.
func (p *AnimPlayer) step(tr *animTrack, step float64) (from, to float64, wrapped bool) {
	d := float64(p.model.clips[tr.clip].Duration)
	from = tr.time
	to = from + step
	switch {
	case d <= 0:
		to = 0
	case tr.loop:
		if to >= d || to < 0 {
			wrapped = true
			to = math.Mod(to, d)
			if to < 0 {
				to += d
			}
		}
	default:
		to = max(0, min(to, d))
	}
	tr.time = to
	return from, to, wrapped
}

// fire reports the track's events between from and to.
func (p *AnimPlayer) fire(tr *animTrack, from, to float64, wrapped, forward bool) {
	for _, e := range p.events {
		if e.clip != tr.clip {
			continue
		}
		t := float64(e.time)
		var hit bool
		switch {
		case tr.fresh:
			hit = (forward && t <= to) || (!forward && t >= to)
		case wrapped && forward:
			hit = t > from || t <= to
		case wrapped:
			hit = t < from || t >= to
		case forward:
			hit = t > from && t <= to
		default:
			hit = t < from && t >= to
		}
		if hit {
			ev := AnimEvent{Clip: p.model.clips[e.clip].Name, Time: e.time, Name: e.name}
			p.fired = append(p.fired, ev)
			if p.OnEvent != nil {
				p.OnEvent(ev)
			}
		}
	}
	tr.fresh = false
}

// sample writes the track's clip at its current time over the rest pose
// into out.
func (p *AnimPlayer) sample(tr *animTrack, out *animPose) {
	out.copyFrom(&p.rest)
	clip := p.model.clips[tr.clip]
	t := float32(tr.time)
	for _, ch := range clip.Channels {
		if ch.Node < 0 || ch.Node >= len(out.local) || len(ch.Times) == 0 {
			continue
		}
		if ch.Path == gltf.PathWeights {
			if w := out.morph[ch.Node]; w != nil {
				sampleWeights(ch, t, w)
			}
			continue
		}
		applyChannel(ch, t, &out.local[ch.Node])
	}
}

// sampleNode samples only one node of a clip, for root motion.
func (p *AnimPlayer) sampleNode(clip gltf.Animation, node int, t float32) nodeTRS {
	n := p.rest.local[node]
	for _, ch := range clip.Channels {
		if ch.Node == node && ch.Path != gltf.PathWeights && len(ch.Times) > 0 {
			applyChannel(ch, t, &n)
		}
	}
	return n
}

func applyChannel(ch gltf.Channel, t float32, n *nodeTRS) {
	v := sampleChannel(ch, t)
	switch ch.Path {
	case gltf.PathTranslation:
		n.t = v.Vec3()
	case gltf.PathRotation:
		n.r = lin.Quat{X: v.X, Y: v.Y, Z: v.Z, W: v.W}.Norm()
	case gltf.PathScale:
		n.s = v.Vec3()
	}
}

// blend mixes src into a by weight. With a mask only flagged nodes
// change. Additive adds src's difference from rest instead of replacing.
func (a *animPose) blend(src, rest *animPose, weight float32, mask AnimMask, additive bool) {
	for i := range a.local {
		if mask != nil && (i >= len(mask) || !mask[i]) {
			continue
		}
		s := src.local[i]
		n := &a.local[i]
		if additive {
			r := rest.local[i]
			n.t = n.t.Add(s.t.Sub(r.t).Mul(weight))
			n.r = n.r.Mul(lin.QuatIdentity().Slerp(conj(r.r).Mul(s.r), weight)).Norm()
			n.s = lin.V3(n.s.X*lerp(1, ratio(s.s.X, r.s.X), weight), n.s.Y*lerp(1, ratio(s.s.Y, r.s.Y), weight), n.s.Z*lerp(1, ratio(s.s.Z, r.s.Z), weight))
		} else if weight >= 1 {
			*n = s
		} else {
			n.t = n.t.Lerp(s.t, weight)
			n.r = n.r.Slerp(s.r, weight)
			n.s = n.s.Lerp(s.s, weight)
		}
		if w := a.morph[i]; w != nil && src.morph[i] != nil {
			for k := range w {
				if k >= len(src.morph[i]) {
					break
				}
				if additive {
					base := float32(0)
					if k < len(rest.morph[i]) {
						base = rest.morph[i][k]
					}
					w[k] += (src.morph[i][k] - base) * weight
				} else {
					w[k] = lerp(w[k], src.morph[i][k], weight)
				}
			}
		}
	}
}

func lerp(a, b, t float32) float32 { return a + (b-a)*t }

func ratio(a, b float32) float32 {
	if b == 0 {
		return 1
	}
	return a / b
}

// conj is the inverse of a unit quaternion.
func conj(q lin.Quat) lin.Quat { return lin.Quat{X: -q.X, Y: -q.Y, Z: -q.Z, W: q.W} }

// rootMotionOf is how far the root node moved between two times of a
// track, in its parent's space, crossing the loop point when it wrapped.
func (p *AnimPlayer) rootMotionOf(tr *animTrack, from, to float64, wrapped, forward bool) (lin.Vec3, float32) {
	r := p.rootNode
	if r < 0 || r >= len(p.rest.local) {
		return lin.Vec3{}, 0
	}
	clip := p.model.clips[tr.clip]
	at := func(t float64) (lin.Vec3, float32) {
		n := p.sampleNode(clip, r, float32(t))
		return n.t, yawOf(n.r)
	}
	t0, y0 := at(from)
	t1, y1 := at(to)
	if !wrapped {
		return t1.Sub(t0), wrapAngle(y1 - y0)
	}
	te, ye := at(float64(clip.Duration))
	ts, ys := at(0)
	if forward {
		return te.Sub(t0).Add(t1.Sub(ts)), wrapAngle(ye-y0) + wrapAngle(y1-ys)
	}
	return ts.Sub(t0).Add(t1.Sub(te)), wrapAngle(ys-y0) + wrapAngle(y1-ye)
}

// yawOf is a rotation's turn about +Y, from where it takes +Z.
func yawOf(q lin.Quat) float32 {
	f := q.Rotate(lin.V3(0, 0, 1))
	if f.X*f.X+f.Z*f.Z < 1e-8 {
		r := q.Rotate(lin.V3(1, 0, 0))
		return float32(math.Atan2(float64(-r.Z), float64(r.X)))
	}
	return float32(math.Atan2(float64(f.X), float64(f.Z)))
}

// wrapAngle brings an angle into (-pi, pi].
func wrapAngle(a float32) float32 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a <= -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

// evaluate composes world matrices parent-first; nodes are visited in
// index order after a topological pass computed at load.
func (p *AnimPlayer) evaluate() {
	for _, i := range p.model.order {
		n := p.model.nodes[i]
		local := lin.TRS(p.pose.local[i].t, p.pose.local[i].r, p.pose.local[i].s)
		if n.Parent >= 0 {
			p.world[i] = p.world[n.Parent].Mul(local)
		} else {
			p.world[i] = local
		}
	}
	p.dirty = false
}

// ensure rebuilds the world matrices after a node override.
func (p *AnimPlayer) ensure() {
	if p.dirty {
		p.evaluate()
	}
}

// NodeMatrix returns a node's current world matrix (in model space).
func (p *AnimPlayer) NodeMatrix(node int) lin.Mat4 {
	if node < 0 || node >= len(p.world) {
		return lin.Identity()
	}
	p.ensure()
	return p.world[node]
}

// NodePosition returns a node's position in model space.
func (p *AnimPlayer) NodePosition(node int) lin.Vec3 {
	m := p.NodeMatrix(node)
	return lin.V3(m[12], m[13], m[14])
}

// NodeRotation returns a node's rotation in model space.
func (p *AnimPlayer) NodeRotation(node int) lin.Quat {
	return rotationOf(p.NodeMatrix(node))
}

// rotationOf extracts the rotation of a TRS matrix, dropping its scale.
func rotationOf(m lin.Mat4) lin.Quat {
	for c := range 3 {
		l := lin.V3(m[c*4], m[c*4+1], m[c*4+2]).Len()
		if l > 0 {
			m[c*4], m[c*4+1], m[c*4+2] = m[c*4]/l, m[c*4+1]/l, m[c*4+2]/l
		}
	}
	return lin.QuatFromMat4(m)
}

// NodeLocal returns a node's current local translation, rotation and
// scale, relative to its parent.
func (p *AnimPlayer) NodeLocal(node int) (t lin.Vec3, r lin.Quat, s lin.Vec3) {
	if node < 0 || node >= len(p.pose.local) {
		return lin.Vec3{}, lin.QuatIdentity(), lin.V3(1, 1, 1)
	}
	n := p.pose.local[node]
	return n.t, n.r, n.s
}

// SetNodeLocal replaces a node's local transform in the current pose: an
// aimed turret, a procedural tail. The next Advance samples the clips
// again, so call it after Advance or from PostPose each frame.
func (p *AnimPlayer) SetNodeLocal(node int, t lin.Vec3, r lin.Quat, s lin.Vec3) {
	if node < 0 || node >= len(p.pose.local) {
		return
	}
	p.pose.local[node] = nodeTRS{t, r.Norm(), s}
	p.dirty = true
}

// SetNodeRotation replaces a node's local rotation in the current pose.
func (p *AnimPlayer) SetNodeRotation(node int, r lin.Quat) {
	if node < 0 || node >= len(p.pose.local) {
		return
	}
	p.pose.local[node].r = r.Norm()
	p.dirty = true
}

// RotateNode turns a node by a rotation given in model space, about its
// own position, so its children follow: what an inverse kinematics or
// look-at solver produces.
func (p *AnimPlayer) RotateNode(node int, q lin.Quat) {
	if node < 0 || node >= len(p.pose.local) {
		return
	}
	parent := lin.QuatIdentity()
	if pi := p.model.nodes[node].Parent; pi >= 0 {
		parent = p.NodeRotation(pi)
	}
	// world' = q · world = q · parent · local, so local' = parent⁻¹ · q · parent · local.
	local := &p.pose.local[node].r
	*local = conj(parent).Mul(q).Mul(parent).Mul(*local).Norm()
	p.dirty = true
}

// MorphWeights returns a node's morph target weights in the current
// pose, one per target; nil when the node has none.
func (p *AnimPlayer) MorphWeights(node int) []float32 {
	if node < 0 || node >= len(p.pose.morph) {
		return nil
	}
	return p.pose.morph[node]
}

// SetMorphWeights sets the weights a node's morph targets start from on
// every Advance, until a playing clip's weights channel replaces them: a
// smile held while the body animates. Weights beyond the target count
// are ignored.
func (p *AnimPlayer) SetMorphWeights(node int, weights []float32) {
	if node < 0 || node >= len(p.rest.morph) || p.rest.morph[node] == nil {
		return
	}
	copy(p.rest.morph[node], weights)
	copy(p.pose.morph[node], weights)
}

// jointMatrices returns the skin's joint matrices for the current pose.
func (p *AnimPlayer) jointMatrices(skin int, out []lin.Mat4) []lin.Mat4 {
	if skin < 0 || skin >= len(p.model.skins) {
		return out
	}
	p.ensure()
	s := p.model.skins[skin]
	for i, j := range s.Joints {
		out = append(out, p.world[j].Mul(s.InverseBind[i]))
	}
	return out
}

// keyPair finds the keys around t: their indices and the fraction between.
func keyPair(times []float32, t float32) (lo, hi int, f float32) {
	last := len(times) - 1
	if t <= times[0] {
		return 0, 0, 0
	}
	if t >= times[last] {
		return last, last, 0
	}
	lo, hi = 0, last
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	if span := times[hi] - times[lo]; span > 0 {
		f = (t - times[lo]) / span
	}
	return lo, hi, f
}

// sampleChannel interpolates a channel at time t.
func sampleChannel(ch gltf.Channel, t float32) lin.Vec4 {
	lo, hi, f := keyPair(ch.Times, t)
	if lo == hi || ch.Step {
		return ch.Values[lo]
	}
	a, b := ch.Values[lo], ch.Values[hi]
	if ch.Path == gltf.PathRotation {
		q := lin.Quat{X: a.X, Y: a.Y, Z: a.Z, W: a.W}.Slerp(lin.Quat{X: b.X, Y: b.Y, Z: b.Z, W: b.W}, f)
		return lin.V4(q.X, q.Y, q.Z, q.W)
	}
	return a.Add(b.Sub(a).Mul(f))
}

// sampleWeights interpolates a weights channel at time t into out.
func sampleWeights(ch gltf.Channel, t float32, out []float32) {
	per := ch.WeightCount()
	if per == 0 {
		return
	}
	lo, hi, f := keyPair(ch.Times, t)
	a := ch.Weights[lo*per : lo*per+per]
	b := ch.Weights[hi*per : hi*per+per]
	for k := range out {
		if k >= per {
			break
		}
		if lo == hi || ch.Step {
			out[k] = a[k]
		} else {
			out[k] = lerp(a[k], b[k], f)
		}
	}
}

// DrawModelAnimated draws a model under a transform with the player's
// pose: node-animated parts move rigidly, skinned parts deform and morph
// targets blend to the player's weights.
func (g *Graphics) DrawModelAnimated(m *Model, t Transform, p *AnimPlayer) {
	g.DrawModelAnimatedWith(m, t, p, nil)
}

// DrawModelAnimatedWith is DrawModelAnimated with a material override,
// so a posed character can be drawn in a team colour or with one part
// swapped. A nil override draws the file's materials.
func (g *Graphics) DrawModelAnimatedWith(m *Model, t Transform, p *AnimPlayer, override MaterialOverride) {
	g.DrawModelAnimatedMoved(m, t, t, p, override)
}

// DrawModelAnimatedMoved is DrawModelAnimatedWith for a model that
// moved: prev is the transform it was drawn with last frame, which the
// velocity buffer carries for temporal anti-aliasing and motion blur.
// The pose's own motion is not carried; see DrawSkinnedMoved.
func (g *Graphics) DrawModelAnimatedMoved(m *Model, t, prev Transform, p *AnimPlayer, override MaterialOverride) {
	world, was := t.Matrix(), prev.Matrix()
	for _, mm := range m.morphs {
		if w := p.MorphWeights(mm.node); w != nil {
			_ = mm.apply(w)
		}
	}
	var joints []lin.Mat4
	for i, part := range m.Parts {
		mat := override.apply(i, part)
		if part.Mesh.skinned && part.skin >= 0 {
			joints = p.jointMatrices(part.skin, joints[:0])
			g.DrawSkinnedMoved(part.Mesh, mat, world, was, joints)
			continue
		}
		node := p.NodeMatrix(part.node)
		g.DrawMeshMoved(part.Mesh, mat, world.Mul(node), was.Mul(node))
	}
}
