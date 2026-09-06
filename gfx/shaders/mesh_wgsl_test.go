package shaders

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMeshWGSLStages(t *testing.T) {
	terrain, err := os.ReadFile("terrain_default.wgsl")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		body   string
		stages []Stage
	}{
		{"default", "fn surface(s: Surface) -> Surface { return s; }", []Stage{StageFrag, StageOITFrag, StageVert, StageSkinVert, StageShadowVert, StageShadowSkinVert}},
		{"terrain", string(terrain), []Stage{StageFrag, StageOITFrag}},
		{"hooks", `
struct Params { offset: vec4f, tint: vec4f }
@group(4) @binding(0) var<uniform> params: Params;
fn vertex(v: VertexData) -> VertexData {
    var moved = v;
    moved.position = v.position + params.offset.xyz * sampleImage0(v.uv).r;
    return moved;
}
fn surface(s: Surface) -> Surface {
    var changed = s;
    changed.albedo = s.albedo * sampleImage1(s.uv).rgb * params.tint.rgb;
    return changed;
}
fn finish(lit: vec4f, s: Surface) -> vec4f {
    return vec4f(lit.rgb, lit.a * params.tint.a);
}`, []Stage{StageFrag, StageOITFrag, StageVert, StageSkinVert, StageShadowVert, StageShadowSkinVert}},
	} {
		for _, stage := range tc.stages {
			t.Run(tc.name+"/"+stage.String(), func(t *testing.T) {
				source, _, err := composeMesh(stage, tc.body)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "mesh.wgsl")
				if err := os.WriteFile(path, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				data, err := (Compiler{}).CompileRaw(context.Background(), source)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				spv := filepath.Join(t.TempDir(), "mesh.spv")
				if err := os.WriteFile(spv, data, 0600); err != nil {
					t.Fatal(err)
				}
				if val, err := exec.LookPath("spirv-val"); err == nil {
					if output, err := exec.Command(val, "--target-env", "vulkan1.1", spv).CombinedOutput(); err != nil {
						t.Fatalf("%v: %s", err, output)
					}
				}
			})
		}
	}
}
