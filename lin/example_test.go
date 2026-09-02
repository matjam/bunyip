package lin_test

import (
	"fmt"

	"github.com/matjam/bunyip/lin"
)

func ExampleVec3_Cross() {
	x, y := lin.V3(1, 0, 0), lin.V3(0, 1, 0)
	fmt.Println(x.Cross(y))
	fmt.Println(x.Dot(y))
	// Output:
	// {0 0 1}
	// 0
}

func ExampleTranslate() {
	// Matrices compose right to left: scale first, then move.
	m := lin.Translate(lin.V3(10, 0, 0)).Mul(lin.Scale(lin.V3(2, 2, 2)))
	fmt.Println(m.MulPoint(lin.V3(1, 1, 1)))
	// Output:
	// {12 2 2}
}

func ExampleAxisAngle() {
	q := lin.AxisAngle(lin.V3(0, 1, 0), lin.Radians(90))
	p := q.Rotate(lin.V3(1, 0, 0))
	fmt.Printf("%.1f %.1f %.1f\n", p.X, p.Y, p.Z)
	// Output:
	// 0.0 0.0 -1.0
}
