package gfx

import (
	"fmt"
	"math"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// maxProbes is how many reflection probes one frame's block holds.
const maxProbes = 8

// MaxProbes is how many reflection probes a frame keeps.
const MaxProbes = maxProbes

// ReflectionProbe is the environment of one part of a scene, captured
// from a point and reflected by the surfaces inside its volume: a red
// room that reddens the chrome in it, a cave that stays dark under a
// bright sky. Fill in the position and the volume, bake it once with
// BakeProbe, and add it to each frame with AddProbe. Draws whose centre
// falls inside the volume reflect the probe instead of the light's
// Environment or Sky; everything outside every probe keeps those.
//
// A probe changes reflections, not the diffuse ambient light, which comes
// from the light's environment or a LightProbeGrid.
type ReflectionProbe struct {
	// Position is where the cube map is captured, in world units. It is
	// also the centre a box projection reflects around, so put it where a
	// viewer looks from rather than in a wall.
	Position lin.Vec3
	// Extent is the half-size of the box the probe covers. A zero Extent
	// with a positive Radius makes a sphere probe instead. A probe with
	// neither covers nothing and is ignored.
	Extent lin.Vec3
	// Radius is the sphere probe's radius in world units; zero means the
	// probe is a box.
	Radius float32
	// Margin is how far inside the volume's edge the probe fades towards
	// the frame's own environment, in world units; zero means it does not
	// fade and the reflection changes at the boundary.
	Margin float32
	// Resolution is the cube face size in texels the bake renders and
	// prefilters; zero means 64. Larger is sharper in a mirror and slower
	// to bake.
	Resolution int
	// Intensity multiplies the probe's light; zero means 1.
	Intensity float32
	// BoxProjection reflects the box's walls at the place the ray meets
	// them rather than at infinity, so a floor mirrors the wall it faces.
	// It applies to box probes and is ignored by a sphere probe, which
	// always projects onto its sphere.
	BoxProjection bool

	env *Environment
}

// Environment returns the probe's baked environment, or nil before the
// first BakeProbe. It is the same form NewEnvironment builds, so it can
// be set as Light.Environment to light a whole scene from a probe.
func (p *ReflectionProbe) Environment() *Environment {
	if p == nil {
		return nil
	}
	return p.env
}

// Destroy frees the probe's cube map. Baking again destroys the previous
// one on its own, so this is for a probe a game is finished with.
func (p *ReflectionProbe) Destroy() {
	if p == nil || p.env == nil {
		return
	}
	p.env.Destroy()
	p.env = nil
}

// kind is what the frame block calls the probe's volume: 1 box, 2 sphere,
// 0 nothing to test.
func (p *ReflectionProbe) kind() float32 {
	switch {
	case p.Extent.X > 0 && p.Extent.Y > 0 && p.Extent.Z > 0:
		return 1
	case p.Radius > 0:
		return 2
	}
	return 0
}

// weight is how firmly a point sits inside the probe: 1 well inside, 0 at
// or outside the edge, ramping over the margin. It is the CPU side of the
// same fade the shader applies per fragment.
func (p *ReflectionProbe) weight(pos lin.Vec3) float32 {
	d := pos.Sub(p.Position)
	depth := float32(0)
	switch p.kind() {
	case 1:
		depth = min(p.Extent.X-abs32(d.X), p.Extent.Y-abs32(d.Y), p.Extent.Z-abs32(d.Z))
	case 2:
		depth = p.Radius - d.Len()
	default:
		return 0
	}
	if depth <= 0 {
		return 0
	}
	if p.Margin <= 0 {
		return 1
	}
	return min(depth/p.Margin, 1)
}

// volume is the probe's size, for choosing the tighter of two probes that
// both hold a draw.
func (p *ReflectionProbe) volume() float32 {
	if p.kind() == 2 {
		return p.Radius * p.Radius * p.Radius
	}
	return p.Extent.X * p.Extent.Y * p.Extent.Z
}

// AddProbe adds a reflection probe for this frame. Draws inside its
// volume reflect it; a frame keeps its first MaxProbes probes and counts
// the rest in FrameStats.ProbesDropped. A probe that has not been baked
// is ignored.
func (g *Graphics) AddProbe(p *ReflectionProbe) {
	if p != nil {
		g.requireEnvironmentOwner(p.env)
	}
	if p == nil || p.env == nil || p.env.cube == nil || p.kind() == 0 {
		return
	}
	q := g.cur
	if len(q.probes) >= maxProbes {
		g.stats.ProbesDropped++
		return
	}
	q.probes = append(q.probes, p)
}

// probeFor picks the probe a draw at centre reflects: the one holding it
// most firmly, and of two that hold it equally the smaller. It returns
// the index in the queue's list plus one, so zero means the frame's own
// environment. Only one cube map is bound per draw, so two probes are
// never blended; the margin fades a probe towards the frame's average
// environment near the volume's edge instead.
func (q *drawQueue) probeFor(centre lin.Vec3) int {
	best, bestWeight, bestVolume := 0, float32(0), float32(0)
	for i, p := range q.probes {
		w := p.weight(centre)
		if w <= 0 {
			continue
		}
		v := p.volume()
		if best == 0 || w > bestWeight || (w == bestWeight && v < bestVolume) {
			best, bestWeight, bestVolume = i+1, w, v
		}
	}
	return best
}

// probeEnv is the environment a draw's probe index selects, or the
// frame's own environment when the draw is outside every probe.
func (q *drawQueue) probeEnv(index int, env *Environment) *Environment {
	if index <= 0 || index > len(q.probes) {
		return env
	}
	return q.probes[index-1].env
}

// BakeProbe renders the scene from the probe's position into a cube map
// and prefilters it for every roughness. Call it from Init or Update, not
// from Draw: it submits its own command buffers and waits for them. The
// scene function queues the draws and lights the bake sees, exactly as
// Draw would, and the engine sets the camera for each of the six faces.
// A second call rebakes the probe and frees what the first one made.
func (g *Graphics) BakeProbe(p *ReflectionProbe, scene func()) error {
	if p == nil {
		return fmt.Errorf("gfx: BakeProbe needs a probe")
	}
	if g.frame != nil {
		return fmt.Errorf("gfx: BakeProbe cannot run inside Draw; call it from Init or Update")
	}
	size := p.Resolution
	if size <= 0 {
		size = 64
	}
	b, err := g.newBaker(size, scene)
	if err != nil {
		return err
	}
	defer b.destroy()
	faces, err := b.capture(p.Position)
	if err != nil {
		return err
	}
	env, err := g.newEnvironmentFrom(faces.sample, EnvironmentOptions{Size: size, Intensity: p.Intensity})
	if err != nil {
		return err
	}
	p.Destroy()
	p.env = env
	return nil
}

// baker renders a scene into cube faces from any number of positions. The
// draws are queued once and re-rendered per face, so a grid of probes
// costs one queue and one set of targets.
type baker struct {
	g     *Graphics
	size  int
	t     *sceneTargets
	q     *drawQueue
	stats FrameStats
}

// newBaker builds the offscreen targets and queue for size by size faces
// and runs scene once to fill the queue.
func (g *Graphics) newBaker(size int, scene func()) (*baker, error) {
	size = min(max(size, 8), 512)
	extent := vk.VkExtent2D{Width: uint32(size), Height: uint32(size)}
	b := &baker{g: g, size: size, stats: g.stats}
	var err error
	// A bake renders small faces that are then blurred into an irradiance
	// map, so it stays single-sample whatever the post settings ask for.
	if b.t, err = g.newSceneTargets(extent, vk.VK_SAMPLE_COUNT_1_BIT); err != nil {
		return nil, err
	}
	if b.q, err = g.newQueue(float32(size), float32(size)); err != nil {
		b.destroy()
		return nil, err
	}
	prev := g.cur
	g.cur = b.q
	b.q.reset()
	if scene != nil {
		scene()
	}
	g.cur = prev
	// A shader's uniform blocks live in the arena the frame writes at its
	// end; the bake writes slot 0 itself, since it renders outside a frame.
	if err := g.uniforms.Write(0, g.arena.Bytes()); err != nil {
		b.destroy()
		return nil, err
	}
	return b, nil
}

func (b *baker) destroy() {
	if b.q != nil {
		b.q.destroy()
		b.q = nil
	}
	if b.t != nil {
		b.t.destroy(b.g)
		b.t = nil
	}
	b.g.stats = b.stats // a bake is not part of the frame's counts
}

// capture renders the six faces from pos and reads them back.
func (b *baker) capture(pos lin.Vec3) (*cubeFaces, error) {
	faces := &cubeFaces{side: b.size}
	for face := range 6 {
		b.q.camera = faceCamera(pos, face)
		b.q.hasCam = true
		var inner error
		err := b.g.r.Device.OneShot(func(cb vk.VkCommandBuffer) {
			fr := &render.Frame{CB: cb, Slot: 0, Extent: b.t.extent}
			inner = b.g.renderScene(fr, b.q, b.t)
		})
		if err != nil {
			return nil, err
		}
		if inner != nil {
			return nil, inner
		}
		pix, err := b.g.r.Device.ReadImageRaw(b.t.hdr.Color, 8)
		if err != nil {
			return nil, err
		}
		faces.pix[face] = pix
	}
	return faces, nil
}

// faceCamera looks along one cube face from pos, with the field of view
// and orientation the face's texels are laid out in (see cubeDir).
func faceCamera(pos lin.Vec3, face int) Camera {
	dir := cubeDir(face, 0, 0)
	up := lin.V3(0, 1, 0)
	switch face {
	case 2:
		up = lin.V3(0, 0, -1)
	case 3:
		up = lin.V3(0, 0, 1)
	}
	return Camera{Position: pos, Target: pos.Add(dir), Up: up, FovY: math.Pi / 2, Near: 0.05, Far: 1000}
}

// cubeFaces is one bake's six square faces of half-float RGBA radiance,
// in Vulkan's face order.
type cubeFaces struct {
	side int
	pix  [6][]byte
}

// texel reads one face's texel, clamped to the face's edge. The rendered
// image runs the other way along u than the cube face convention does, so
// x is mirrored here rather than in the camera.
func (c *cubeFaces) texel(face, x, y int) (r, g, b float32) {
	p := c.pix[face]
	if len(p) < c.side*c.side*8 {
		return 0, 0, 0
	}
	x = min(max(x, 0), c.side-1)
	y = min(max(y, 0), c.side-1)
	i := ((y * c.side) + (c.side - 1 - x)) * 8
	return getF16(p[i:]), getF16(p[i+2:]), getF16(p[i+4:])
}

// sample reads the radiance arriving from a direction, bilinearly inside
// the face the direction falls on.
func (c *cubeFaces) sample(d lin.Vec3) (r, g, b float32) {
	ax, ay, az := abs32(d.X), abs32(d.Y), abs32(d.Z)
	var face int
	var u, v, m float32
	switch {
	case ax >= ay && ax >= az:
		m = max(ax, 1e-9)
		if d.X > 0 {
			face, u, v = 0, -d.Z/m, -d.Y/m
		} else {
			face, u, v = 1, d.Z/m, -d.Y/m
		}
	case ay >= az:
		m = max(ay, 1e-9)
		if d.Y > 0 {
			face, u, v = 2, d.X/m, d.Z/m
		} else {
			face, u, v = 3, d.X/m, -d.Z/m
		}
	default:
		m = max(az, 1e-9)
		if d.Z > 0 {
			face, u, v = 4, d.X/m, -d.Y/m
		} else {
			face, u, v = 5, -d.X/m, -d.Y/m
		}
	}
	fx := (u+1)*0.5*float32(c.side) - 0.5
	fy := (v+1)*0.5*float32(c.side) - 0.5
	x0, y0 := int(math.Floor(float64(fx))), int(math.Floor(float64(fy)))
	tx, ty := fx-float32(x0), fy-float32(y0)
	r00, g00, b00 := c.texel(face, x0, y0)
	r10, g10, b10 := c.texel(face, x0+1, y0)
	r01, g01, b01 := c.texel(face, x0, y0+1)
	r11, g11, b11 := c.texel(face, x0+1, y0+1)
	lerp := func(a, b, t float32) float32 { return a + (b-a)*t }
	return lerp(lerp(r00, r10, tx), lerp(r01, r11, tx), ty),
		lerp(lerp(g00, g10, tx), lerp(g01, g11, tx), ty),
		lerp(lerp(b00, b10, tx), lerp(b01, b11, tx), ty)
}
