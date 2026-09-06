package shaders

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

const compilerTestSource = `@fragment fn main() -> @location(0) vec4f { return vec4f(1.0,0.0,0.0,1.0); }`

func TestHookDetectionIgnoresComments(t *testing.T) {
	source := `/* outer /* fn vertex(v: VertexData) -> VertexData {} */ still comment */
// fn vertex(v: VertexData) -> VertexData {}
fn surface(s: Surface) -> Surface { return s; }`
	if HasVertexHook(source) {
		t.Fatal("comment created vertex stages")
	}
	if !HasVertexHook(source + "\nfn /* comment */ vertex(v: VertexData) -> VertexData { return v; }") {
		t.Fatal("real vertex hook with a comment was missed")
	}
}

func TestCompilerNativeAndConcurrent(t *testing.T) {
	c := Compiler{}
	want, err := c.CompileRaw(context.Background(), compilerTestSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCompiledSPIRV(want); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, err := c.CompileRaw(context.Background(), compilerTestSource)
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(want, got) {
				t.Error("concurrent compilation differs")
			}
		})
	}
	wg.Wait()
}

func TestCompilerErrors(t *testing.T) {
	c := Compiler{}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.CompileRaw(canceled, compilerTestSource); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled compile: %v", err)
	}
	for _, source := range []string{"fn {", `@fragment fn main() -> @location(0) vec4f { return missing; }`} {
		if _, err := c.CompileRaw(context.Background(), source); err == nil {
			t.Fatalf("invalid source accepted: %s", source)
		}
	}
}

// External validation complements native IR validation when SPIRV-Tools is
// installed. It validates every embedded production program, including variants.
func TestBuiltinsSPIRVValidation(t *testing.T) {
	tool, err := exec.LookPath("spirv-val")
	if err != nil {
		t.Skip("SPIRV-Tools not installed")
	}
	files, err := filepath.Glob("*.spv")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 42 {
		t.Fatalf("expected 42 built-in programs, got %d", len(files))
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateCompiledSPIRV(data); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(tool, "--target-env", "vulkan1.3", file).CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
		})
	}
}
