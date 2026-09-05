package console

import (
	"slices"
	"strings"
	"testing"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/ui"
)

// tabIndex is the position of a tab by name, so a test can open it.
func tabIndex(t testing.TB, name string) int {
	t.Helper()
	i := slices.Index(tabNames, name)
	if i < 0 {
		t.Fatalf("no tab called %q", name)
	}
	return i
}

// openPanels opens the debug window on a tab, in a rectangle tall enough
// that a tab's contents are not scrolled out of reach.
func (r *rig) openPanels(t testing.TB, tab string) {
	t.Helper()
	p := &r.con.panels
	p.open, p.tab = true, tabIndex(t, tab)
	p.rect = ui.Rect{X: 0, Y: 0, W: 620, H: 1600}
	p.sized = true
}

// node finds the last frame's widget with a label, and fails when there
// is none.
func (r *rig) node(t testing.TB, label string) ui.AccessibleNode {
	t.Helper()
	for _, n := range r.con.ui.Accessible() {
		if n.Label == label {
			return n
		}
	}
	var labels []string
	for _, n := range r.con.ui.Accessible() {
		labels = append(labels, n.Label)
	}
	t.Fatalf("no widget labelled %q; the panel has %s", label, strings.Join(labels, ", "))
	return ui.AccessibleNode{}
}

// click presses and releases the left button in the middle of a
// rectangle, with the frames the interface needs: hover is one frame
// behind, so the pointer arrives before the press.
func (r *rig) click(t testing.TB, rect ui.Rect) {
	t.Helper()
	x, y := rect.X+rect.W/2, rect.Y+rect.H/2
	r.in.FeedMouseMove(x, y)
	r.draw(t)
	r.in.FeedMouseButton(input.MouseLeft, true, x, y)
	r.draw(t)
	r.in.FeedMouseButton(input.MouseLeft, false, x, y)
	r.draw(t)
}

// position is a component the entity panel edits.
type position struct {
	Pos     lin.Vec3
	Name    string
	Awake   bool
	Health  int
	hidden  float32
	Nothing []byte
}

// TestEntityPanel lists a world's entities, filters them, shows the
// selected entity's components and writes an edited field back.
func TestEntityPanel(t *testing.T) {
	r := newRig(t, Options{})
	c := r.con
	w := ecs.NewWorld()
	e := w.SpawnWith(position{Pos: lin.V3(1, 2, 3), Name: "hero", Health: 10})
	other := w.SpawnWith(gfx.Transform{})
	c.Attach("world", w)
	r.openPanels(t, "Entities")
	r.drawN(t, 2)

	// Every entity is listed, and the filter narrows the list by
	// component type name.
	if got := c.panels.entities; !slices.Contains(got, e) || !slices.Contains(got, other) {
		t.Fatalf("the panel listed %v, want both %v and %v", got, e, other)
	}
	c.panels.filter = "position"
	r.draw(t)
	if got := c.panels.entities; len(got) != 1 || got[0] != e {
		t.Errorf("filtering by position listed %v, want just %v", got, e)
	}
	c.panels.filter = "nothing matches this"
	r.draw(t)
	if got := c.panels.entities; len(got) != 0 {
		t.Errorf("a filter matching nothing listed %v", got)
	}
	c.panels.filter = ""

	// The selected entity's fields show their live values.
	c.panels.selected = e
	r.drawN(t, 2)
	if n := r.node(t, "position.Pos.X"); n.Value != "1" {
		t.Errorf("the X field shows %q, want 1", n.Value)
	}
	if n := r.node(t, "position.Name"); n.Value != "hero" {
		t.Errorf("the name field shows %q, want hero", n.Value)
	}
	r.node(t, "position.Awake")  // a bool is a checkbox
	r.node(t, "position.Health") // and a whole number a field of its own

	// Editing a float writes the component back to the world.
	field := r.node(t, "position.Pos.X")
	r.click(t, field.Rect)
	r.in.press(input.KeyEnd)
	r.draw(t)
	r.in.press(input.KeyBackspace)
	r.draw(t)
	r.in.typed("42")
	r.drawN(t, 2)
	p, ok := w.Get[position](e)
	if !ok {
		t.Fatal("the entity lost its component")
	}
	if p.Pos.X != 42 {
		t.Errorf("Pos.X = %v, want 42 after editing the field", p.Pos.X)
	}
	if p.Pos.Y != 2 || p.Name != "hero" || p.Health != 10 {
		t.Errorf("editing one field changed the rest: %+v", *p)
	}

	// Despawning removes the entity and clears the selection.
	r.click(t, r.node(t, "despawn").Rect)
	if w.Alive(e) {
		t.Error("the despawn button left the entity alive")
	}
	if c.panels.selected != ecs.None {
		t.Error("the selection survived the despawn")
	}
}

// TestGraphicsPanel edits the live post-processing settings.
func TestGraphicsPanel(t *testing.T) {
	r := newRig(t, Options{})
	r.g.SetPost(gfx.DefaultPost())
	r.openPanels(t, "Graphics")
	r.drawN(t, 2)
	// Dragging the exposure slider to the middle of its 0..4 range.
	slider := r.node(t, "exposure")
	x, y := slider.Rect.X+slider.Rect.W/2, slider.Rect.Y+slider.Rect.H/2
	r.in.FeedMouseMove(x, y)
	r.draw(t)
	r.in.FeedMouseButton(input.MouseLeft, true, x, y)
	r.draw(t)
	if got := r.g.Post().Exposure; got < 1.9 || got > 2.1 {
		t.Errorf("exposure = %v, want about 2 after dragging the slider to the middle", got)
	}
	r.in.FeedMouseButton(input.MouseLeft, false, x, y)
	r.draw(t)
	// The checkbox turns anti-aliasing off.
	r.click(t, r.node(t, "no anti-alias").Rect)
	if !r.g.Post().NoAntiAlias {
		t.Error("the no anti-alias checkbox did not reach the settings")
	}
	// And the reset button puts every setting back.
	r.click(t, r.node(t, "reset post settings").Rect)
	if got := r.g.Post(); got != gfx.DefaultPost() {
		t.Errorf("after the reset the settings are %+v", got)
	}
	// A font and a mesh made for the test show in the resource list.
	if len(r.g.Resources()) == 0 {
		t.Error("the console's own font is not in the resource list")
	}
}

// TestPhysicsPanel pauses a world, steps it once and counts its bodies.
func TestPhysicsPanel(t *testing.T) {
	r := newRig(t, Options{})
	c := r.con
	w := ecs.NewWorld()
	w.SetResource(phys.Settings3{Gravity: lin.V3(0, -10, 0), Substeps: 2, Iterations: 4})
	w.SpawnWith(gfx.At(0, 10, 0), phys.Dynamic3(1), phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	w.AddSystem("physics", phys.System3)
	c.Attach("world", w)
	r.openPanels(t, "Physics")
	r.drawN(t, 2)

	pause := r.node(t, "pause the world")
	r.click(t, pause.Rect)
	if !c.panels.sim[0].paused {
		t.Fatal("the pause checkbox did not pause the world")
	}
	if w.SystemEnabled("physics") {
		t.Error("pausing left the physics system running")
	}
	before := w.Updates()
	w.Update(1.0 / 60)
	if got := velocityOf(t, w); got != 0 {
		t.Errorf("a paused world moved: velocity %v", got)
	}
	// A step runs exactly one update and pauses again.
	r.click(t, r.node(t, "step").Rect)
	if !w.SystemEnabled("physics") {
		t.Fatal("the step did not turn the system on")
	}
	w.Update(1.0 / 60)
	r.draw(t)
	if w.SystemEnabled("physics") {
		t.Error("the system stayed on after its one step")
	}
	if got := velocityOf(t, w); got == 0 {
		t.Error("the step did not advance the simulation")
	}
	if w.Updates() != before+2 {
		t.Errorf("the world ran %d updates, want 2", w.Updates()-before)
	}
	// Unpausing puts the system back.
	r.click(t, r.node(t, "pause the world").Rect)
	if !w.SystemEnabled("physics") {
		t.Error("unpausing left the system off")
	}
	// The collider drawing toggle queues debug lines rather than failing.
	r.click(t, r.node(t, "draw colliders").Rect)
	if !c.panels.sim[0].draw {
		t.Fatal("the collider toggle did not stick")
	}
	r.draw(t)
}

// velocityOf is the downward speed of the world's one body.
func velocityOf(t testing.TB, w *ecs.World) float32 {
	t.Helper()
	var v float32
	w.Each(func(_ ecs.Entity, b *phys.Body3) { v = b.Vel.Y })
	return v
}

// TestOtherPanels draws every remaining tab with something attached, so
// a panel that cannot lay itself out fails here rather than in a game.
func TestOtherPanels(t *testing.T) {
	r := newRig(t, Options{})
	c := r.con
	mix := audio.NewMixer(48000)
	sound, err := mix.NewSound(audio.Sine(440, 1, 48000))
	if err != nil {
		t.Fatal(err)
	}
	mix.Play(sound, audio.PlayOptions{Bus: mix.Music(), Loop: true})
	mix.Play(sound, audio.PlayOptions{Positional: true, Position: lin.V3(1, 0, 2)})
	r.frame.Audio = mix
	actions := input.NewActions()
	actions.Bind("jump", input.KeySource(input.KeySpace))
	c.AttachActions("player", actions)
	c.AttachInfo("locale", func() string { return "en-AU" })
	c.AttachLinks("server", func() []Link {
		return []Link{{Peer: "10.0.0.1:9000", Loss: 0.02, Pending: 3, Connected: true}}
	})
	var speed float32 = 2
	c.Float("player.speed", &speed, "how fast the player runs")
	for _, tab := range []string{"Engine", "Audio", "Input", "Services"} {
		r.openPanels(t, tab)
		r.drawN(t, 2)
	}
	// The services tab shows what was attached and the console's own
	// variables; the audio tab the buses.
	r.openPanels(t, "Services")
	r.drawN(t, 2)
	r.node(t, "locale: en-AU")
	r.node(t, "  player.speed = 2")
	r.openPanels(t, "Audio")
	r.drawN(t, 2)
	r.node(t, "music gain")
	if n := r.node(t, "music mute"); n.State {
		t.Error("the music bus starts muted")
	}
	r.click(t, r.node(t, "music mute").Rect)
	if !mix.Music().Muted() {
		t.Error("the mute checkbox did not reach the bus")
	}
	if got := mix.Voices(); len(got) != 2 {
		t.Fatalf("the mixer reports %d voices, want 2", len(got))
	}
}
