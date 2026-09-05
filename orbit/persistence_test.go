package orbit

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
)

func registerOrbitPersistence() {
	ecs.Register[Body]("orbit.Body")
	ecs.Register[Kepler]("orbit.Kepler")
	ecs.Register[Settings]("orbit.Settings")
}

func savedOrbitBody(t *testing.T, w *ecs.World, e ecs.Entity, want State) {
	t.Helper()
	b, ok := ecs.Get[Body](w, e)
	if !ok {
		t.Fatal("missing orbit body")
	}
	if b.Pos.Sub(want.Pos).Len() > 1e-12 || b.Vel.Sub(want.Vel).Len() > 1e-12 {
		t.Fatalf("body state = %+v, want %+v", *b, want)
	}
}

func TestKeplerSaveLoadContinuesPhase(t *testing.T) {
	registerOrbitPersistence()
	for _, tc := range []struct {
		name  string
		scale float64
	}{
		{"default clock", 0},
		{"scaled clock", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := ecs.NewWorld()
			ecs.SetResource(w, Settings{G: 1, TimeScale: tc.scale})
			star := w.SpawnWith(Body{Mass: 1}, ecs.Name{Text: "star"})
			planet := w.SpawnWith(Body{Mass: 0.25}, Kepler{Primary: star, Elements: Circular(1)}, ecs.Name{Text: "planet"})
			moon := w.SpawnWith(Body{}, Kepler{Primary: planet, Elements: Circular(0.25), Mu: 0.5}, ecs.Name{Text: "moon"})
			System(w, 1)
			scale := tc.scale
			if scale == 0 {
				scale = 1
			}
			savedOrbitBody(t, w, planet, State{Pos: V3(math.Cos(scale), math.Sin(scale), 0), Vel: V3(-math.Sin(scale), math.Cos(scale), 0)})
			var data bytes.Buffer
			if err := w.Save(&data, ecs.SaveOptions{SkipUnregistered: true}); err != nil {
				t.Fatal(err)
			}
			restored := ecs.NewWorld()
			// Make the restored handles differ from the saved handles.
			restored.Spawn()
			if err := restored.Load(&data); err != nil {
				t.Fatal(err)
			}
			loaded := map[string]ecs.Entity{}
			ecs.NewQuery1[ecs.Name](restored).Each(func(e ecs.Entity, n *ecs.Name) { loaded[n.Text] = e })
			for name, original := range map[string]ecs.Entity{"star": star, "planet": planet, "moon": moon} {
				if !restored.Alive(loaded[name]) || loaded[name] == original {
					t.Fatalf("%s handle = %v, want a live handle distinct from saved %v", name, loaded[name], original)
				}
			}
			for _, link := range []struct{ child, parent string }{{"planet", "star"}, {"moon", "planet"}} {
				k, ok := ecs.Get[Kepler](restored, loaded[link.child])
				if !ok || k.Primary != loaded[link.parent] {
					t.Fatalf("%s primary was not remapped: %+v", link.child, k)
				}
			}
			for _, dt := range []float64{0, 0.25, 0.5} {
				System(w, dt)
				System(restored, dt)
				for name, e := range map[string]ecs.Entity{"planet": planet, "moon": moon} {
					b, _ := ecs.Get[Body](w, e)
					savedOrbitBody(t, restored, loaded[name], State{Pos: b.Pos, Vel: b.Vel})
				}
			}
		})
	}
}

func TestKeplerLegacyJSONStartsAtEpoch(t *testing.T) {
	var k Kepler
	if err := json.Unmarshal([]byte(`{"Primary":0,"Elements":{"SemiMajorAxis":1,"TrueAnomaly":0.75},"Mu":1}`), &k); err != nil {
		t.Fatal(err)
	}
	w := ecs.NewWorld()
	e := w.SpawnWith(Body{}, k)
	System(w, 0)
	savedOrbitBody(t, w, e, Circular(1).AtTime(1, 0.75).State(1))
	System(w, 0.25)
	savedOrbitBody(t, w, e, Circular(1).AtTime(1, 1).State(1))
}

func TestKeplerCopiesContinuePhase(t *testing.T) {
	registerOrbitPersistence()
	for _, mode := range []string{"clone", "prefab JSON"} {
		t.Run(mode, func(t *testing.T) {
			w := ecs.NewWorld()
			star := w.SpawnWith(Body{Mass: 1})
			e := w.SpawnWith(Body{}, Kepler{Primary: star, Elements: Circular(1), Mu: 1})
			System(w, 1)
			var copied ecs.Entity
			if mode == "clone" {
				copied = ecs.Clone(w, e)
			} else {
				data, err := json.Marshal(ecs.PrefabOf(w, e))
				if err != nil {
					t.Fatal(err)
				}
				p, err := ecs.ParsePrefab(data)
				if err != nil {
					t.Fatal(err)
				}
				copied = p.Spawn(w)
			}
			k, ok := ecs.Get[Kepler](w, copied)
			if !ok || k.Primary != star {
				t.Fatalf("copy lost external primary: %+v", k)
			}
			for _, dt := range []float64{0, 0.25} {
				System(w, dt)
				b, _ := ecs.Get[Body](w, e)
				savedOrbitBody(t, w, copied, State{Pos: b.Pos, Vel: b.Vel})
			}
		})
	}
}
