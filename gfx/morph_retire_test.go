package gfx

import (
	"testing"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/lin"
)

// Exercise the real slot fence and retire ring even when no draw uses the
// model: a deferred destructor must retain the resource it will free.
func TestMorphRetiresAtSlotReuse(t *testing.T) {
	g := newHeadless(t, 32, 32)
	m, err := g.LoadModel(morphGridDoc(1))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Destroy()
	buf := m.morphBuf.buf
	beginMorphRetireFrame(t, g)
	slot := g.frame.Slot
	before := g.r.Device.Waits()
	m.Destroy()
	m.Destroy()
	if g.r.Device.Waits() != before {
		t.Error("destroying a morph model inside a frame waited for the device")
	}
	if buf.Handle == 0 {
		t.Fatal("morph buffer freed before its frame finished")
	}
	endMorphRetireFrame(t, g)
	for i := 1; i <= render.FramesInFlight; i++ {
		beginMorphRetireFrame(t, g)
		if retired := buf.Handle == 0; retired != (g.frame.Slot == slot) {
			t.Errorf("frame %d: retired = %v, slot = %d, retiring slot = %d", i, retired, g.frame.Slot, slot)
		}
		endMorphRetireFrame(t, g)
	}
}

func TestMorphDestroyPreservesDraws(t *testing.T) {
	for _, when := range []string{"queued", "submitted", "outside frame"} {
		t.Run(when, func(t *testing.T) {
			g := newHeadless(t, 64, 64)
			g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
			m, err := g.LoadModel(morphGridDoc(1))
			if err != nil {
				t.Fatal(err)
			}
			defer m.Destroy()
			want := morphScene(t, g, m, []float32{1})
			buf := m.morphBuf.buf
			beginMorphRetireFrame(t, g)
			g.SetCamera(Camera{Position: lin.V3(0, 1.6, 2.4), Target: lin.V3(0, 0.2, 0)})
			g.SetLight(Light{Direction: lin.V3(-0.4, -1, -0.3), Color: Color{2, 2, 2, 1},
				Sky: Sky{Zenith: Color{0.2, 0.25, 0.35, 1}, Ground: Color{0.1, 0.1, 0.1, 1}}})
			g.DrawModel(m, lin.Identity())
			if when == "queued" {
				m.Destroy()
				m.Destroy()
				got, err := g.end(true)
				if err != nil {
					t.Fatal(err)
				}
				if diff := imageDiff(want, got); diff != 0 {
					t.Errorf("destroying the queued model changed %d pixels", diff)
				}
			} else {
				// Submit without capture, so no readback fence has waited for
				// the draw when Destroy is called.
				endMorphRetireFrame(t, g)
				if when == "submitted" {
					beginMorphRetireFrame(t, g)
				}
				before := g.r.Device.Waits()
				m.Destroy()
				m.Destroy()
				if when == "submitted" {
					if g.r.Device.Waits() != before || buf.Handle == 0 {
						t.Error("in-frame destruction must defer without waiting")
					}
					endMorphRetireFrame(t, g)
				} else if g.r.Device.Waits() == before || buf.Handle != 0 {
					t.Error("outside-frame destruction must wait and free the buffer")
				}
			}
			for range render.FramesInFlight {
				beginMorphRetireFrame(t, g)
				endMorphRetireFrame(t, g)
			}
			if buf.Handle != 0 {
				t.Error("morph buffer remains allocated after retirement")
			}
			m.Destroy()
		})
	}
}

func beginMorphRetireFrame(t *testing.T, g *Graphics) {
	t.Helper()
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatalf("begin frame: ok=%v, err=%v", ok, err)
	}
}

func endMorphRetireFrame(t *testing.T, g *Graphics) {
	t.Helper()
	if _, err := g.end(false); err != nil {
		t.Fatal(err)
	}
}
