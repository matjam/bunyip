// Package shaders holds the gfx package's WGSL sources and committed SPIR-V,
// and the preludes that game shaders are compiled against. Compose builds
// WGSL without compiling; Compiler and bunyip-shader use the native Go compiler. Mesh bundles contain regular and order-independent fragment
// programs and optionally static/skinned vertex programs for lit and
// shadow passes. Embedded byte slices are shared and must not be modified.
package shaders

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"regexp"
)

// The 2D and mesh programs, including the engine's own, are game-style
// shader bodies compiled against the preludes by bunyip-shader.

//go:generate go run ../../cmd/bunyip-shader -raw -o sprite.vert.spv sprite.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o sprite.frag.spv sprite_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o sdf.frag.spv sdf_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o text_outline.frag.spv text_outline.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o colormatrix.frag.spv colormatrix_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind sprite -o lit.frag.spv lit_default.wgsl

var (
	//go:embed sprite.vert.spv
	SpriteVert []byte
	//go:embed sprite.frag.spv
	SpriteFrag []byte
	//go:embed colormatrix.frag.spv
	MatrixFrag []byte
	//go:embed lit.frag.spv
	LitFrag []byte
	//go:embed sdf.frag.spv
	SDFFrag []byte
	//go:embed text_outline.frag.spv
	TextOutlineFrag []byte
)

//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage frag -o pbr.frag.spv pbr_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage oitfrag -o pbr_oit.frag.spv pbr_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage vert -o pbr.vert.spv pbr_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage skinvert -o pbr_skin.vert.spv pbr_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage shadowvert -o shadow.vert.spv pbr_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage shadowskinvert -o shadow_skin.vert.spv pbr_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -stage frag -o terrain.frag.spv terrain_default.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o shadow.frag.spv shadow.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o post.vert.spv post.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o post.frag.spv post.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o bright.frag.spv bright.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o blur.frag.spv blur.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o fxaa.frag.spv fxaa.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o ssao.frag.spv ssao.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o ssr.frag.spv ssr.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o aoblur.frag.spv aoblur.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o velocity.vert.spv velocity.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o velocity_skin.vert.spv velocity_skin.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o velocity.frag.spv velocity.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o taa.frag.spv taa.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o dof.frag.spv dof.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o motionblur.frag.spv motionblur.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o godrays.frag.spv godrays.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o oit.frag.spv oit.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o sky.frag.spv sky.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o skyparam.frag.spv skyparam.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o line.vert.spv line.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o line.frag.spv line.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o particle.vert.spv particle.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o particle.frag.spv particle.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o particle3d.vert.spv particle3d.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o particle3d.frag.spv particle3d.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o outline.vert.spv outline.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o solid.frag.spv solid.frag.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o decal.vert.spv decal.vert.wgsl
//go:generate go run ../../cmd/bunyip-shader -raw -o decal.frag.spv decal.frag.wgsl

var (
	//go:embed sky.frag.spv
	SkyFrag []byte
	//go:embed skyparam.frag.spv
	SkyParamFrag []byte
	//go:embed line.vert.spv
	LineVert []byte
	//go:embed line.frag.spv
	LineFrag []byte
	//go:embed particle.vert.spv
	ParticleVert []byte
	//go:embed particle.frag.spv
	ParticleFrag []byte
	//go:embed particle3d.vert.spv
	Particle3DVert []byte
	//go:embed particle3d.frag.spv
	Particle3DFrag []byte
	//go:embed outline.vert.spv
	OutlineVert []byte
	//go:embed solid.frag.spv
	SolidFrag []byte
	//go:embed decal.vert.spv
	DecalVert []byte
	//go:embed decal.frag.spv
	DecalFrag []byte
	//go:embed pbr.frag.spv
	PBRFrag []byte
	//go:embed pbr_oit.frag.spv
	PBROITFrag []byte
	// TerrainFrag blends four tiling layers by a splat map, for gfx.Terrain.
	//go:embed terrain.frag.spv
	TerrainFrag []byte
	//go:embed pbr.vert.spv
	PBRVert []byte
	//go:embed pbr_skin.vert.spv
	PBRSkinVert []byte
	//go:embed shadow.vert.spv
	ShadowVert []byte
	//go:embed shadow_skin.vert.spv
	ShadowSkinVert []byte
	//go:embed shadow.frag.spv
	ShadowFrag []byte
	//go:embed post.vert.spv
	PostVert []byte
	//go:embed post.frag.spv
	PostFrag []byte
	//go:embed bright.frag.spv
	BrightFrag []byte
	//go:embed blur.frag.spv
	BlurFrag []byte
	//go:embed fxaa.frag.spv
	FXAAFrag []byte
	//go:embed ssao.frag.spv
	SSAOFrag []byte
	//go:embed ssr.frag.spv
	SSRFrag []byte
	//go:embed aoblur.frag.spv
	AOBlurFrag []byte
	//go:embed velocity.vert.spv
	VelocityVert []byte
	//go:embed velocity_skin.vert.spv
	VelocitySkinVert []byte
	//go:embed velocity.frag.spv
	VelocityFrag []byte
	//go:embed taa.frag.spv
	TAAFrag []byte
	//go:embed dof.frag.spv
	DOFFrag []byte
	//go:embed motionblur.frag.spv
	MotionBlurFrag []byte
	//go:embed godrays.frag.spv
	GodRaysFrag []byte
	//go:embed oit.frag.spv
	OITFrag []byte
)

// Kind is which pipeline a game shader is written for.
type Kind string

const (
	// Sprite shaders colour 2D fragments: sprites, text, shapes.
	Sprite Kind = "sprite"
	// Mesh shaders adjust a Surface before the engine lights it, and may
	// move vertices first.
	Mesh Kind = "mesh"
)

// Stage is one program of a mesh shader.
type Stage uint32

const (
	StageFrag           Stage = iota // the lit fragment program
	StageVert                        // static meshes, lit pass
	StageSkinVert                    // skinned meshes, lit pass
	StageShadowVert                  // static meshes, shadow pass
	StageShadowSkinVert              // skinned meshes, shadow pass
	StageOITFrag                     // the fragment program of the order-independent transparency pass
	stageCount
)

// String names a stage as bunyip-shader's -stage flag spells it.
func (s Stage) String() string {
	names := [...]string{"frag", "vert", "skinvert", "shadowvert", "shadowskinvert", "oitfrag"}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("Stage(%d)", int(s))
}

// ParseStage reads a -stage flag value.
func ParseStage(s string) (Stage, bool) {
	for st := Stage(0); st < stageCount; st++ {
		if st.String() == s {
			return st, true
		}
	}
	return 0, false
}

// HasVertexHook reports whether the source declares the optional vertex hook.
// Comments are ignored, including nested block comments.
func HasVertexHook(source string) bool { return hookPresent(source, "vertex") }

func hookPresent(source, name string) bool {
	return regexp.MustCompile(`\bfn\s+` + name + `\s*\(`).MatchString(withoutComments(source))
}

func withoutComments(source string) string {
	out := []byte(source)
	depth := 0
	for i := 0; i < len(out); {
		if depth > 0 {
			if i+1 < len(out) && source[i:i+2] == "/*" {
				out[i], out[i+1] = ' ', ' '
				depth++
				i += 2
				continue
			}
			if i+1 < len(out) && source[i:i+2] == "*/" {
				out[i], out[i+1] = ' ', ' '
				depth--
				i += 2
				continue
			}
			if out[i] != '\n' {
				out[i] = ' '
			}
			i++
			continue
		}
		if i+1 < len(out) && source[i:i+2] == "/*" {
			out[i], out[i+1] = ' ', ' '
			depth = 1
			i += 2
			continue
		}
		if i+1 < len(out) && source[i:i+2] == "//" {
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}
		i++
	}
	return string(out)
}

// Compose adds Bunyip's WGSL declarations and entry point around game source.
// Kind defaults to Sprite. The second result is the number of prefix lines
// before the source body, useful when reading compiler diagnostics.
func Compose(kind Kind, stage Stage, source string) (string, int, error) {
	if kind == "" {
		kind = Sprite
	}
	switch kind {
	case Sprite:
		if stage != StageFrag {
			return "", 0, fmt.Errorf("sprite shaders have no %s stage", stage)
		}
		return composeSprite(source)
	case Mesh:
		return composeMesh(stage, source)
	default:
		return "", 0, fmt.Errorf("unknown shader kind %q", kind)
	}
}

// A bundle holds the programs of a mesh shader in one
// file: the magic, a count, then (stage, size, SPIR-V) records. A file
// without the magic is plain SPIR-V for the fragment stage alone.
var bundleMagic = []byte("BYSH")

// Bundle copies stage programs into a new blob in stage-number order.
// Supply only defined Stage values and include StageFrag for Unbundle.
// It does not validate SPIR-V program bytes.
func Bundle(stages map[Stage][]byte) []byte {
	out := append([]byte{}, bundleMagic...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(stages)))
	for st := Stage(0); st < stageCount; st++ {
		spv, ok := stages[st]
		if !ok {
			continue
		}
		out = binary.LittleEndian.AppendUint32(out, uint32(st))
		out = binary.LittleEndian.AppendUint32(out, uint32(len(spv)))
		out = append(out, spv...)
	}
	return out
}

// Unbundle splits a bundle into its stages; plain SPIR-V comes back as
// the fragment stage alone. Returned slices alias data. It validates bundle
// record bounds and stage numbers, but not the SPIR-V bytes themselves;
// NewMeshShader validates those when loading the programs.
func Unbundle(data []byte) (map[Stage][]byte, error) {
	if len(data) < 4 || string(data[:4]) != string(bundleMagic) {
		return map[Stage][]byte{StageFrag: data}, nil
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("shader bundle is truncated")
	}
	n := binary.LittleEndian.Uint32(data[4:8])
	out := map[Stage][]byte{}
	p := 8
	for range n {
		if p+8 > len(data) {
			return nil, fmt.Errorf("shader bundle is truncated")
		}
		st := Stage(binary.LittleEndian.Uint32(data[p:]))
		size := int(binary.LittleEndian.Uint32(data[p+4:]))
		p += 8
		if p+size > len(data) || st >= stageCount {
			return nil, fmt.Errorf("shader bundle is corrupt")
		}
		out[st] = data[p : p+size]
		p += size
	}
	if _, ok := out[StageFrag]; !ok {
		return nil, fmt.Errorf("shader bundle has no fragment stage")
	}
	return out, nil
}
