package console

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/ui"
)

// drawPhysics counts the world's bodies, contacts and joints, edits the
// solver settings, and pauses or single-steps the simulation.
func (c *Console) drawPhysics(f Frame, area ui.Rect) {
	u := c.ui
	w, _ := c.world()
	if w == nil {
		u.Label("no world attached: call Attach(name, world) on the console")
		return
	}
	p := &c.panels
	for len(p.sim) < len(c.attach.worlds) {
		p.sim = append(p.sim, simState{})
	}
	sim := &p.sim[p.world]
	s3 := ecs.Resource[phys.Settings3](w)
	s2 := ecs.Resource[phys.Settings2](w)
	u.ScrollArea("physics", area, c.rowsH(22), func() {
		if names := c.Worlds(); len(names) > 1 {
			u.Dropdown("world", &p.world, names)
		}
		bodies3, asleep3 := 0, 0
		ecs.Each(w, func(_ ecs.Entity, b *phys.Body3) {
			bodies3++
			if b.Asleep() {
				asleep3++
			}
		})
		bodies2, asleep2 := 0, 0
		ecs.Each(w, func(_ ecs.Entity, b *phys.Body2) {
			bodies2++
			if b.Asleep() {
				asleep2++
			}
		})
		contacts := len(ecs.Events[phys.Collision3](w)) + len(ecs.Events[phys.Collision2](w))
		triggers := len(ecs.Events[phys.Trigger3](w)) + len(ecs.Events[phys.Trigger2](w))
		u.Label(fmt.Sprintf("3D: %d bodies (%d asleep), %d colliders", bodies3, asleep3, ecs.Count[phys.Collider3](w)))
		u.Label(fmt.Sprintf("2D: %d bodies (%d asleep), %d colliders", bodies2, asleep2, ecs.Count[phys.Collider2](w)))
		u.Label(fmt.Sprintf("%d contacts and %d triggers in the last update, %d joints", contacts, triggers, jointCount(w)))
		u.Separator()
		u.Row(3, func() {
			paused := sim.paused
			if u.Checkbox("pause the world", &paused) {
				c.setPaused(w, sim, paused)
			}
			if u.Button("step") {
				c.step(w, sim)
			}
			u.Checkbox("draw colliders", &sim.draw)
		})
		u.Label("pausing turns every system in the world off, physics included")
		u.Separator()
		switch {
		case s3 != nil:
			u.Label("3D settings")
			u.Slider("gravity x", &s3.Gravity.X, -30, 30)
			u.Slider("gravity y", &s3.Gravity.Y, -30, 30)
			u.Slider("gravity z", &s3.Gravity.Z, -30, 30)
			u.IntSlider("substeps", &s3.Substeps, 0, 16)
			u.IntSlider("iterations", &s3.Iterations, 0, 32)
			u.Slider("sleep time", &s3.SleepTime, 0, 3)
			u.Slider("sleep threshold", &s3.SleepThreshold, 0, 1)
		case s2 != nil:
			u.Label("2D settings")
			u.Slider("gravity x", &s2.Gravity.X, -30, 30)
			u.Slider("gravity y", &s2.Gravity.Y, -30, 30)
			u.IntSlider("substeps", &s2.Substeps, 0, 16)
			u.IntSlider("iterations", &s2.Iterations, 0, 32)
			u.Slider("sleep time", &s2.SleepTime, 0, 3)
			u.Slider("sleep threshold", &s2.SleepThreshold, 0, 1)
		default:
			u.Label("no phys.Settings2 or Settings3 resource on this world")
		}
	})
}

// jointCount counts every kind of joint in a world.
func jointCount(w *ecs.World) int {
	return ecs.Count[phys.DistanceJoint3](w) + ecs.Count[phys.HingeJoint3](w) +
		ecs.Count[phys.BallJoint3](w) + ecs.Count[phys.SpringJoint3](w) + ecs.Count[phys.FixedJoint3](w) +
		ecs.Count[phys.DistanceJoint2](w) + ecs.Count[phys.RevoluteJoint2](w) +
		ecs.Count[phys.SpringJoint2](w) + ecs.Count[phys.FixedJoint2](w)
}

// setPaused turns every system in the world off, remembering which were
// already off so unpausing puts them back as they were.
func (c *Console) setPaused(w *ecs.World, sim *simState, paused bool) {
	if paused == sim.paused {
		return
	}
	sim.paused = paused
	if paused {
		sim.held = sim.held[:0]
		for _, s := range w.Stats() {
			if w.SystemEnabled(s.Name) {
				sim.held = append(sim.held, s.Name)
				w.SetSystemEnabled(s.Name, false)
			}
		}
		return
	}
	for _, name := range sim.held {
		w.SetSystemEnabled(name, true)
	}
	sim.held, sim.stepping = sim.held[:0], false
}

// step runs the paused world for one update, and pauses it again.
func (c *Console) step(w *ecs.World, sim *simState) {
	if !sim.paused || sim.stepping {
		return
	}
	for _, name := range sim.held {
		w.SetSystemEnabled(name, true)
	}
	sim.stepping, sim.stepFrom = true, w.Updates()
}

// finishStep turns the systems off again once the world has run the one
// update a step asked for. Draw calls it every frame.
func (c *Console) finishStep() {
	for i := range c.panels.sim {
		sim := &c.panels.sim[i]
		if !sim.stepping || i >= len(c.attach.worlds) {
			continue
		}
		w := c.attach.worlds[i].world
		if w.Updates() > sim.stepFrom {
			for _, name := range sim.held {
				w.SetSystemEnabled(name, false)
			}
			sim.stepping = false
		}
	}
}

// drawColliders outlines the colliders of every world whose panel asked
// for them, as debug lines over the scene.
func (c *Console) drawColliders(f Frame) {
	for i := range c.panels.sim {
		if !c.panels.sim[i].draw || i >= len(c.attach.worlds) {
			continue
		}
		phys.DrawColliders3(f.Gfx, c.attach.worlds[i].world)
		phys.DrawColliders2(f.Gfx, c.attach.worlds[i].world)
	}
}

// drawAudio lists the playing voices, the buses and the listener.
func (c *Console) drawAudio(f Frame, area ui.Rect) {
	u := c.ui
	if f.Audio == nil {
		u.Label("no mixer")
		return
	}
	m := f.Audio
	voices := m.Voices()
	buses := m.Buses()
	u.ScrollArea("audio", area, c.rowsH(8+len(buses)*2+len(voices)+2), func() {
		l := m.Listener()
		u.Label(fmt.Sprintf("%d voices at %d Hz, master %s", len(voices), m.Rate(), pausedText(m.Paused())))
		u.Label(fmt.Sprintf("listener at %s facing %s", vecText(l.Position), vecText(l.Forward)))
		u.Label("the mixer does not measure its own load")
		u.Separator()
		u.Label("buses")
		for _, b := range buses {
			vol := b.Volume()
			if u.Slider(b.Name()+" gain", &vol, 0, 2) {
				b.SetVolume(vol)
			}
			u.Row(3, func() {
				mute := b.Muted()
				if u.Checkbox(b.Name()+" mute", &mute) {
					b.SetMute(mute)
				}
				solo := b.Soloed()
				if u.Checkbox(b.Name()+" solo", &solo) {
					b.SetSolo(solo)
				}
				paused := b.Paused()
				if u.Checkbox(b.Name()+" pause", &paused) {
					b.SetPaused(paused)
				}
			})
		}
		u.Separator()
		u.Label("voices")
		if len(voices) == 0 {
			u.Label("  nothing playing")
			return
		}
		u.Table([]string{"source", "bus", "gain", "pitch", "where"}, []float32{2, 1.4, 1, 1, 2.4}, len(voices), func(row, col int) {
			v := voices[row]
			switch col {
			case 0:
				u.Cell(voiceSource(v))
			case 1:
				u.Cell(orDash(v.Bus))
			case 2:
				u.Cell(strconv.FormatFloat(float64(v.Volume), 'f', 2, 32))
			case 3:
				u.Cell(strconv.FormatFloat(float64(v.Pitch), 'f', 2, 32))
			case 4:
				if v.Positional {
					u.Cell(vecText(v.Position))
				} else {
					u.Cell("pan " + strconv.FormatFloat(float64(v.Pan), 'f', 2, 32))
				}
			}
		})
	})
}

// voiceSource names what a voice is playing and how far into it.
func voiceSource(v audio.VoiceInfo) string {
	kind := "sound"
	if v.Stream {
		kind = "stream"
	}
	s := fmt.Sprintf("%s %.1fs", kind, v.Seconds)
	if v.Loop {
		s += " loop"
	}
	if v.Paused {
		s += " paused"
	}
	if v.Muted {
		s += " muted"
	}
	return s
}

func pausedText(paused bool) string {
	if paused {
		return "paused"
	}
	return "running"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func vecText(v lin.Vec3) string {
	return fmt.Sprintf("%.1f %.1f %.1f", v.X, v.Y, v.Z)
}

// drawInput shows what the keyboard, pointer and gamepads are doing, and
// the value of every action a game attached.
func (c *Console) drawInput(f Frame, area ui.Rect) {
	u := c.ui
	in := f.Input
	pads := 0
	for i := range input.MaxGamepads {
		if g := in.Gamepad(i); g != nil && g.Connected {
			pads++
		}
	}
	actions := 0
	for _, a := range c.attach.actions {
		actions += len(a.actions.Names()) + 1
	}
	u.ScrollArea("input", area, c.rowsH(8+pads*4+actions), func() {
		keys := in.KeysDown()
		names := make([]string, len(keys))
		for i, k := range keys {
			names[i] = k.String()
		}
		u.Label("keys down: " + orDash(strings.Join(names, " ")))
		x, y := in.Mouse()
		dx, dy := in.MouseDelta()
		sx, sy := in.Scroll()
		u.Label(fmt.Sprintf("pointer %.1f, %.1f   moved %.1f, %.1f   scroll %.2f, %.2f", x, y, dx, dy, sx, sy))
		var buttons []string
		for b := range input.MouseButtonCount {
			if in.MouseDown(b) {
				buttons = append(buttons, strconv.Itoa(int(b)))
			}
		}
		u.Label("buttons down: " + orDash(strings.Join(buttons, " ")))
		u.Label("modifiers: " + orDash(in.Mods().String()))
		u.Separator()
		u.Label(fmt.Sprintf("%d gamepads connected", pads))
		for i := range input.MaxGamepads {
			g := in.Gamepad(i)
			if g == nil || !g.Connected {
				continue
			}
			u.Label(fmt.Sprintf("pad %d: %s", i, g.Name))
			var down []string
			for b := range input.GamepadButtonCount {
				if g.Down(b) {
					down = append(down, strconv.Itoa(int(b)))
				}
			}
			u.Label("  buttons: " + orDash(strings.Join(down, " ")))
			var axes []string
			for a := range input.GamepadAxisCount {
				axes = append(axes, strconv.FormatFloat(float64(g.Axis(a)), 'f', 2, 32))
			}
			u.Label("  axes: " + strings.Join(axes, " "))
		}
		if len(c.attach.actions) == 0 {
			u.Separator()
			u.Label("no action map attached: call AttachActions(name, actions)")
			return
		}
		for _, a := range c.attach.actions {
			u.Separator()
			u.Label("actions: " + a.name)
			for _, name := range a.actions.Names() {
				var srcs []string
				for _, s := range a.actions.Bindings(name) {
					srcs = append(srcs, s.String())
				}
				u.Label(fmt.Sprintf("  %s = %.2f   %s", name, a.actions.Value(in, name), strings.Join(srcs, ", ")))
			}
		}
	})
}

// drawServices shows the console's own variables and whatever the game
// attached: lines of text and network links.
func (c *Console) drawServices(f Frame, area ui.Rect) {
	u := c.ui
	vars := c.VarNames()
	links := 0
	for _, l := range c.attach.links {
		links += len(l.links()) + 2
	}
	u.ScrollArea("services", area, c.rowsH(6+len(vars)+len(c.attach.info)+links), func() {
		u.Label("console variables")
		if len(vars) == 0 {
			u.Label("  none registered")
		}
		for _, name := range vars {
			v, _ := c.GetVar(name)
			u.Label("  " + name + " = " + v)
		}
		u.Separator()
		if len(c.attach.info) == 0 {
			u.Label("nothing attached: call AttachInfo(name, text) for a line here")
		}
		for _, i := range c.attach.info {
			u.Label(i.name + ": " + i.text())
		}
		for _, l := range c.attach.links {
			u.Separator()
			rows := l.links()
			u.Label(fmt.Sprintf("%s: %d links", l.name, len(rows)))
			if len(rows) == 0 {
				continue
			}
			u.Table([]string{"peer", "rtt", "loss", "pending"}, []float32{2.4, 1, 1, 1}, len(rows), func(row, col int) {
				r := rows[row]
				switch col {
				case 0:
					peer := r.Peer
					if !r.Connected {
						peer += " (down)"
					}
					u.Cell(peer)
				case 1:
					u.Cell(fmt.Sprintf("%.1f ms", float64(r.RTT.Microseconds())/1000))
				case 2:
					u.Cell(fmt.Sprintf("%.1f%%", r.Loss*100))
				case 3:
					u.Cell(strconv.Itoa(r.Pending))
				}
			})
		}
	})
}
