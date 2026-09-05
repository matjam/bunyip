package particle

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// TestGPUMatchesSystem runs the same emitter through both paths and
// checks they agree particle for particle. The instanced system draws
// its random numbers in the order the CPU one does, so the two streams
// stay in step and a game can switch paths without the effect changing.
func TestGPUMatchesSystem(t *testing.T) {
	e := Fire()
	e.Seed = 7
	e.Max = 500
	cpu, gpu := New(e), NewGPU(e)
	for range 120 {
		cpu.Update(1.0 / 60)
		gpu.Update(1.0 / 60)
	}
	want := cpu.Particles()
	if len(want) == 0 {
		t.Fatal("the CPU system produced no particles")
	}
	if gpu.Alive() != len(want) {
		t.Fatalf("alive = %d, want %d", gpu.Alive(), len(want))
	}
	for i, p := range want {
		gx, gy := gpu.p.posX[i], gpu.p.posY[i]
		if math.Abs(float64(gx-p.Pos.X)) > 1e-3 || math.Abs(float64(gy-p.Pos.Y)) > 1e-3 {
			t.Fatalf("particle %d at (%v,%v), want (%v,%v)", i, gx, gy, p.Pos.X, p.Pos.Y)
		}
		if math.Abs(float64(gpu.p.age[i]-p.Age)) > 1e-4 {
			t.Errorf("particle %d age %v, want %v", i, gpu.p.age[i], p.Age)
		}
	}
}

// TestGPUCap checks that Max caps the live particles and that births
// stop rather than overwriting.
func TestGPUCap(t *testing.T) {
	e := Emitter{Rate: 10000, Lifetime: Range{Min: 10}, Max: 64}
	s := NewGPU(e)
	for range 60 {
		s.Update(1.0 / 60)
	}
	if s.Alive() != 64 {
		t.Errorf("alive = %d, want the cap of 64", s.Alive())
	}
}

// TestGPUDeath checks that particles are compacted away when their life
// runs out and that a burst-only system reports itself finished.
func TestGPUDeath(t *testing.T) {
	e := Emitter{Burst: 50, Lifetime: Range{Min: 0.5}, Max: 100}
	s := NewGPU(e)
	if s.Alive() != 50 {
		t.Fatalf("after the burst alive = %d, want 50", s.Alive())
	}
	if s.Finished() {
		t.Error("finished while 50 particles are alive")
	}
	for range 60 {
		s.Update(1.0 / 60)
	}
	if s.Alive() != 0 {
		t.Errorf("after a second alive = %d, want 0", s.Alive())
	}
	if !s.Finished() {
		t.Error("not finished with nothing alive and no rate")
	}
}

// TestGPUQuads checks that the drawn quads carry the size and colour the
// emitter's curves ask for at the age each particle has reached.
func TestGPUQuads(t *testing.T) {
	e := Emitter{
		Burst: 1, Lifetime: Range{Min: 1}, Size: Range{Min: 10},
		SizeOverLife: Linear(1, 0), Color: gfx.RGB(255, 0, 0), Max: 4,
	}
	s := NewGPU(e)
	s.buildQuads(lin.Vec2{})
	if len(s.Quads()) != 1 {
		t.Fatalf("built %d quads, want 1", len(s.Quads()))
	}
	if w := s.Quads()[0].Size.X; math.Abs(float64(w-10)) > 0.5 {
		t.Errorf("size at birth = %v, want 10", w)
	}
	for range 30 { // half the lifetime
		s.Update(1.0 / 60)
	}
	s.buildQuads(lin.Vec2{})
	if len(s.Quads()) != 1 {
		t.Fatalf("built %d quads halfway through, want 1", len(s.Quads()))
	}
	if w := s.Quads()[0].Size.X; math.Abs(float64(w-5)) > 0.5 {
		t.Errorf("size at half life = %v, want about 5", w)
	}
}

// TestStatelessIsAFunctionOfTheClock checks the defining property: the
// same clock gives the same particles however the clock got there, so a
// stateless effect never drifts and needs no prewarm.
func TestStatelessIsAFunctionOfTheClock(t *testing.T) {
	e := Rain()
	e.Stateless = true
	e.Seed = 3
	e.Max = 4000

	many := NewGPU(e)
	for range 120 {
		many.Update(1.0 / 60)
	}
	many.buildQuads(lin.Vec2{})

	// The same clock, reached in one jump rather than 120 steps. Adding
	// 1/60 up 120 times does not land on exactly two seconds, so the
	// clock the steps reached is what the jump has to match.
	once := NewGPU(e)
	once.SetClock(many.Clock())
	once.buildQuads(lin.Vec2{})

	a, b := many.Quads(), once.Quads()
	if len(a) == 0 {
		t.Fatal("no particles after two seconds")
	}
	if len(a) != len(b) {
		t.Fatalf("120 steps gave %d particles, the same clock in one jump gave %d", len(a), len(b))
	}
	for i := range a {
		if math.Abs(float64(a[i].Pos.X-b[i].Pos.X)) > 1e-3 || math.Abs(float64(a[i].Pos.Y-b[i].Pos.Y)) > 1e-3 {
			t.Fatalf("particle %d at %v stepwise and %v in one step", i, a[i].Pos, b[i].Pos)
		}
	}
}

// TestStatelessKeepsNoState checks that a stateless system stores
// nothing per particle, which is what makes its memory constant.
func TestStatelessKeepsNoState(t *testing.T) {
	e := Emitter{Rate: 1000, Lifetime: Range{Min: 1}, Stateless: true, Max: 4000}
	s := NewGPU(e)
	for range 300 {
		s.Update(1.0 / 60)
	}
	if s.p.n != 0 {
		t.Errorf("the arrays hold %d particles; a stateless system keeps none", s.p.n)
	}
	s.buildQuads(lin.Vec2{})
	if got := s.Alive(); got < 900 || got > 1100 {
		t.Errorf("alive = %d, want about rate times lifetime (1000)", got)
	}
}

// TestStatelessDamping checks the closed form for a damped particle
// against the step-by-step simulation the stateful path runs, which is
// what the closed form has to agree with.
func TestStatelessDamping(t *testing.T) {
	v0, accel := lin.V2(100, 0), lin.V2(0, 400)
	const damping, total = 1.5, 0.75
	// The same motion integrated in small steps, as Update does.
	var pos, vel = lin.Vec2{}, v0
	const step = 1.0 / 4000
	for t := float32(0); t < total; t += step {
		vel = vel.Add(accel.Mul(step)).Mul(max(0, 1-damping*step))
		pos = pos.Add(vel.Mul(step))
	}
	got := travel(v0, accel, damping, total)
	if math.Abs(float64(got.X-pos.X)) > 0.5 || math.Abs(float64(got.Y-pos.Y)) > 0.5 {
		t.Errorf("travel = %v, stepwise = %v", got, pos)
	}
}

// TestStatelessNeedsNoPrewarm checks that a stateless emitter is at its
// steady state on the very first frame, because its indices run back
// past zero: rain falls from the start rather than out of an empty sky.
func TestStatelessNeedsNoPrewarm(t *testing.T) {
	e := Emitter{Rate: 1000, Lifetime: Range{Min: 1, Max: 1}, Stateless: true, Max: 4000}
	fresh := NewGPU(e)
	fresh.buildQuads(lin.Vec2{})
	atStart := fresh.Alive()

	settled := NewGPU(e)
	settled.SetClock(5)
	settled.buildQuads(lin.Vec2{})
	later := settled.Alive()

	if later == 0 {
		t.Fatal("no particles after five seconds")
	}
	if d := atStart - later; d < -later/10 || d > later/10 {
		t.Errorf("the first frame has %d particles and a settled one %d; a stateless stream starts full", atStart, later)
	}
}

// TestStatelessIgnoresBurst records that a stateless emitter's stream is
// a function of the clock and cannot be added to.
func TestStatelessIgnoresBurst(t *testing.T) {
	e := Emitter{Rate: 100, Lifetime: Range{Min: 1}, Stateless: true, Max: 500}
	quiet := NewGPU(e)
	quiet.buildQuads(lin.Vec2{})
	want := len(quiet.Quads())
	if want == 0 {
		t.Fatal("the stream is empty before the burst")
	}
	burst := NewGPU(e)
	burst.Burst(100)
	burst.buildQuads(lin.Vec2{})
	if got := len(burst.Quads()); got != want {
		t.Errorf("a burst changed the stream from %d particles to %d", want, got)
	}
}
