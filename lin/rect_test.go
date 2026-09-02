package lin

import (
	"math"
	"testing"
)

func TestRect(t *testing.T) {
	r := R(10, 10, 20, 10)
	if !r.Contains(V2(10, 10)) || r.Contains(V2(30, 15)) || r.Contains(V2(15, 20)) {
		t.Error("Contains is inclusive at the top-left and exclusive at the bottom-right")
	}
	s := R(25, 15, 20, 20)
	if !r.Intersects(s) || r.Intersects(R(40, 40, 5, 5)) {
		t.Error("Intersects")
	}
	if got := r.Intersect(s); got != R(25, 15, 5, 5) {
		t.Errorf("Intersect = %v", got)
	}
	if got := r.Intersect(R(40, 40, 5, 5)); !got.Empty() {
		t.Errorf("disjoint Intersect = %v, want empty", got)
	}
	if got := r.Union(s); got != R(10, 10, 35, 25) {
		t.Errorf("Union = %v", got)
	}
	if got := (Rect{}).Union(r); got != r {
		t.Errorf("Union with the zero rect = %v", got)
	}
	if got := r.Inset(2); got != R(12, 12, 16, 6) {
		t.Errorf("Inset = %v", got)
	}
	if got := RectAround(V2(0, 0), 4, 2); got != R(-2, -1, 4, 2) {
		t.Errorf("RectAround = %v", got)
	}
	if got := RectBetween(V2(5, 9), V2(1, 3)); got != R(1, 3, 4, 6) {
		t.Errorf("RectBetween = %v", got)
	}
	if got := r.Clamp(V2(0, 100)); got != V2(10, 20) {
		t.Errorf("Clamp = %v", got)
	}
	if r.Center() != V2(20, 15) || r.Max() != V2(30, 20) {
		t.Error("Center or Max")
	}
}

func TestVecHelpers(t *testing.T) {
	if V2(1, 0).Perp() != V2(0, 1) {
		t.Error("Perp")
	}
	if a := V2(0, 1).Angle(); math.Abs(float64(a)-math.Pi/2) > 1e-6 {
		t.Errorf("Angle = %v", a)
	}
	if p := V2(1, 0).Rotate(math.Pi / 2); p.Distance(V2(0, 1)) > 1e-6 {
		t.Errorf("Rotate = %v", p)
	}
	if V3(3, 4, 0).Project(V3(0, 2, 0)) != V3(0, 4, 0) {
		t.Error("Project")
	}
	if V3(1, -1, 0).Reflect(V3(0, 1, 0)) != V3(1, 1, 0) {
		t.Error("Reflect")
	}
	if V3(-1, 2, -3).Abs() != V3(1, 2, 3) || V3(1, 5, 3).Min(V3(2, 4, 3)) != V3(1, 4, 3) {
		t.Error("Abs or Min")
	}
}

func TestEulerRoundTrip(t *testing.T) {
	for _, c := range [][3]float32{{0.3, -0.4, 0.5}, {2.5, 1.2, -2}, {-1, 0, 0}, {0, 0, 1}} {
		q := FromEuler(c[0], c[1], c[2])
		y, p, r := q.Euler()
		back := FromEuler(y, p, r)
		for _, v := range []Vec3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}} {
			if q.Rotate(v).Distance(back.Rotate(v)) > 1e-4 {
				t.Errorf("euler %v -> %v %v %v rotates %v differently", c, y, p, r, v)
			}
		}
	}
}

func TestQuatLookAtAndAxisAngle(t *testing.T) {
	fwd := V3(1, 0, -1).Norm()
	q := QuatLookAt(fwd, V3(0, 1, 0))
	if got := q.Rotate(V3(0, 0, -1)); got.Distance(fwd) > 1e-5 {
		t.Errorf("look at turns -Z to %v, want %v", got, fwd)
	}
	if got := q.Rotate(V3(0, 1, 0)); got.Y < 0.99 {
		t.Errorf("look at tilts up to %v", got)
	}
	axis, angle := AxisAngle(V3(0, 0, 1), 1.2).AxisAngle()
	if axis.Distance(V3(0, 0, 1)) > 1e-5 || math.Abs(float64(angle)-1.2) > 1e-5 {
		t.Errorf("AxisAngle = %v %v", axis, angle)
	}
	if axis, angle := QuatIdentity().AxisAngle(); angle != 0 || axis != V3(0, 1, 0) {
		t.Errorf("identity AxisAngle = %v %v", axis, angle)
	}
}

func TestDecompose(t *testing.T) {
	m := TRS(V3(1, 2, 3), AxisAngle(V3(0, 1, 0), 0.7), V3(2, 3, 4))
	tr, r, s := m.Decompose()
	if tr != V3(1, 2, 3) || s.Distance(V3(2, 3, 4)) > 1e-5 {
		t.Errorf("Decompose t %v s %v", tr, s)
	}
	if got := r.Rotate(V3(1, 0, 0)); got.Distance(AxisAngle(V3(0, 1, 0), 0.7).Rotate(V3(1, 0, 0))) > 1e-5 {
		t.Errorf("Decompose rotation %v", got)
	}
	n := Scale(V3(2, 1, 1)).NormalMatrix().MulVec(V3(1, 1, 0)).Norm()
	if math.Abs(float64(n.X)-0.4472) > 1e-3 {
		t.Errorf("normal matrix %v", n)
	}
}
