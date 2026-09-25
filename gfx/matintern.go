package gfx

import (
	"hash/maphash"
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// frameMaterial is one distinct material a queue's draws use in a frame.
// A frame's draws name their material by its index in the queue's table,
// so the material is copied once per frame rather than once per draw, and
// what depends on it alone (its descriptor sets, its half of the instance
// record, its pipeline key) is worked out once per frame rather than once
// per draw.
type frameMaterial struct {
	mat    Material // with the zero-value defaults filled in
	shader *Shader  // mat.Shader, or the default shader; never nil
	hash   uint64
	// blended, stencil and transmissive are mat.blended(),
	// mat.marksStencil() and mat.Transmission > 0; cutout is whether the
	// shadow pass reads the albedo's alpha.
	blended, stencil, transmissive, cutout bool
	// key and shellKey are the lit pass's pipeline key for the material's
	// own draws and for its fur shells, for a static mesh and the screen's
	// output; the caller fills in skinned and the output.
	key, shellKey pipeKey
	// gen is the preparation that last resolved sets and inst. A queue is
	// prepared once per frame and once per probe face, and each time the
	// scene copy the sets bind may differ, so they are resolved again.
	gen  uint32
	sets [maxProbes + 1]vk.VkDescriptorSet // by the draw's probe index; zero until resolved
	// inst is the material's part of the instance record: everything but
	// the model rows, the joint base, the probe, the opaque flag, the
	// shell and the morph block, which each draw writes over it.
	inst    meshInstance
	hasInst bool
}

// materialSeed seeds the material hash. Tables are rebuilt every frame,
// so the seed only has to be the same within a process.
var materialSeed = maphash.MakeSeed()

// materialBytes views a material as its bytes, for hashing and comparing.
// Two materials with the same bytes draw the same; two that are equal by
// == but differ in a byte, a negative zero or a NaN, are kept apart,
// which costs a table entry and nothing else.
func materialBytes(m *Material) string {
	return unsafe.String((*byte)(unsafe.Pointer(m)), unsafe.Sizeof(*m))
}

// defaultedMaterial returns m with the zero-value defaults filled in: a
// white base colour and a roughness of 0.6. It returns m itself when
// neither applies and fills tmp otherwise.
func defaultedMaterial(m, tmp *Material) *Material {
	if m.BaseColor != (Color{}) && m.Roughness != 0 {
		return m
	}
	*tmp = *m
	if tmp.BaseColor == (Color{}) {
		tmp.BaseColor = White
	}
	if tmp.Roughness == 0 {
		tmp.Roughness = 0.6
	}
	return tmp
}

// findMaterial returns the index of a material already in the queue's
// table, comparing the last one found first, or -1 with the material's
// hash for addMaterial.
func (q *drawQueue) findMaterial(m *Material) (int32, uint64) {
	key := materialBytes(m)
	if last := q.lastMat - 1; last >= 0 && materialBytes(&q.mats[last].mat) == key {
		return last, 0
	}
	h := maphash.String(materialSeed, key)
	if len(q.matSlots) == 0 {
		return -1, h
	}
	mask := uint64(len(q.matSlots) - 1)
	for i := h & mask; ; i = (i + 1) & mask {
		s := q.matSlots[i]
		if s == 0 {
			return -1, h
		}
		fm := &q.mats[s-1]
		if fm.hash == h && materialBytes(&fm.mat) == key {
			q.lastMat = s
			return s - 1, h
		}
	}
}

// addMaterial appends a material the table does not hold, whose hash
// findMaterial returned, and returns its index. The caller has checked
// that the material's resources belong to this Graphics.
func (q *drawQueue) addMaterial(m *Material, shader *Shader, h uint64) int32 {
	if (len(q.mats)+1)*2 > len(q.matSlots) {
		q.growMaterialSlots()
	}
	at := int32(len(q.mats))
	q.mats = append(q.mats, frameMaterial{mat: *m, shader: shader, hash: h})
	fm := &q.mats[at]
	fm.blended = m.blended()
	fm.stencil = m.marksStencil()
	fm.transmissive = m.Transmission > 0
	fm.cutout = m.AlphaCutoff > 0
	fm.key = meshKey(m, false, false, outKey{})
	fm.shellKey = meshKey(m, false, true, outKey{})
	q.insertMaterialSlot(at)
	q.lastMat = at + 1
	return at
}

// insertMaterialSlot puts a table entry's index in the hash slots.
func (q *drawQueue) insertMaterialSlot(at int32) {
	mask := uint64(len(q.matSlots) - 1)
	for i := q.mats[at].hash & mask; ; i = (i + 1) & mask {
		if q.matSlots[i] == 0 {
			q.matSlots[i] = at + 1
			return
		}
	}
}

// growMaterialSlots doubles the hash slots and puts every entry back.
func (q *drawQueue) growMaterialSlots() {
	q.matSlots = make([]int32, max(2*len(q.matSlots), 64))
	for i := range q.mats {
		q.insertMaterialSlot(int32(i))
	}
}

// resetMaterials empties the material table for a new frame.
func (q *drawQueue) resetMaterials() {
	q.mats = q.mats[:0]
	clear(q.matSlots)
	q.lastMat = 0
}

// internMaterial returns the index of a draw's material in the current
// queue's table, adding it the first time a frame uses it. A material is
// checked for resources from another Graphics and for a sprite shader
// when it is added, so a draw call panics exactly as it did when every
// draw was checked: a material that fails is never added, and one that
// was added has the same textures and shader as the draw.
func (g *Graphics) internMaterial(q *drawQueue, m *Material) uint32 {
	var tmp Material
	m = defaultedMaterial(m, &tmp)
	at, h := q.findMaterial(m)
	if at >= 0 {
		return uint32(at)
	}
	if err := g.materialOwnerError(m); err != nil {
		panic(err)
	}
	shader := m.Shader
	if shader == nil {
		shader = g.meshes.defaultShader
	} else if !shader.mesh {
		panic("gfx: Material.Shader wants a mesh shader from NewMeshShader")
	}
	return uint32(q.addMaterial(m, shader, h))
}

// resolveMaterial returns a draw's material set for its probe and makes
// sure the material's instance template is built, once per material and
// probe in each preparation.
func (g *Graphics) resolveMaterial(q *drawQueue, fm *frameMaterial, probe int32, env *Environment, scene *render.Image) (vk.VkDescriptorSet, error) {
	if fm.gen != q.prepGen {
		fm.gen = q.prepGen
		fm.sets = [maxProbes + 1]vk.VkDescriptorSet{}
	}
	if set := fm.sets[probe]; set != 0 {
		return set, nil
	}
	set, samplers, err := g.materialSet(&fm.mat, q.probeEnv(int(probe), env), scene)
	if err != nil {
		return 0, err
	}
	fm.sets[probe] = set
	if !fm.hasInst {
		fm.buildInstance(samplers)
	}
	return set, nil
}

// buildInstance fills the material's part of the instance record.
func (fm *frameMaterial) buildInstance(samplers float32) {
	m := &fm.mat
	flags := boolFloat(m.NormalTexture != nil) + 2*boolFloat(m.Unlit) + 4*boolFloat(m.OcclusionUV2) + 8*boolFloat(m.EmissiveTexture == nil)
	occlusion := float32(0)
	if m.OcclusionTexture != nil {
		occlusion = orOne(m.OcclusionStrength, true)
	}
	uv := m.UVTransform
	if uv == (lin.Affine{}) {
		uv = lin.Identity2()
	}
	ccRough := m.ClearcoatRoughness
	if m.Clearcoat > 0 && ccRough == 0 {
		ccRough = 0.03
	}
	sheenRough := m.SheenRoughness
	if m.Sheen != (Color{}) && sheenRough == 0 {
		sheenRough = 0.5
	}
	ior := m.IOR
	if ior == 0 {
		ior = 1.5
	}
	atten := m.AttenuationColor
	if atten == (Color{}) {
		atten = White
	}
	specColor := m.SpecularColor
	if specColor == (Color{}) {
		specColor = White
	}
	specular := m.Specular
	if specular == 0 {
		specular = 1
	}
	filmIOR := m.IridescenceIOR
	if filmIOR == 0 {
		filmIOR = 1.3
	}
	filmThick := m.IridescenceThickness
	if filmThick == 0 {
		filmThick = 400
	}
	fm.inst = meshInstance{
		baseColor: [4]float32{m.BaseColor.R, m.BaseColor.G, m.BaseColor.B, m.BaseColor.A},
		material:  [4]float32{orOne(m.Metallic, m.MetalRoughTexture != nil), m.Roughness, m.Emissive, flags},
		extra:     [4]float32{0, m.AlphaCutoff, occlusion, m.Subsurface},
		uvT0:      [4]float32{uv.A, uv.B, uv.C, uv.D},
		uvT1:      [4]float32{uv.E, uv.F, m.Clearcoat, ccRough},
		sheen:     [4]float32{m.Sheen.R, m.Sheen.G, m.Sheen.B, sheenRough},
		volume:    [4]float32{m.Transmission, ior, m.Thickness, m.AttenuationDistance},
		atten:     [4]float32{atten.R, atten.G, atten.B, samplers},
		spec:      [4]float32{specColor.R, specColor.G, specColor.B, specular},
		irid:      [4]float32{m.Iridescence, filmIOR, m.IridescenceThicknessMin, filmThick},
		fur:       [4]float32{m.Anisotropy, m.AnisotropyRotation, 0, 0},
	}
	fm.hasInst = true
}

// writeInstance fills one draw's instance record: the material's template
// with the draw's own fields over it.
func (q *drawQueue) writeInstance(in *meshInstance, d *meshDraw, fm *frameMaterial) {
	*in = fm.inst
	mm := &d.model
	in.model = [3]lin.Vec4{
		lin.V4(mm.At(0, 0), mm.At(0, 1), mm.At(0, 2), mm.At(0, 3)),
		lin.V4(mm.At(1, 0), mm.At(1, 1), mm.At(1, 2), mm.At(1, 3)),
		lin.V4(mm.At(2, 0), mm.At(2, 1), mm.At(2, 2), mm.At(2, 3)),
	}
	if d.prev >= 0 {
		pm := &q.prevs[d.prev]
		in.prevModel = [3]lin.Vec4{
			lin.V4(pm.At(0, 0), pm.At(0, 1), pm.At(0, 2), pm.At(0, 3)),
			lin.V4(pm.At(1, 0), pm.At(1, 1), pm.At(1, 2), pm.At(1, 3)),
			lin.V4(pm.At(2, 0), pm.At(2, 1), pm.At(2, 2), pm.At(2, 3)),
		}
	} else {
		in.prevModel = in.model
	}
	in.extra[0] = float32(d.jointBase)
	in.gi = [4]float32{float32(d.probe), boolFloat(!d.blended), 0, 0}
	in.fur[2], in.fur[3] = d.shell*shellLength(&fm.mat), d.shell
	if d.morph >= 0 {
		q.morphs[d.morph].instance(in)
	}
}
