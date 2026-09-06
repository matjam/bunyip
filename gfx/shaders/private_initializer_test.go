package shaders

import (
	"context"
	"strings"
	"testing"
)

func TestCompilerRejectsPrivateInitializers(t *testing.T) {
	for _, tc := range []struct{ name, declaration, value string }{
		{"scalar", "var<private> gain: f32 = 0.5;", "vec4f(gain)"},
		{"vector", "var<private> tint: vec4f = vec4f(1.0, 0.5, 0.25, 1.0);", "tint"},
		{"zero", "var<private> gain: f32 = 0.0;", "vec4f(gain)"},
		{"default_space", "var gain: f32 = 0.5;", "vec4f(gain)"},
		{"nested_comments", "/* var<private> ignored = 1.0; /* nested */ */ var /* between */ < private > gain: f32 = 0.5;", "vec4f(gain)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.declaration + "\n@fragment fn main() -> @location(0) vec4f { return " + tc.value + "; }"
			data, err := (Compiler{}).CompileRaw(context.Background(), source)
			if err == nil || len(data) != 0 {
				t.Fatalf("initialized private global compiled: %d bytes, error %v", len(data), err)
			}
			if !strings.Contains(err.Error(), "private global") || !strings.Contains(err.Error(), "entry point or hook") {
				t.Fatalf("missing actionable diagnostic: %v", err)
			}
		})
	}
}

func TestCompilerAcceptsPrivateDeclarationsWithoutInitializer(t *testing.T) {
	for _, source := range []string{
		"var<private> value: f32; @fragment fn main() -> @location(0) vec4f { return vec4f(value); }",
		"var<private> value: vec4f; @fragment fn main() -> @location(0) vec4f { value = vec4f(0.5); return value; }",
		"@fragment fn main() -> @location(0) vec4f { var value = vec4f(0.5); return value; }",
	} {
		if _, err := (Compiler{}).CompileRaw(context.Background(), source); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPrivateInitializerScannerIgnoresUnrelatedTokens(t *testing.T) {
	for _, source := range []string{
		"// var<private> ignored = 1;\nvar<private> values: array<vec4f, 3>;",
		"/* outer /* var<private> ignored = 1; */ */ const flag = 2 == 2;",
		"fn main() { var value = 1.0; if value == 1.0 { value += 1.0; } }",
		"var<private> values: array<vec4f, select(2, 3, 1 == 1)>;",
		"@group(0) @binding(0) var<uniform> params: Params;",
	} {
		if name := initializedPrivateGlobal(source); name != "" {
			t.Fatalf("false initializer %q in %s", name, source)
		}
	}
}
