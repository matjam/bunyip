package lin

import (
	"math"
	"math/rand/v2"
	"testing"
)

// randTRS draws a translation, a unit rotation and a scale, with the
// scale sometimes negative (a mirror) and sometimes tiny or large.
func randTRS(r *rand.Rand) (Vec3, Quat, Vec3) {
	f := func(lo, hi float32) float32 { return lo + (hi-lo)*r.Float32() }
	t := V3(f(-100, 100), f(-100, 100), f(-100, 100))
	q := Quat{f(-1, 1), f(-1, 1), f(-1, 1), f(-1, 1)}.Norm()
	s := V3(f(0.01, 10), f(0.01, 10), f(0.01, 10))
	if r.IntN(4) == 0 {
		s.X = -s.X
	}
	return t, q, s
}

// trsByMul is TRS as it was first written, as three multiplies.
func trsByMul(t Vec3, r Quat, s Vec3) Mat4 {
	return Translate(t).Mul(r.Mat4()).Mul(Scale(s))
}

// closeRel reports whether every entry of a is within tol of b's,
// relative to the larger magnitude of the matrix.
func closeRel(a, b Mat4, tol float64) bool {
	scale := 1.0
	for i := range b {
		scale = max(scale, math.Abs(float64(b[i])))
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > tol*scale {
			return false
		}
	}
	return true
}

func TestTRSClosedForm(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 10000 {
		tr, q, s := randTRS(r)
		got, want := TRS(tr, q, s), trsByMul(tr, q, s)
		if !closeRel(got, want, 1e-6) {
			t.Fatalf("TRS(%v, %v, %v) = %v, want %v", tr, q, s, got, want)
		}
	}
	// Identity inputs give the identity exactly.
	if got := TRS(Vec3{}, QuatIdentity(), V3(1, 1, 1)); got != Identity() {
		t.Fatalf("identity TRS = %v", got)
	}
}

func TestMulAffine(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 10000 {
		a := TRS(randTRS(r))
		b := TRS(randTRS(r))
		got, want := a.MulAffine(b), a.Mul(b)
		// The same products are summed in the same order; only the
		// fusing of multiplies into adds can differ, by a rounding.
		if !closeRel(got, want, 1e-6) || got[3] != 0 || got[7] != 0 || got[11] != 0 || got[15] != 1 {
			t.Fatalf("%v.MulAffine(%v) = %v, want %v", a, b, got, want)
		}
	}
	// Shear and non-uniform scale are affine too.
	a := Mat4{1, 0.5, 0, 0, 0.25, 2, 0.1, 0, 0, 0, 3, 0, 4, 5, 6, 1}
	b := Translate(V3(1, 2, 3)).Mul(Rotate(0.7, V3(1, 1, 0)))
	if got, want := a.MulAffine(b), a.Mul(b); !closeRel(got, want, 1e-6) {
		t.Fatalf("sheared: got %v, want %v", got, want)
	}
}

var benchMat Mat4

func BenchmarkTRS(b *testing.B) {
	t, q, s := V3(1, 2, 3), AxisAngle(V3(0, 1, 0), 0.5), V3(1, 2, 1)
	for range b.N {
		benchMat = TRS(t, q, s)
	}
}

func BenchmarkMul(b *testing.B) {
	m, n := TRS(V3(1, 2, 3), AxisAngle(V3(0, 1, 0), 0.5), V3(1, 2, 1)), TRS(V3(3, 2, 1), AxisAngle(V3(1, 0, 0), 0.3), V3(1, 1, 1))
	b.Run("Mul", func(b *testing.B) {
		for range b.N {
			benchMat = m.Mul(n)
		}
	})
	b.Run("MulAffine", func(b *testing.B) {
		for range b.N {
			benchMat = m.MulAffine(n)
		}
	})
}
