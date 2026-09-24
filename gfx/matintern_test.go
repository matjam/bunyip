package gfx

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
	"unsafe"

	"github.com/matjam/bunyip/lin"
)

// withDefaults is a material as the draw path sees it: a zero base colour
// is white and a zero roughness is 0.6. It is written out here rather
// than calling defaultedMaterial, so the test checks that too.
func withDefaults(m Material) Material {
	if m.BaseColor == (Color{}) {
		m.BaseColor = White
	}
	if m.Roughness == 0 {
		m.Roughness = 0.6
	}
	return m
}

func sameBytes(a, b *Material) bool {
	return unsafe.String((*byte)(unsafe.Pointer(a)), unsafe.Sizeof(*a)) == unsafe.String((*byte)(unsafe.Pointer(b)), unsafe.Sizeof(*b))
}

// TestMaterialIntern queues draws over a spread of materials, many of
// them repeated and some differing in a single field, a negative zero or
// a default, and checks the queue's material table: every draw's entry
// holds exactly the material it was drawn with, two different materials
// never share an entry, and a material drawn again reuses its entry.
func TestMaterialIntern(t *testing.T) {
	g := newHeadless(t, 32, 32)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	tex, err := g.NewBlankTexture(2, 2, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	negZero := float32(math.Copysign(0, -1))
	r := rand.New(rand.NewSource(3))
	pick := func(vals ...float32) float32 { return vals[r.Intn(len(vals))] }
	var mats []Material
	for range 600 {
		m := Material{
			BaseColor: Color{pick(0, 1, 0.5), pick(0, 1), 1, pick(0, 1)},
			Roughness: pick(0, 0.6, 0.3),
			Metallic:  pick(0, negZero, 1),
			Blend:     r.Intn(4) == 0,
			Outline:   pick(0, 0, 0, 2),
			Shells:    r.Intn(8) / 7 * 2,
			IOR:       pick(0, 1.5),
		}
		if r.Intn(3) == 0 {
			m.Texture = tex
		}
		if r.Intn(5) == 0 {
			m.UVTransform = lin.Affine{A: 1, D: pick(1, negZero)}
		}
		mats = append(mats, m)
	}
	// Repeat some materials straight after themselves and some far away.
	for i := range 200 {
		mats = append(mats, mats[i*3%len(mats)])
	}
	ok, err := g.begin(Black)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	var first []int // the first draw of each queued material
	for _, m := range mats {
		first = append(first, len(g.cur.draws))
		g.DrawMesh(cube, m, lin.Identity())
	}
	q := g.cur
	distinct := map[string]uint32{}
	for i, m := range mats {
		want := withDefaults(m)
		d := &q.draws[first[i]]
		fm := &q.mats[d.mat]
		if !sameBytes(&fm.mat, &want) {
			t.Fatalf("draw %d: its table entry holds %+v, want %+v", i, fm.mat, want)
		}
		key := unsafe.String((*byte)(unsafe.Pointer(&want)), unsafe.Sizeof(want))
		if at, seen := distinct[key]; seen && at != d.mat {
			t.Errorf("draw %d: an identical material has entries %d and %d", i, at, d.mat)
		}
		distinct[key] = d.mat
	}
	// Different materials never share an entry.
	owner := map[uint32]string{}
	for key, at := range distinct {
		if other, taken := owner[at]; taken && other != key {
			t.Fatalf("entry %d holds two different materials", at)
		}
		owner[at] = key
	}
	if len(q.mats) != len(distinct) {
		t.Errorf("the table holds %d entries for %d distinct materials", len(q.mats), len(distinct))
	}
	q.draws = q.draws[:0]
	if _, err := g.end(false); err != nil {
		t.Fatal(err)
	}
}

// TestMaterialOwnerPanics checks that a material with another Graphics'
// texture or shader still panics from DrawMesh with the same message
// every time it is drawn, although materials are checked once per frame.
func TestMaterialOwnerPanics(t *testing.T) {
	g := newHeadless(t, 32, 32)
	other, err := newGraphics(g.r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.destroy)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	foreign, err := other.NewBlankTexture(2, 2, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	own, err := g.NewBlankTexture(2, 2, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := g.begin(Black)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer func() {
		g.cur.draws = g.cur.draws[:0]
		if _, err := g.end(false); err != nil {
			t.Fatal(err)
		}
	}()
	panics := func(mat Material) (msg string) {
		defer func() {
			if v := recover(); v != nil {
				msg = fmt.Sprint(v)
			}
		}()
		g.DrawMesh(cube, mat, lin.Identity())
		return ""
	}
	good := Material{Texture: own, Roughness: 0.5}
	for k := range 3 {
		if msg := panics(good); msg != "" {
			t.Fatalf("draw %d of an owned material panicked: %s", k, msg)
		}
		for _, bad := range []Material{
			{Texture: foreign, Roughness: 0.5},
			{Texture: own, NormalTexture: foreign},
			{FurTexture: foreign},
		} {
			if msg := panics(bad); !strings.Contains(msg, "gfx: texture belongs to another Graphics") {
				t.Errorf("draw %d of a material with a foreign texture: panic %q", k, msg)
			}
		}
	}
	n := len(g.cur.draws)
	if n != 3 {
		t.Errorf("%d draws queued, want the 3 owned ones", n)
	}
}
