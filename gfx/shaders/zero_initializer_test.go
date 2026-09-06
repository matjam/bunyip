package shaders

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const zeroInitializerSource = `
struct State { count: u32, flag: bool, values: array<vec4f, 2>, transform: mat3x3f, }
var<private> first: State;
var<private> second: State;
var<private> sum: f32;
@fragment fn main() -> @location(0) vec4f {
    sum += 1.0;
    first.count += 1u;
    return vec4f(sum, f32(first.count + second.count), select(0.0, 1.0, first.flag), 1.0)
        + first.values[0] + second.values[1] + vec4f(first.transform[0], 0.0);
}`

func TestCompilerPrivateZeroInitializers(t *testing.T) {
	data, err := (Compiler{}).CompileRaw(context.Background(), zeroInitializerSource)
	if err != nil {
		t.Fatal(err)
	}
	words := make([]uint32, len(data)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(data[4*i:])
	}
	pointees, nulls := map[uint32]uint32{}, map[uint32]uint32{}
	uses := map[uint32]int{}
	privateCount := 0
	for i := 5; i < len(words); {
		n, op := int(words[i]>>16), words[i]&0xffff
		a := words[i+1 : i+n]
		switch op {
		case 32:
			pointees[a[0]] = a[2]
		case 46:
			if a[1] >= words[3] {
				t.Fatal("null ID is outside the module bound")
			}
			nulls[a[1]] = a[0]
		case 59:
			if a[2] == 6 {
				privateCount++
				if len(a) != 4 {
					t.Fatal("private variable has no initializer")
				}
				if typ, ok := nulls[a[3]]; !ok || typ != pointees[a[0]] {
					t.Fatal("initializer is not a preceding null of the pointee type")
				}
				uses[a[3]]++
			}
		}
		i += n
	}
	if privateCount != 3 || len(uses) != 2 {
		t.Fatalf("expected three private variables sharing two nulls: count=%d uses=%v", privateCount, uses)
	}
	again, err := zeroPrivateGlobals(data)
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("repair is not idempotent: %v", err)
	}

	t.Run("Vulkan 1.1 validation", func(t *testing.T) {
		tool, err := exec.LookPath("spirv-val")
		if err != nil {
			t.Skip("SPIRV-Tools not installed")
		}
		path := filepath.Join(t.TempDir(), "zero.spv")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(tool, "--target-env", "vulkan1.1", path).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	})
}

// These instruction fixtures isolate preservation, storage class selection and
// ID allocation from compiler optimizations that may remove unused variables.
func TestPrivateZeroRepairPreservesOtherVariables(t *testing.T) {
	words := []uint32{0x07230203, 0x00010300, 0, 20, 0,
		3<<16 | 22, 1, 32, // float
		4<<16 | 32, 2, 6, 1, // Private float pointer
		4<<16 | 32, 3, 3, 1, // Output float pointer
		4<<16 | 32, 4, 7, 1, // Function float pointer
		4<<16 | 43, 1, 5, 0x3f800000, // float 1
		5<<16 | 59, 2, 6, 6, 5, // explicitly initialized private
		4<<16 | 59, 3, 7, 3, // output
		4<<16 | 59, 4, 8, 7, // function variable (untouched)
		4<<16 | 59, 2, 9, 6, // private with no initializer
		4<<16 | 59, 2, 10, 6, // same type shares the null
	}
	encode := func(words []uint32) []byte {
		data := make([]byte, len(words)*4)
		for i, w := range words {
			binary.LittleEndian.PutUint32(data[4*i:], w)
		}
		return data
	}
	original := encode(words)
	got, err := zeroPrivateGlobals(original)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]uint32{}, words[:len(words)-8]...)
	want[3] = 21
	want = append(want, 3<<16|46, 1, 20, 5<<16|59, 2, 9, 6, 20, 5<<16|59, 2, 10, 6, 20)
	if !bytes.Equal(got, encode(want)) {
		t.Fatalf("repair changed unrelated instructions:\ngot %x\nwant%x", got, encode(want))
	}
	if !bytes.Equal(original, encode(words)) {
		t.Fatal("repair mutated input")
	}
}
