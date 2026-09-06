package gfx

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// Regenerate the independent GLSL layout oracle with the project shader tools.
//go:generate glslangValidator -V -S frag -o testdata/uniforms.frag.spv testdata/uniforms.frag

//go:embed testdata/uniforms.frag.spv
var uniformFixture []byte

type uniformPair struct {
	XY lin.Vec2
	Z  float32
}

type uniformFixtureBlock struct {
	Vector        lin.Vec3
	Scalar        float32
	Values        [2]float32
	Pairs         [2]uniformPair
	Matrices      [2]lin.Mat3
	Enabled       bool
	SignedValue   int32
	UnsignedValue uint32
	Colour        Color
	Transform     lin.Mat4
}

func uniformFixtureValues() uniformFixtureBlock {
	v := uniformFixtureBlock{
		Vector: lin.V3(1, 2, 3), Scalar: 4, Values: [2]float32{5, 6},
		Pairs:   [2]uniformPair{{lin.V2(7, 8), 9}, {lin.V2(10, 11), 12}},
		Enabled: true, SignedValue: -13, UnsignedValue: 14, Colour: Color{0.25, 0.5, 0.75, 1},
	}
	for m := range v.Matrices {
		for i := range v.Matrices[m] {
			v.Matrices[m][i] = float32(20 + m*9 + i)
		}
	}
	for i := range v.Transform {
		v.Transform[i] = float32(40 + i)
	}
	return v
}

// TestUniformsCompiledLayout reads offsets and strides from glslang's SPIR-V,
// rather than deriving an expected block with the packer's alignment rules.
func TestUniformsCompiledLayout(t *testing.T) {
	words := make([]uint32, len(uniformFixture)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(uniformFixture[i*4:])
	}
	names := map[string]uint32{}
	members := map[uint32][]uint32{}
	offsets := map[[2]uint32]int{}
	matrixStrides := map[[2]uint32]int{}
	arrayStrides := map[uint32]int{}
	for i := 5; i < len(words); {
		n, op := int(words[i]>>16), words[i]&0xffff
		if n < 1 || i+n > len(words) {
			t.Fatal("invalid SPIR-V fixture instruction")
		}
		a := words[i+1 : i+n]
		switch op {
		case 5: // OpName
			b := make([]byte, (len(a)-1)*4)
			for j, word := range a[1:] {
				binary.LittleEndian.PutUint32(b[j*4:], word)
			}
			names[strings.TrimRight(string(b), "\x00")] = a[0]
		case 30: // OpTypeStruct
			members[a[0]] = a[1:]
		case 71: // OpDecorate, ArrayStride
			if a[1] == 6 {
				arrayStrides[a[0]] = int(a[2])
			}
		case 72: // OpMemberDecorate
			key := [2]uint32{a[0], a[1]}
			if a[2] == 35 { // Offset
				offsets[key] = int(a[3])
			} else if a[2] == 7 { // MatrixStride
				matrixStrides[key] = int(a[3])
			}
		}
		i += n
	}
	block, pair := names["Parameters"], names["Pair"]
	if block == 0 || pair == 0 || len(members[block]) != 10 {
		t.Fatal("fixture lost named uniform structures")
	}
	offset := func(member uint32) int { return offsets[[2]uint32{block, member}] }
	if offset(1) != 12 || arrayStrides[members[block][2]] != 16 || arrayStrides[members[block][4]] != 48 || matrixStrides[[2]uint32{block, 4}] != 16 {
		t.Fatal("fixture no longer exercises strict std140 vec3/scalar/array/matrix layout")
	}
	want := make([]byte, offset(9)+64)
	put := func(at int, value float32) { binary.LittleEndian.PutUint32(want[at:], math.Float32bits(value)) }
	v := uniformFixtureValues()
	for i, value := range []float32{1, 2, 3} {
		put(offset(0)+i*4, value)
	}
	put(offset(1), v.Scalar)
	for i, value := range v.Values {
		put(offset(2)+i*arrayStrides[members[block][2]], value)
	}
	for i, value := range v.Pairs {
		base := offset(3) + i*arrayStrides[members[block][3]]
		put(base+offsets[[2]uint32{pair, 0}], value.XY.X)
		put(base+offsets[[2]uint32{pair, 0}]+4, value.XY.Y)
		put(base+offsets[[2]uint32{pair, 1}], value.Z)
	}
	for m, matrix := range v.Matrices {
		for i, value := range matrix {
			put(offset(4)+m*arrayStrides[members[block][4]]+(i/3)*matrixStrides[[2]uint32{block, 4}]+(i%3)*4, value)
		}
	}
	binary.LittleEndian.PutUint32(want[offset(5):], 1)
	binary.LittleEndian.PutUint32(want[offset(6):], uint32(v.SignedValue))
	binary.LittleEndian.PutUint32(want[offset(7):], v.UnsignedValue)
	for i, value := range []float32{v.Colour.R, v.Colour.G, v.Colour.B, v.Colour.A} {
		put(offset(8)+i*4, value)
	}
	for i, value := range v.Transform {
		put(offset(9)+(i/4)*matrixStrides[[2]uint32{block, 9}]+(i%4)*4, value)
	}
	s := new(Shader)
	if err := s.SetUniforms(&v); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.block, want) {
		t.Fatalf("packed block differs from independently compiled GLSL layout:\ngot %x\nwant%x", s.block, want)
	}
	v.Scalar = 999
	if !bytes.Equal(s.block, want) {
		t.Fatal("uniforms borrowed the caller's memory")
	}
}

func TestUniformsValidationPreservesBlock(t *testing.T) {
	s := new(Shader)
	if err := s.SetUniforms(uniformFixtureValues()); err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(s.block)
	for _, value := range []any{nil, (*struct{ X float32 })(nil), 1, struct{}{}, struct{ X int }{}, struct{ X float64 }{}, struct{ X string }{}, struct{ X []float32 }{}, struct{ X *float32 }{}, struct{ X map[string]float32 }{}, struct{ hidden float32 }{}, struct{ X [0]float32 }{}, struct{ X [65]float32 }{}, struct{ Nested struct{ Bad []byte } }{}} {
		if err := s.SetUniforms(value); err == nil {
			t.Errorf("accepted unsupported block %T", value)
		}
		if !bytes.Equal(s.block, want) {
			t.Fatalf("invalid %T changed prior uniforms", value)
		}
	}
	// Build only a type, never a huge Go value. Planning must reject this
	// before either multiplication overflow or a per-element allocation.
	huge := reflect.ArrayOf(1<<29, reflect.TypeFor[lin.Mat4]())
	if _, err := cachedUniformPlan(huge); err == nil {
		t.Fatal("accepted huge array")
	}
	type scalar float32
	type flag bool
	if err := s.SetUniforms(struct {
		X  scalar
		On flag
	}{7, true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUniforms(struct {
		X  scalar
		On flag
	}{8, false}); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(s.block[4:]) != 0 || !bytes.Equal(s.block[8:], make([]byte, 8)) {
		t.Fatal("false/padding retained previous data")
	}
}

func TestUniformsRendered(t *testing.T) {
	g := newHeadless(t, 64, 32)
	s, err := g.NewShader(uniformFixture)
	if err != nil {
		t.Fatal(err)
	}
	v := uniformFixtureValues()
	img := frame2D(t, g, func() {
		if err := s.SetUniforms(v); err != nil {
			t.Fatal(err)
		}
		g.Shaded(s, func() { g.FillRect(0, 0, 32, 32, White) })
		v.Scalar = 99
		if err := s.SetUniforms(v); err != nil {
			t.Fatal(err)
		}
		g.Shaded(s, func() { g.FillRect(32, 0, 32, 32, White) })
	})
	if good, bad := img.RGBAAt(16, 16), img.RGBAAt(48, 16); good.G < 200 || good.R > 30 || bad.R < 200 || bad.G > 30 {
		t.Fatalf("packed values or queued uniform snapshots wrong: correct%v changed%v", good, bad)
	}
}

func TestUniformsFrameFailures(t *testing.T) {
	for _, failure := range []string{"arena", "upload"} {
		t.Run(failure, func(t *testing.T) {
			g := newHeadless(t, 32, 32)
			s, err := g.NewShader(uniformFixture)
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := g.begin(Black); err != nil || !ok {
				t.Fatal(err)
			}
			draws := 128
			if failure == "arena" {
				// Make the second offset exceed Arena's cap, without allocating
				// a gigabyte or changing production limits.
				u := *g.uniforms
				u.Align = 1 << 30
				g.arena = u.NewArena()
				draws = 2
			} else {
				create := vk.VkCreateBuffer
				vk.VkCreateBuffer = func(d vk.VkDevice, info *vk.VkBufferCreateInfo, alloc *vk.VkAllocationCallbacks, out *vk.VkBuffer) vk.VkResult {
					if info.Usage&vk.VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT != 0 {
						return vk.VK_ERROR_DEVICE_LOST
					}
					return create(d, info, alloc, out)
				}
				t.Cleanup(func() { vk.VkCreateBuffer = create })
			}
			for i := range draws {
				v := uniformFixtureValues()
				v.Scalar = float32(i)
				if err := s.SetUniforms(v); err != nil {
					t.Fatal(err)
				}
				g.Shaded(s, func() { g.FillRect(0, 0, 32, 32, White) })
			}
			_, err = g.end(false)
			if failure == "arena" {
				if err == nil || !strings.Contains(err.Error(), "uniform arena full") {
					t.Fatalf("arena error lost: %v", err)
				}
			} else if !errors.Is(err, render.ErrDeviceLost) {
				t.Fatalf("uniform upload lost device-loss classification: %v", err)
			}
		})
	}
}
