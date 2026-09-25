package gfx

import (
	"math"
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// The atmosphere's lookup tables, in texels. The ATMOSPHERE MAPPING
// block in atmoslut.frag.wgsl, prelude_mesh.wgsl and skyparam.frag.wgsl
// declares the same sizes.
const (
	// The transmittance table: the optical depth to the top of the air
	// by the ray's angle across and the height down.
	atmosTransW, atmosTransH = 256, 64
	// The sky view: the azimuth from the sun across, the angle from the
	// zenith down, one block for air over one for haze.
	atmosSkyW, atmosSkyH = 128, 128
	// The aerial perspective: the sky view's mapping at a coarser size,
	// at atmosAerialD distances side by side, air over haze.
	atmosAerialW, atmosAerialH, atmosAerialD = 32, 64, 32
	// The reflection table: the sky's colour for every direction, on an
	// octahedral mapping, for the lit meshes' reflections.
	atmosReflectN = 256
	atmosFormat   = vk.VK_FORMAT_R16G16B16A16_SFLOAT
	// atmosAltitudeStep is how far the camera's altitude moves, as a
	// fraction of the air's height, before the view tables are built
	// again. Until then the frame reads them at the altitude they were
	// built for, so the lookup and the table always agree.
	atmosAltitudeStep = 1.0 / 1024
	// atmosSunStep is how far any component of the direction to the sun
	// moves before the view tables are built again: about a twentieth of
	// the drawn sun's radius. A sun that crosses the sky in a thirty
	// minute day moves that far in about three frames at sixty a second.
	atmosSunStep = 2e-4
)

// Which table a pass of atmoslut.frag.wgsl builds, in the push block.
const (
	atmosBuildTrans   = 0
	atmosBuildSky     = 1
	atmosBuildAerial  = 2
	atmosBuildReflect = 3
)

// atmosPush is the push block of atmoslut.frag.wgsl, and what the
// tables are built from.
type atmosPush struct {
	atmos lin.Vec4 // planet radius, air height, rayleigh and mie falloff heights
	betaR lin.Vec4 // rgb rayleigh scattering per unit at the ground, w the sun's intensity
	betaM lin.Vec4 // x mie scattering, y forward lobe, z camera altitude, w the table to build
	up    lin.Vec4 // xyz away from the ground, w the air (1 - vacuum)
	sun   lin.Vec4 // xyz towards the sun
}

// atmosTables are the atmosphere's lookup tables, which the sky and the
// lit meshes read instead of integrating the scattering per pixel. They
// are made the first time a frame has an atmosphere, and built by
// fullscreen passes before the frame's scene passes: the transmittance
// table when the air's shape changes, and the sky view, the aerial
// perspective and the reflection table when the sun, the camera's
// altitude or the air changes (plan and viewFits). One set of tables
// serves every output, so outputs with different atmospheres in one
// frame each build them again before they draw.
type atmosTables struct {
	pipe                        *render.Pipeline
	trans, sky, aerial, reflect *render.Target
	// transSet names the transmittance table, which the sky view and the
	// reflection table read.
	transSet vk.VkDescriptorSet
	// built is what the tables hold, when haveTrans and haveView say
	// they hold anything.
	built               atmosPush
	haveTrans, haveView bool
	// builds counts the times the view tables were built, for tests.
	builds int
	// want is what the frame block written last needs, and planned says
	// that frame has an atmosphere. build consumes them.
	want    atmosPush
	planned bool
	// push and bound are the block and set being recorded, kept here so
	// recording them does not move a local to the heap.
	push  atmosPush
	bound vk.VkDescriptorSet
}

// plan records what the frame block u needs from the tables and, when
// the view tables built last are close enough, points u at the altitude
// and the sun they were built for, so the frame reads them exactly where
// they were written. The drawn sun and the phase functions keep the
// frame's own sun. writeUniforms calls it for a frame with an
// atmosphere, and buildAtmosphere then brings the tables up to date.
func (t *atmosTables) plan(u *frameUniforms) {
	t.want = atmosPush{
		atmos: u.atmos,
		betaR: u.betaR,
		betaM: lin.V4(u.betaM.X, u.betaM.Y, u.betaM.Z, 0),
		up:    lin.V4(u.skyUp.X, u.skyUp.Y, u.skyUp.Z, u.horizon.W),
		sun:   lin.V4(u.sun.X, u.sun.Y, u.sun.Z, 0),
	}
	t.planned = true
	sun := t.want.sun
	if t.viewFits(t.want) {
		u.betaM.Z = t.built.betaM.Z
		sun = t.built.sun
	}
	radius := u.atmos.X
	r := radius + u.betaM.Z
	first, grazing := atmosHorizon(r, radius, radius+u.atmos.Y)
	u.atmosView = atmosSunSide(u.skyUp.Vec3(), sun.Vec3()).Vec4(r)
	u.atmosLimb = lin.V4(first, grazing, 1/(grazing-first), 0)
}

// atmosSunSide is the horizontal direction towards the sun, which the
// view tables measure azimuth from. It is atmosSunSide in the shaders.
func atmosSunSide(up, sun lin.Vec3) lin.Vec3 {
	side := sun.Sub(up.Mul(sun.Dot(up)))
	if side.Dot(side) < 1e-12 {
		axis := lin.V3(1, 0, 0)
		if abs32(up.X) > 0.9 {
			axis = lin.V3(0, 0, 1)
		}
		side = up.Cross(axis)
	}
	return side.Norm()
}

// atmosHorizon is where the view tables split for a camera at radius r:
// the tangents of half the angle from the zenith of the first ray that
// reaches the air (0 inside it) and of the ray that grazes the ground.
// It is atmosHorizon in the shaders.
func atmosHorizon(r, radius, top float32) (first, grazing float32) {
	r = max(r, radius)
	if r > top {
		first = (r + sqrt32(r*r-top*top)) / top
	}
	return first, (r + sqrt32(max(r*r-radius*radius, 0))) / radius
}

func sqrt32(v float32) float32 { return float32(math.Sqrt(float64(v))) }

// viewFits reports whether the view tables built last serve w.
func (t *atmosTables) viewFits(w atmosPush) bool {
	b := t.built
	if !t.haveTrans || !t.haveView || w.atmos != b.atmos || w.betaR != b.betaR || w.betaM.X != b.betaM.X ||
		w.betaM.Y != b.betaM.Y || w.up != b.up {
		return false
	}
	if abs32(w.betaM.Z-b.betaM.Z) > w.atmos.Y*atmosAltitudeStep {
		return false
	}
	d := w.sun.Sub(b.sun)
	return abs32(d.X) <= atmosSunStep && abs32(d.Y) <= atmosSunStep && abs32(d.Z) <= atmosSunStep
}

// buildAtmosphere records the passes that bring the tables up to date
// with the frame block writeUniforms wrote last, outside any render
// pass. A frame without an atmosphere, or whose tables are current,
// records nothing.
func (g *Graphics) buildAtmosphere(cb vk.VkCommandBuffer) error {
	t := &g.meshes.atmos
	if !t.planned {
		return nil
	}
	t.planned = false
	want := t.want
	if t.viewFits(want) {
		return nil
	}
	if err := t.ensure(g); err != nil {
		return err
	}
	g.timestamps.Begin(cb, "atmosphere")
	if !t.haveTrans || t.built.atmos != want.atmos {
		// The transmittance table depends on the air's shape alone.
		t.pass(cb, t.trans, atmosBuildTrans, g.meshes.black.set, want)
		t.haveTrans, t.haveView, t.built.atmos = true, false, want.atmos
	}
	t.pass(cb, t.sky, atmosBuildSky, t.transSet, want)
	t.pass(cb, t.aerial, atmosBuildAerial, g.meshes.black.set, want)
	t.pass(cb, t.reflect, atmosBuildReflect, t.transSet, want)
	g.timestamps.End(cb)
	t.built, t.haveView = want, true
	t.builds++
	return nil
}

// pass records one fullscreen pass of atmoslut.frag.wgsl into a table.
// set is the transmittance table for the sky view and a stand-in for
// the others, which do not read it.
func (t *atmosTables) pass(cb vk.VkCommandBuffer, target *render.Target, table int, set vk.VkDescriptorSet, want atmosPush) {
	t.push = want
	t.push.betaM.W = float32(table)
	render.BeginTargetPass(cb, render.PassDesc{Target: target})
	vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, t.pipe.Handle)
	t.bound = set
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, t.pipe.Layout, 0, 1, &t.bound, 0, nil)
	vk.CmdPushConstants(cb, t.pipe.Layout, meshStages, 0, uint32(unsafe.Sizeof(t.push)), unsafe.Pointer(&t.push))
	vk.CmdDraw(cb, 3, 1, 0, 0)
	render.EndTargetPass(cb, target)
}

// ensure makes the tables, their pipeline and their sets the first time
// a frame has an atmosphere, and points the mesh pass's set 2 at them.
func (t *atmosTables) ensure(g *Graphics) error {
	if t.pipe != nil {
		return nil
	}
	dev := g.r.Device
	table := func(w, h int) (*render.Target, error) {
		// Tests read the tables back, which needs them to be copied from.
		return dev.NewTargetDesc(render.TargetDesc{
			Extent:      vk.VkExtent2D{Width: uint32(w), Height: uint32(h)},
			ColorFormat: atmosFormat, ColorUsage: vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
		})
	}
	fail := func(err error) error {
		t.destroy(g)
		return err
	}
	var err error
	if t.trans, err = table(atmosTransW, atmosTransH); err != nil {
		return fail(err)
	}
	if t.sky, err = table(atmosSkyW, 2*atmosSkyH); err != nil {
		return fail(err)
	}
	if t.aerial, err = table(atmosAerialW*atmosAerialD, 2*atmosAerialH); err != nil {
		return fail(err)
	}
	if t.reflect, err = table(atmosReflectN, atmosReflectN); err != nil {
		return fail(err)
	}
	if t.transSet, err = g.descriptors.Allocate(t.trans.Color.View, g.linear); err != nil {
		return fail(err)
	}
	mp := &g.meshes
	set, err := mp.shadowDesc.AllocateMany(mp.atlasBindings(t.trans.Color.View, t.sky.Color.View, t.aerial.Color.View, t.reflect.Color.View))
	if err != nil {
		return fail(err)
	}
	pipe, err := dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.AtmosLUTFrag, ColorFormat: atmosFormat,
		PushConstantSize: uint32(unsafe.Sizeof(atmosPush{})),
		SetLayouts:       []vk.VkDescriptorSetLayout{g.descriptors.Layout},
	})
	if err != nil {
		mp.shadowDesc.Free(set)
		return fail(err)
	}
	t.pipe = pipe
	// Frames in flight keep the set they bound, which names the stand-ins,
	// so the tables go into a new set and the old one retires with them.
	old := mp.shadowSet
	mp.shadowSet = set
	g.deferDestroy(func() { mp.shadowDesc.Free(old) })
	return nil
}

// destroy frees the tables and their pipeline. The sets go with their
// pools.
func (t *atmosTables) destroy(g *Graphics) {
	if t.pipe != nil {
		t.pipe.Destroy()
		t.pipe = nil
	}
	if t.transSet != 0 {
		g.descriptors.Free(t.transSet)
		t.transSet = 0
	}
	for _, tt := range []**render.Target{&t.trans, &t.sky, &t.aerial, &t.reflect} {
		if *tt != nil {
			(*tt).Destroy()
			*tt = nil
		}
	}
	t.haveTrans, t.haveView, t.planned = false, false, false
}
