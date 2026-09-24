package anim

import "testing"

// A blend space drives a player every update, so advancing it must not
// allocate once its slices have grown.
func TestBlendAdvanceAllocs(t *testing.T) {
	g := headless(t)
	model, err := g.LoadModel(strideDoc())
	if err != nil {
		t.Fatal(err)
	}
	defer model.Destroy()
	p := model.NewAnimPlayer()
	b := NewBlend(&BlendSpace1D{Parameter: "speed", Clips: []BlendPoint1D{{"short", 0}, {"long", 1}}})
	b.Set("speed", 0.5)
	b.Advance(p, 1.0/60)
	if allocs := testing.AllocsPerRun(100, func() { b.Advance(p, 1.0/60) }); allocs != 0 {
		t.Fatalf("Blend.Advance allocated %v times per update", allocs)
	}
	// Moving between one clip and two keeps to the storage already there.
	b.Set("speed", 1)
	b.Advance(p, 1.0/60)
	b.Set("speed", 0.5)
	if allocs := testing.AllocsPerRun(100, func() { b.Advance(p, 1.0/60) }); allocs != 0 {
		t.Fatalf("Blend.Advance allocated %v times per update after a change", allocs)
	}
}
