package lin

import "testing"

func TestAffineTransformRect(t *testing.T) {
	for _, tc := range []struct {
		m       Affine
		r, want Rect
	}{
		{Translate2(10, 20).Mul(Scale2(-2, 3)), R(1, 2, 4, 5), R(0, 26, 8, 15)},
		{Shear2(2, -1), R(0, 0, 4, 3), R(0, -4, 10, 7)},
		{Identity2(), R(5, 4, -3, -2), R(2, 2, 3, 2)},
		{Shear2(1, 0), R(2, 3, 0, 4), R(5, 3, 4, 4)},
	} {
		if got := tc.m.TransformRect(tc.r); got != tc.want {
			t.Errorf("%v.TransformRect(%v)=%v, want %v", tc.m, tc.r, got, tc.want)
		}
	}
}
