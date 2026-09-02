package particle

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// run advances a system by steps of dt for seconds.
func run(s *System, seconds, dt float64) {
	for t := 0.0; t < seconds-dt/2; t += dt {
		s.Update(dt)
	}
}

func TestRateMatchesOverTime(t *testing.T) {
	cases := []struct {
		name string
		rate float32
		dt   float64
		want int
	}{
		{"100 a second at 60Hz", 100, 1.0 / 60, 100},
		{"7 a second at 30Hz", 7, 1.0 / 30, 7},
		{"fractional rate", 0.5, 1.0 / 60, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := New(Emitter{Rate: c.rate, Lifetime: Range{Min: 100}})
			run(s, 1, c.dt)
			if got := s.Alive(); got < c.want-1 || got > c.want+1 {
				t.Fatalf("alive after 1s = %d, want about %d", got, c.want)
			}
		})
	}
	// Half a particle a second carries its fraction across updates.
	s := New(Emitter{Rate: 0.5, Lifetime: Range{Min: 100}})
	run(s, 4.5, 1.0/60)
	if got := s.Alive(); got != 2 {
		t.Fatalf("alive after 4.5s at 0.5/s = %d, want 2", got)
	}
}

func TestLifetimeKills(t *testing.T) {
	s := New(Emitter{Burst: 10, Lifetime: Range{Min: 0.5}})
	if s.Alive() != 10 {
		t.Fatalf("alive after burst = %d", s.Alive())
	}
	run(s, 0.4, 1.0/60)
	if s.Alive() != 10 {
		t.Fatalf("alive at 0.4s = %d, want 10", s.Alive())
	}
	run(s, 0.2, 1.0/60)
	if s.Alive() != 0 {
		t.Fatalf("alive at 0.6s = %d, want 0", s.Alive())
	}
}

func TestBurstAndMax(t *testing.T) {
	s := New(Emitter{Max: 25, Lifetime: Range{Min: 10}})
	s.Burst(7)
	if s.Alive() != 7 {
		t.Fatalf("alive after Burst(7) = %d", s.Alive())
	}
	s.Burst(100)
	if s.Alive() != 25 {
		t.Fatalf("alive past Max = %d, want 25", s.Alive())
	}
	if cap(s.live) != 25 {
		t.Fatalf("particle slice cap = %d, want 25", cap(s.live))
	}
	s.SetEmitter(Emitter{Rate: 1000, Max: 30, Lifetime: Range{Min: 10}})
	run(s, 1, 1.0/60)
	if s.Alive() != 30 {
		t.Fatalf("alive after raising Max = %d, want 30", s.Alive())
	}
	s.Clear()
	if s.Alive() != 0 {
		t.Fatal("Clear left particles")
	}
}

func TestWorldSpaceKeepsPositions(t *testing.T) {
	for _, world := range []bool{true, false} {
		e := Emitter{Burst: 1, Lifetime: Range{Min: 10}, WorldSpace: world}
		e.Position = lin.V2(10, 20)
		s := New(e)
		p0 := s.Particles()[0].Pos
		s.SetPosition(lin.V2(50, 60))
		s.Update(1.0 / 60)
		p1 := s.Particles()[0].Pos
		if p0 != p1 {
			t.Fatalf("world=%v: stored position moved from %v to %v", world, p0, p1)
		}
		if world && p0 != lin.V2(10, 20) {
			t.Fatalf("world particle born at %v, want the system position", p0)
		}
		if !world && p0 != (lin.Vec2{}) {
			t.Fatalf("local particle born at %v, want the origin", p0)
		}
	}
}

func TestCurves(t *testing.T) {
	cases := []struct {
		name  string
		curve Curve
		t     float32
		want  float32
	}{
		{"empty is one", nil, 0.5, 1},
		{"constant", Constant(3), 0.9, 3},
		{"linear start", Linear(2, 4), 0, 2},
		{"linear middle", Linear(2, 4), 0.5, 3},
		{"linear end", Linear(2, 4), 1, 4},
		{"keys between", Keys(0, 0, 0.2, 1, 1, 0), 0.1, 0.5},
		{"keys after peak", Keys(0, 0, 0.2, 1, 1, 0), 0.6, 0.5},
		{"held before first", Keys(0.5, 2, 1, 4), 0.1, 2},
		{"held after last", Keys(0, 2, 0.5, 4), 0.9, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.curve.At(c.t); math.Abs(float64(got-c.want)) > 1e-5 {
				t.Fatalf("At(%v) = %v, want %v", c.t, got, c.want)
			}
		})
	}
	e := Emitter{ColorOverLife: []ColorKey{{0, gfx.Color{R: 1, A: 1}}, {1, gfx.Color{B: 1, A: 1}}}}
	if got := e.colorAt(0.5); got != (gfx.Color{R: 0.5, B: 0.5, A: 1}) {
		t.Fatalf("colour at 0.5 = %v", got)
	}
	e = Emitter{Color: gfx.White, ColorEnd: gfx.Color{A: 1}}
	if got := e.colorAt(1); got != (gfx.Color{A: 1}) {
		t.Fatalf("colour at end = %v", got)
	}
}

func TestFinishedAfterStop(t *testing.T) {
	s := New(Emitter{Rate: 60, Lifetime: Range{Min: 0.5}})
	run(s, 0.5, 1.0/60)
	if !s.Emitting() || s.Finished() {
		t.Fatal("running system reports finished")
	}
	s.Stop()
	if s.Emitting() {
		t.Fatal("still emitting after Stop")
	}
	if s.Finished() {
		t.Fatal("finished while particles live")
	}
	run(s, 0.6, 1.0/60)
	if !s.Finished() {
		t.Fatalf("not finished after particles died, %d alive", s.Alive())
	}
	// A burst-only effect finishes when its burst has died.
	b := New(Emitter{Burst: 5, Lifetime: Range{Min: 0.1}})
	if b.Finished() {
		t.Fatal("burst finished at once")
	}
	run(b, 0.2, 1.0/60)
	if !b.Finished() {
		t.Fatal("burst not finished after its lifetime")
	}
}

func TestDeterminism(t *testing.T) {
	e := Confetti()
	e.Rate = 30
	e.Seed = 9
	a, b := New(e), New(e)
	run(a, 1, 1.0/60)
	run(b, 1, 1.0/60)
	if a.Alive() == 0 || a.Alive() != b.Alive() {
		t.Fatalf("alive %d vs %d", a.Alive(), b.Alive())
	}
	for i := range a.Particles() {
		if a.Particles()[i] != b.Particles()[i] {
			t.Fatalf("particle %d differs: %+v vs %+v", i, a.Particles()[i], b.Particles()[i])
		}
	}
	e.Seed = 10
	c := New(e)
	run(c, 1, 1.0/60)
	if c.Particles()[0] == a.Particles()[0] {
		t.Fatal("different seeds gave the same particle")
	}
}

func TestMotion(t *testing.T) {
	e := Emitter{Burst: 1, Lifetime: Range{Min: 10}, Speed: Range{Min: 10}, Acceleration: lin.V2(0, 100)}
	s := New(e)
	run(s, 1, 1.0/100)
	p := s.Particles()[0]
	if math.Abs(float64(p.Pos.X-10)) > 0.01 {
		t.Fatalf("x after 1s at 10/s = %v", p.Pos.X)
	}
	// Semi-implicit Euler lands a little past the exact 50.
	if p.Pos.Y < 50 || p.Pos.Y > 51 {
		t.Fatalf("y after 1s under 100 gravity = %v", p.Pos.Y)
	}
	if math.Abs(float64(p.Vel.Y-100)) > 0.01 {
		t.Fatalf("vy after 1s = %v", p.Vel.Y)
	}
}

func TestShapes(t *testing.T) {
	cases := []struct {
		name  string
		shape Shape
		check func(p lin.Vec2) bool
	}{
		{"point", Point(), func(p lin.Vec2) bool { return p == (lin.Vec2{}) }},
		{"circle", Circle(5), func(p lin.Vec2) bool { return p.Len() <= 5.001 }},
		{"ring", Ring(5), func(p lin.Vec2) bool { return math.Abs(float64(p.Len()-5)) < 0.001 }},
		{"rect", Rect(4, 2), func(p lin.Vec2) bool { return math.Abs(float64(p.X)) <= 2 && math.Abs(float64(p.Y)) <= 1 }},
		{"rect edge", Shape{Kind: ShapeRect, W: 4, H: 2, Edge: true}, func(p lin.Vec2) bool {
			onX := math.Abs(math.Abs(float64(p.X))-2) < 0.001 && math.Abs(float64(p.Y)) <= 1.001
			onY := math.Abs(math.Abs(float64(p.Y))-1) < 0.001 && math.Abs(float64(p.X)) <= 2.001
			return onX || onY
		}},
		{"line", Line(lin.V2(10, 0)), func(p lin.Vec2) bool { return p.Y == 0 && p.X >= 0 && p.X <= 10 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := New(Emitter{Burst: 200, Lifetime: Range{Min: 10}, Shape: c.shape})
			for _, p := range s.Particles() {
				if !c.check(p.Pos) {
					t.Fatalf("particle born at %v outside the shape", p.Pos)
				}
			}
		})
	}
}
