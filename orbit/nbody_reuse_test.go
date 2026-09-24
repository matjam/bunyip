package orbit

import "testing"

// refStep is Simulation.Step as it was first written: both halves of the
// kick compute every acceleration on one goroutine.
func refStep(s *Simulation, dt float64) {
	acc := make([]Vec3, len(s.Bodies))
	s.accelRange(acc, 0, len(s.Bodies))
	for i := range s.Bodies {
		b := &s.Bodies[i]
		b.Vel = b.Vel.Add(acc[i].Mul(dt / 2))
		b.Pos = b.Pos.Add(b.Vel.Mul(dt))
	}
	s.accelRange(acc, 0, len(s.Bodies))
	for i := range s.Bodies {
		b := &s.Bodies[i]
		b.Vel = b.Vel.Add(acc[i].Mul(dt / 2))
	}
	s.Time += dt
}

func sameBodies(t *testing.T, step int, got, want []Body) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("step %d: %d bodies, want %d", step, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("step %d body %d: %+v, want %+v", step, i, got[i], want[i])
		}
	}
}

// Reusing the end-of-step accelerations, and computing them across
// goroutines, gives the same bodies bit for bit, including when the
// bodies, G or Softening change between steps.
func TestStepReusesAccelerations(t *testing.T) {
	for _, n := range []int{50, 700} {
		got, want := benchNBody(n), benchNBody(n)
		for step := range 12 {
			switch step {
			case 3: // a body moved by hand
				got.Bodies[5].Pos.X += 0.5
				want.Bodies[5].Pos.X += 0.5
			case 5: // a body added
				b := Body{Pos: V3(3, 4, 5), Vel: V3(0, 1, 0), Mass: 2}
				got.Bodies = append(got.Bodies, b)
				want.Bodies = append(want.Bodies, b)
			case 7: // a mass changed and then the constants
				got.Bodies[1].Mass, want.Bodies[1].Mass = 50, 50
			case 9:
				got.G, want.G = 2, 2
			case 10:
				got.Softening, want.Softening = 0.1, 0.1
			case 11: // a body removed
				got.Bodies = got.Bodies[:len(got.Bodies)-2]
				want.Bodies = want.Bodies[:len(want.Bodies)-2]
			}
			got.Step(1e-4)
			refStep(want, 1e-4)
			sameBodies(t, step, got.Bodies, want.Bodies)
		}
	}
}

func TestAccelerationsParallelMatchesSerial(t *testing.T) {
	s := benchNBody(1000)
	par := make([]Vec3, len(s.Bodies))
	ser := make([]Vec3, len(s.Bodies))
	s.Accelerations(par)
	s.accelRange(ser, 0, len(s.Bodies))
	for i := range par {
		if par[i] != ser[i] {
			t.Fatalf("body %d: %v, want %v", i, par[i], ser[i])
		}
	}
}

// Copies of a Simulation that share its scratch never reuse an
// acceleration computed under another copy's constants.
func TestStepCopiesStayIndependent(t *testing.T) {
	a := benchNBody(40)
	a.Step(1e-4)
	b := *a
	b.Bodies = append([]Body(nil), a.Bodies...)
	ref := *a
	ref.Bodies = append([]Body(nil), a.Bodies...)
	a.G = 3
	a.Step(1e-4) // rewrites the shared scratch under G = 3
	b.Step(1e-4)
	refStep(&ref, 1e-4)
	sameBodies(t, 0, b.Bodies, ref.Bodies)
}
