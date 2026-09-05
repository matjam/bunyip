package gfx

import (
	"fmt"
	"image"
	"image/color"
	"slices"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Model is a glTF document uploaded to the GPU: one Mesh per primitive,
// one Texture per image, and the placements to draw.
type Model struct {
	Parts    []ModelPart
	Min, Max lin.Vec3
	meshes   []*Mesh
	textures []*Texture
	nodes    []gltf.Node
	skins    []gltf.Skin
	clips    []gltf.Animation
	order    []int // node indices, parents before children
	morphs   []*morphMesh
	// morphOf finds a part's morph targets when it is drawn, so a draw
	// carries the weights the vertex shader blends with.
	morphOf map[*Mesh]*morphMesh
	// morphs' deltas and the descriptor set the draws bind for them.
	morphBuf *morphStore
	g        *Graphics
}

// morphMesh is a primitive with morph targets. Its rest geometry stays
// on the CPU, and its deltas go into the model's morph buffer as well, so
// a pose of up to MaxGPUMorphTargets open targets blends in the vertex
// shader and one with more blends here and is uploaded.
type morphMesh struct {
	mesh    *Mesh
	node    int
	names   []string
	targets []gltf.MorphTarget
	base    []Vertex     // rest vertices of a plain mesh
	skin    []SkinVertex // rest vertices of a skinned mesh
	indices []uint32
	rest    []float32 // the file's default weights
	weights []float32 // the weights the uploaded geometry was blended with
	current []float32 // the weights the mesh is drawn with, whichever path
	// gpuBase is where the mesh's deltas start in the model's morph
	// buffer, or -1 when they are not there at all.
	gpuBase int
	// active is the targets the next draw blends in the shader, empty
	// when the pose went through the processor instead, and blended says
	// the uploaded geometry is such a blend rather than the rest pose.
	active  []morphWeight
	blended bool
	// drawn retains the current geometry view. Later uploads replace the
	// mesh's buffers, while queued draws keep this view until retirement.
	drawn *Mesh
}

// apply blends the targets by the weights and uploads the result when
// the weights differ from the last upload.
func (mm *morphMesh) apply(weights []float32) error {
	if len(weights) == len(mm.weights) && slices.Equal(weights, mm.weights) {
		return nil
	}
	mm.weights = append(mm.weights[:0], weights...)
	blend := func(i int, pos, normal lin.Vec3) (lin.Vec3, lin.Vec3) {
		for ti, w := range weights {
			if ti >= len(mm.targets) || w == 0 {
				continue
			}
			t := mm.targets[ti]
			pos = pos.Add(t.Positions[i].Mul(w))
			if t.Normals != nil {
				normal = normal.Add(t.Normals[i].Mul(w))
			}
		}
		return pos, normal.Norm()
	}
	if mm.skin != nil {
		verts := make([]SkinVertex, len(mm.skin))
		for i, v := range mm.skin {
			v.Pos, v.Normal = blend(i, v.Pos, v.Normal)
			verts[i] = v
		}
		return mm.mesh.UpdateSkinned(verts, mm.indices)
	}
	verts := make([]Vertex, len(mm.base))
	for i, v := range mm.base {
		v.Pos, v.Normal = blend(i, v.Pos, v.Normal)
		verts[i] = v
	}
	return mm.mesh.Update(verts, mm.indices)
}

// NodeCount is the number of nodes in the model's hierarchy.
func (m *Model) NodeCount() int { return len(m.nodes) }

// NodeName returns a node's name; an unknown index gives "".
func (m *Model) NodeName(node int) string {
	if node < 0 || node >= len(m.nodes) {
		return ""
	}
	return m.nodes[node].Name
}

// NodeIndex returns the index of the first node with the name, or -1.
func (m *Model) NodeIndex(name string) int {
	for i, n := range m.nodes {
		if n.Name == name {
			return i
		}
	}
	return -1
}

// NodeParent returns a node's parent index, or -1 for a root.
func (m *Model) NodeParent(node int) int {
	if node < 0 || node >= len(m.nodes) {
		return -1
	}
	return m.nodes[node].Parent
}

// NodeMatrix returns a node's rest-pose world matrix in model space, for
// a socket on a model that is not animated: a lamp's bulb, a turret's
// muzzle. An animated model's current pose comes from AnimPlayer.NodeMatrix.
// An unknown index gives the identity.
func (m *Model) NodeMatrix(node int) lin.Mat4 {
	if node < 0 || node >= len(m.nodes) {
		return lin.Identity()
	}
	mat := m.nodes[node].Local()
	for p := m.nodes[node].Parent; p >= 0 && p < len(m.nodes); p = m.nodes[p].Parent {
		mat = m.nodes[p].Local().Mul(mat)
	}
	return mat
}

// NodePosition returns a node's rest-pose position in model space.
func (m *Model) NodePosition(node int) lin.Vec3 {
	mat := m.NodeMatrix(node)
	return lin.V3(mat[12], mat[13], mat[14])
}

// AnimMask is the set of nodes an animation layer affects, one flag per
// node; nil means every node. Build one with MaskNodes or MaskSubtree.
type AnimMask []bool

// MaskNodes makes a mask of exactly the named nodes; unknown names are
// ignored.
func (m *Model) MaskNodes(names ...string) AnimMask {
	mask := make(AnimMask, len(m.nodes))
	for _, name := range names {
		if i := m.NodeIndex(name); i >= 0 {
			mask[i] = true
		}
	}
	return mask
}

// MaskSubtree makes a mask of the named nodes and everything under them:
// "Spine1" for the upper body, "Head" for the head and its children.
func (m *Model) MaskSubtree(names ...string) AnimMask {
	mask := make(AnimMask, len(m.nodes))
	for _, name := range names {
		if i := m.NodeIndex(name); i >= 0 {
			m.markSubtree(mask, i, 0)
		}
	}
	return mask
}

func (m *Model) markSubtree(mask AnimMask, node, depth int) {
	if node < 0 || node >= len(m.nodes) || depth > 64 {
		return
	}
	mask[node] = true
	for _, c := range m.nodes[node].Children {
		m.markSubtree(mask, c, depth+1)
	}
}

// MorphTargets names the morph targets of the node's mesh, blank where
// the file names none; nil when the node has no morph targets.
func (m *Model) MorphTargets(node int) []string {
	for _, mm := range m.morphs {
		if mm.node == node {
			return mm.names
		}
	}
	return nil
}

// MorphWeights returns the morph target weights the node's mesh is drawn
// with, whether they blend in the vertex shader or on the processor; nil
// when the node has no morph targets. The slice is the model's own.
func (m *Model) MorphWeights(node int) []float32 {
	for _, mm := range m.morphs {
		if mm.node == node {
			return mm.current
		}
	}
	return nil
}

// SetMorphWeights blends the node's morph targets by the weights (one per
// target, 0 for none and 1 for the full shape) and uploads the result:
// a facial expression, a wind-bent plant. A player's weights channels do
// the same through DrawModelAnimated. Up to MaxGPUMorphTargets open at
// once blend in the vertex shader and cost nothing to change; past that
// the blend runs here, one pass over the mesh's vertices per open target
// plus an upload, each time the weights change.
// DrawModel captures the current weights and geometry, so later changes
// do not affect instances already queued.
func (m *Model) SetMorphWeights(node int, weights []float32) error {
	found := false
	for _, mm := range m.morphs {
		if mm.node != node {
			continue
		}
		found = true
		if err := mm.set(weights); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("gfx: node %d has no morph targets", node)
	}
	return nil
}

// ModelPart is one primitive placed by one node.
type ModelPart struct {
	Mesh *Mesh
	// Name is the name glTF gave the primitive's material, empty when the
	// file names none. Match on it to override one part's material.
	Name     string
	Material Material
	World    lin.Mat4
	node     int
	skin     int
}

// MaterialOverride returns the material to draw one part of a model
// with. i is the part's index in Model.Parts and part is the part
// itself, so a game can decide by index or by part.Name, the name glTF
// gave the material. Returning part.Material draws what the file asked
// for.
type MaterialOverride func(i int, part ModelPart) Material

// apply returns the material a part is drawn with under an override; a
// nil override is the file's own material.
func (o MaterialOverride) apply(i int, part ModelPart) Material {
	if o == nil {
		return part.Material
	}
	return o(i, part)
}

// LoadModel uploads a parsed glTF document.
func (g *Graphics) LoadModel(doc *gltf.Document) (*Model, error) {
	m := &Model{nodes: doc.Nodes, skins: doc.Skins, clips: doc.Animations, g: g,
		morphOf: map[*Mesh]*morphMesh{}, morphBuf: &morphStore{g: g}}
	m.Min, m.Max = doc.Bounds()
	m.order = topoOrder(doc.Nodes)
	// glTF keeps thickness in a texture's green channel; the material
	// reads red, so those images are swizzled once here.
	thickness := map[int]bool{}
	for _, mat := range doc.Materials {
		if mat.ThicknessImage >= 0 {
			thickness[mat.ThicknessImage] = true
		}
	}
	for i, img := range doc.Images {
		if thickness[i] {
			img = greenToRed(img)
		}
		tex, err := g.NewTexture(img, TextureOptions{Linear: true, Data: doc.IsDataImage(i)})
		if err != nil {
			m.Destroy()
			return nil, err
		}
		m.textures = append(m.textures, tex)
	}
	deriv := &derived{g: g, m: m, doc: doc, cache: map[derivedKey]*Texture{}}
	type key struct{ mesh, prim int }
	uploaded := map[key]*Mesh{}
	for _, inst := range doc.Instances {
		src := doc.Meshes[inst.Mesh]
		for pi, p := range src.Primitives {
			k := key{inst.Mesh, pi}
			mesh, ok := uploaded[k]
			// A primitive with morph targets is uploaded once per instance,
			// since each instance blends its own copy.
			if !ok || len(p.Targets) > 0 {
				var err error
				vertex := func(i int) Vertex {
					v := Vertex{Pos: p.Positions[i], Normal: p.Normals[i], UV: p.UVs[i]}
					if p.UVs2 != nil {
						v.UV2 = p.UVs2[i]
					}
					if p.Colors != nil {
						c := p.Colors[i]
						v.Color = Color{c.X, c.Y, c.Z, c.W}
					}
					return v
				}
				var mm *morphMesh
				if len(p.Targets) > 0 {
					mm = &morphMesh{node: inst.Node, targets: p.Targets, indices: p.Indices, names: make([]string, len(p.Targets)), gpuBase: -1}
					copy(mm.names, src.TargetNames)
				}
				if p.Skinned() && inst.Skin >= 0 {
					verts := make([]SkinVertex, len(p.Positions))
					for i := range verts {
						v := vertex(i)
						verts[i] = SkinVertex{Pos: v.Pos, Normal: v.Normal, UV: v.UV, UV2: v.UV2, Color: v.Color, Joints: p.Joints[i], Weights: p.Weights[i]}
					}
					mesh, err = g.NewSkinnedMesh(verts, p.Indices)
					if mm != nil {
						mm.skin = verts
					}
				} else {
					verts := make([]Vertex, len(p.Positions))
					for i := range verts {
						verts[i] = vertex(i)
					}
					mesh, err = g.NewMesh(verts, p.Indices)
					if mm != nil {
						mm.base = verts
					}
				}
				if err != nil {
					m.Destroy()
					return nil, err
				}
				uploaded[k] = mesh
				m.meshes = append(m.meshes, mesh)
				if mm != nil {
					mm.mesh = mesh
					m.morphs = append(m.morphs, mm)
					m.morphOf[mesh] = mm
					// The deltas go on the device as well as staying here,
					// so a pose of few open targets blends in the shader and
					// one of many still blends here. A file with more targets
					// than a byte can name keeps the processor's path alone.
					if len(mm.targets) <= 256 {
						mm.gpuBase = m.morphBuf.reserve(mm)
					}
					// The upload is the rest geometry: every weight zero.
					mm.rest = m.restWeights(inst.Node, src)
					mm.weights = make([]float32, len(mm.rest))
					if err := mm.set(mm.rest); err != nil {
						m.Destroy()
						return nil, err
					}
				}
			}
			mat := Material{BaseColor: White, Roughness: 0.6}
			name := ""
			if p.Material >= 0 {
				src := doc.Materials[p.Material]
				name = src.Name
				mat.BaseColor = Color{src.BaseColor[0], src.BaseColor[1], src.BaseColor[2], src.BaseColor[3]}
				mat.Metallic, mat.Roughness = src.Metallic, max(src.Roughness, 0.04)
				mat.Texture = m.texture(src.Image)
				mat.MetalRoughTexture = m.texture(src.MetalRoughImage)
				mat.NormalTexture = m.texture(src.NormalImage)
				mat.EmissiveTexture = m.texture(src.EmissiveImage)
				mat.Emissive = max(src.Emissive[0], src.Emissive[1], src.Emissive[2])
				mat.OcclusionTexture = m.texture(src.OcclusionImage)
				mat.OcclusionStrength = src.OcclusionStrength
				mat.OcclusionUV2 = src.OcclusionUV2
				mat.DoubleSided = src.DoubleSided
				mat.Unlit = src.Unlit
				mat.Clearcoat, mat.ClearcoatRoughness = src.Clearcoat, src.ClearcoatRoughness
				mat.Sheen = Color{src.SheenColor[0], src.SheenColor[1], src.SheenColor[2], 1}
				if src.SheenColor == [3]float32{} {
					mat.Sheen = Color{}
				}
				mat.SheenRoughness = src.SheenRoughness
				if src.Transmission > 0 {
					mat.Transmission, mat.IOR, mat.Thickness = src.Transmission, src.IOR, src.Thickness
					mat.AttenuationDistance = src.AttenuationDistance
					mat.AttenuationColor = Color{src.AttenuationColor[0], src.AttenuationColor[1], src.AttenuationColor[2], 1}
					mat.TransmissionTexture = m.texture(src.TransmissionImage)
					mat.ThicknessTexture = m.texture(src.ThicknessImage)
				}
				// KHR_materials_specular: the strength and the tint, whose
				// two images become one.
				mat.Specular = src.SpecularFactor
				if mat.Specular == 0 {
					mat.Specular = 1e-4 // a zero factor means none, not the default
				}
				mat.SpecularColor = Color{src.SpecularColor[0], src.SpecularColor[1], src.SpecularColor[2], 1}
				specTex, err := deriv.get("specular", src.SpecularColorImage, src.SpecularImage, mergeSpecular)
				if err != nil {
					m.Destroy()
					return nil, err
				}
				mat.SpecularTexture = specTex
				// KHR_materials_iridescence, likewise two images in one.
				mat.Iridescence = src.IridescenceFactor
				mat.IridescenceIOR = src.IridescenceIOR
				mat.IridescenceThickness = src.IridescenceThicknessMax
				mat.IridescenceThicknessMin = src.IridescenceThicknessMin
				iridTex, err := deriv.get("iridescence", src.IridescenceImage, src.IridescenceThicknessImage, mergeIridescence)
				if err != nil {
					m.Destroy()
					return nil, err
				}
				mat.IridescenceTexture = iridTex
				// KHR_materials_anisotropy.
				mat.Anisotropy, mat.AnisotropyRotation = src.AnisotropyStrength, src.AnisotropyRotation
				mat.AnisotropyTexture = m.texture(src.AnisotropyImage)
				// The specular-glossiness workflow arrives converted, apart
				// from its image, which becomes a metallic-roughness map.
				if src.SpecGloss && src.SpecGlossImage >= 0 {
					tex, err := deriv.get("specgloss", src.SpecGlossImage, -1, func(a, _ image.Image) image.Image {
						return specGlossMetalRough(a, src.Glossiness, src.Metallic)
					})
					if err != nil {
						m.Destroy()
						return nil, err
					}
					mat.MetalRoughTexture, mat.Metallic, mat.Roughness = tex, 1, 1
				}
				if src.UVOffset != [2]float32{} || src.UVRotation != 0 || src.UVScale != [2]float32{1, 1} {
					// glTF: uv' = T · R · S · uv.
					mat.UVTransform = lin.Translate2(src.UVOffset[0], src.UVOffset[1]).Mul(lin.Rotate2(-src.UVRotation)).Mul(lin.Scale2(src.UVScale[0], src.UVScale[1]))
				}
				switch src.AlphaMode {
				case gltf.AlphaMask:
					mat.AlphaCutoff = src.AlphaCutoff
				case gltf.AlphaBlend:
					mat.Blend = true
				}
			}
			m.Parts = append(m.Parts, ModelPart{Mesh: mesh, Name: name, Material: mat, World: inst.World, node: inst.Node, skin: inst.Skin})
		}
	}
	// Every morph mesh's deltas are on the device from here, so a pose of
	// few open targets blends in the vertex shader instead of here.
	if err := m.morphBuf.upload(); err != nil {
		m.Destroy()
		return nil, err
	}
	g.track(m, Resource{Kind: ResourceModel, Parts: len(m.Parts)})
	return m, nil
}

// restWeights returns the morph weights a node starts with: its own,
// else its mesh's defaults, else zeros, one per target.
func (m *Model) restWeights(node int, mesh gltf.Mesh) []float32 {
	w := make([]float32, mesh.TargetCount())
	if node >= 0 && node < len(m.nodes) && len(m.nodes[node].Weights) > 0 {
		copy(w, m.nodes[node].Weights)
	} else {
		copy(w, mesh.Weights)
	}
	return w
}

// derived builds the textures a model needs that are not images of the
// file: glTF keeps a material's specular tint and strength, and a thin
// film's strength and thickness, in two images each, where the renderer
// reads one. Results are cached by their sources, so materials sharing
// images share one upload, and the model owns them.
type derived struct {
	g     *Graphics
	m     *Model
	doc   *gltf.Document
	cache map[derivedKey]*Texture
}

type derivedKey struct {
	kind string
	a, b int
}

// image returns one of the document's images, or nil.
func (d *derived) image(i int) image.Image {
	if i < 0 || i >= len(d.doc.Images) {
		return nil
	}
	return d.doc.Images[i]
}

// get builds the texture for a pair of source images, or returns the one
// built earlier. Both sources may be -1, in which case the caller has
// asked for nothing and the result is nil.
func (d *derived) get(kind string, a, b int, build func(a, b image.Image) image.Image) (*Texture, error) {
	if a < 0 && b < 0 {
		return nil, nil
	}
	key := derivedKey{kind, a, b}
	if tex, ok := d.cache[key]; ok {
		return tex, nil
	}
	// The combined images are read as data: their channels are already
	// linear by the time they are written.
	tex, err := d.g.NewTexture(build(d.image(a), d.image(b)), TextureOptions{Linear: true, Data: true})
	if err != nil {
		return nil, err
	}
	d.cache[key] = tex
	d.m.textures = append(d.m.textures, tex)
	return tex, nil
}

// combineSize is the size a combined image is built at: the larger of
// its two sources in each direction, or one pixel when neither is given.
func combineSize(a, b image.Image) (w, h int) {
	w, h = 1, 1
	for _, img := range []image.Image{a, b} {
		if img != nil {
			w = max(w, img.Bounds().Dx())
			h = max(h, img.Bounds().Dy())
		}
	}
	return w, h
}

// texelAt reads one texel of a source scaled to a w by h grid, white
// where the source is missing.
func texelAt(src image.Image, x, y, w, h int) color.RGBA {
	if src == nil {
		return color.RGBA{255, 255, 255, 255}
	}
	b := src.Bounds()
	r, g, bl, a := src.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)}
}

// mergeSpecular builds the specular map from glTF's two: the colour
// map's RGB, converted to linear because the slot is read as data, and
// the strength map's alpha.
func mergeSpecular(colorMap, strength image.Image) image.Image {
	w, h := combineSize(colorMap, strength)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := texelAt(colorMap, x, y, w, h)
			s := texelAt(strength, x, y, w, h)
			lin := func(v uint8) uint8 { return uint8(lin32(srgbToLinear(v))*255 + 0.5) }
			out.SetRGBA(x, y, color.RGBA{lin(c.R), lin(c.G), lin(c.B), s.A})
		}
	}
	return out
}

// mergeIridescence builds the iridescence map: the strength map's red
// channel and the thickness map's green, which is where glTF keeps each.
func mergeIridescence(strength, thickness image.Image) image.Image {
	w, h := combineSize(strength, thickness)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			out.SetRGBA(x, y, color.RGBA{texelAt(strength, x, y, w, h).R, texelAt(thickness, x, y, w, h).G, 0, 255})
		}
	}
	return out
}

// specGlossMetalRough turns a specular-glossiness image into the
// metallic-roughness map the renderer reads: the glossiness in its
// alpha, scaled by the material's factor, becomes roughness in green,
// and the converted metallic factor fills blue.
func specGlossMetalRough(src image.Image, gloss, metallic float32) image.Image {
	w, h := combineSize(src, nil)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	blue := uint8(lin.Clamp(metallic, 0, 1)*255 + 0.5)
	for y := range h {
		for x := range w {
			g := float32(texelAt(src, x, y, w, h).A) / 255 * gloss
			out.SetRGBA(x, y, color.RGBA{0, uint8(lin.Clamp(1-g, 0, 1)*255 + 0.5), blue, 255})
		}
	}
	return out
}

// lin32 keeps a linear value inside the unit range.
func lin32(v float32) float32 { return lin.Clamp(v, 0, 1) }

// greenToRed copies an image with its green channel in red, for glTF
// thickness maps, which store thickness in green.
func greenToRed(src image.Image) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, g, _, _ := src.At(x, y).RGBA()
			v := uint8(g >> 8)
			out.SetRGBA(x-b.Min.X, y-b.Min.Y, color.RGBA{v, v, v, 255})
		}
	}
	return out
}

func (m *Model) texture(i int) *Texture {
	if i < 0 || i >= len(m.textures) {
		return nil
	}
	return m.textures[i]
}

// topoOrder lists nodes so that every parent precedes its children.
func topoOrder(nodes []gltf.Node) []int {
	order := make([]int, 0, len(nodes))
	var visit func(int, int)
	visit = func(i, depth int) {
		if i < 0 || i >= len(nodes) || depth > 64 {
			return
		}
		order = append(order, i)
		for _, c := range nodes[i].Children {
			visit(c, depth+1)
		}
	}
	for i, n := range nodes {
		if n.Parent < 0 {
			visit(i, 0)
		}
	}
	return order
}

// DrawModel queues every part of the model under a world transform,
// each with the material its file gave it and its current morph pose.
func (g *Graphics) DrawModel(m *Model, world lin.Mat4) {
	g.DrawModelWith(m, world, nil)
}

// DrawModelWith queues every part of the model under a world transform,
// passing each part through override to decide the material it is drawn
// with: one material for the whole model, a different one for a named
// part, or the file's own material with a field changed. A nil override
// is DrawModel.
//
//	gr.DrawModelWith(ship, world, func(i int, p gfx.ModelPart) gfx.Material {
//		if p.Name == "hull" {
//			m := p.Material
//			m.BaseColor = team
//			return m
//		}
//		return p.Material
//	})
func (g *Graphics) DrawModelWith(m *Model, world lin.Mat4, override MaterialOverride) {
	g.DrawModelMoved(m, world, world, override)
}

// DrawModelMoved is DrawModelWith for a model that moved: prev is the
// world transform it was drawn with last frame, which the velocity
// buffer carries for temporal anti-aliasing and motion blur.
func (g *Graphics) DrawModelMoved(m *Model, world, prev lin.Mat4, override MaterialOverride) {
	for i, p := range m.Parts {
		at, was := world.Mul(p.World), prev.Mul(p.World)
		d := meshDraw{mesh: p.Mesh, mat: override.apply(i, p), model: at, prev: was, moved: was != at,
			morphSet: m.morphSet()}
		m.morphOf[p.Mesh].snapshot(&d)
		g.queueMesh(d)
	}
}

// morphSet is the descriptor set a draw of this model binds for its
// morph target deltas, zero when the model has none, which leaves the
// draw with the empty set every other draw binds.
func (m *Model) morphSet() vk.VkDescriptorSet {
	if m == nil || m.morphBuf == nil {
		return 0
	}
	return m.morphBuf.set
}

// Destroy frees the model's meshes, textures and morph targets after
// queued and submitted draws have finished using them.
func (m *Model) Destroy() {
	if m.g != nil {
		m.g.forget(m)
		m.g.owned.remove(m)
	}
	for _, mesh := range m.meshes {
		mesh.Destroy()
	}
	for _, t := range m.textures {
		t.Destroy()
	}
	m.morphBuf.destroy()
	m.meshes, m.textures, m.Parts, m.morphs = nil, nil, nil, nil
	clear(m.morphOf)
}
