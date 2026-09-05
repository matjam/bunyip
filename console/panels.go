package console

import (
	"fmt"
	"strconv"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/ui"
)

// tabNames are the debug panels, in the order the window shows them.
// The panels command takes one of these names.
var tabNames = []string{"Engine", "Graphics", "Entities", "Physics", "Audio", "Input", "Services"}

// graphSamples is how many frames the engine panel's timing graph keeps.
const graphSamples = 240

// sample is one frame's timing for the graph.
type sample struct{ frame, update, draw, present float32 }

// panelState is everything the debug window remembers between frames.
type panelState struct {
	open  bool
	tab   int
	rect  ui.Rect
	sized bool

	// The engine tab's rolling frame timings, newest last.
	samples  []sample
	peak     float32 // the highest frame time in the samples, for the scale
	maxSteps int     // the most updates the loop ran in one frame

	// The graphics tab keeps no state: it edits the live settings.

	// The entities tab.
	world    int
	filter   string
	selected ecs.Entity
	edits    map[string]*string // each field's text, by entity and field path
	entities []ecs.Entity       // this frame's filtered list, for the selection
	entityAt int

	// The physics tab's pause and single-step, per attached world.
	sim []simState
}

// simState is one world's pause, single-step and collider drawing.
type simState struct {
	paused   bool
	stepping bool
	stepFrom uint64
	held     []string // the systems pausing turned off, to turn on again
	draw     bool     // outline the colliders over the scene
}

// rowsH is the height n stacked widgets take, for a scrolling area that
// has to be told how tall its contents are.
func (c *Console) rowsH(n int) float32 {
	return float32(n) * (c.theme.RowHeight + c.theme.Spacing)
}

// drawPanels draws the debug window and the tab that is showing. It runs
// inside the interface frame Draw opened.
func (c *Console) drawPanels(f Frame) {
	p := &c.panels
	if !p.sized {
		// Wide enough for a table, tall enough for a list and what is
		// under it, and out of the way in the top right corner.
		w := min(max(f.Width*0.46, 380), f.Width-16)
		h := min(max(f.Height*0.8, 380), f.Height-16)
		p.rect = ui.Rect{X: f.Width - w - 8, Y: 8, W: w, H: h}
		p.sized = true
	}
	u := c.ui
	u.Window("bunyip debug", &p.rect, func() {
		u.Tabs(tabNames, &p.tab)
		area := c.tabArea(f)
		switch tabNames[p.tab] {
		case "Engine":
			c.drawEngine(f, area)
		case "Graphics":
			c.drawGraphics(f, area)
		case "Entities":
			c.drawEntities(f, area)
		case "Physics":
			c.drawPhysics(f, area)
		case "Audio":
			c.drawAudio(f, area)
		case "Input":
			c.drawInput(f, area)
		case "Services":
			c.drawServices(f, area)
		}
	})
}

// tabArea is the rectangle inside the window below the title and the
// tabs, where a tab lays its contents out.
func (c *Console) tabArea(f Frame) ui.Rect {
	th := c.theme
	_, titleH := th.Font.Measure("bunyip debug", gfx.TextOptions{})
	top := c.panels.rect.Y + th.Padding + titleH + th.Spacing + th.RowHeight + th.Spacing
	return ui.Rect{
		X: c.panels.rect.X + th.Padding,
		Y: top,
		W: c.panels.rect.W - 2*th.Padding,
		H: max(c.panels.rect.Y+c.panels.rect.H-th.Padding-top, th.RowHeight),
	}
}

// sampleFrame adds this frame's timings to the graph. Draw calls it
// every frame, so the graph is full the moment the panel opens.
func (c *Console) sampleFrame(s Stats) {
	p := &c.panels
	p.samples = append(p.samples, sample{
		frame:   float32(s.FrameMS),
		update:  float32(s.UpdateMS),
		draw:    float32(s.DrawMS),
		present: float32(s.PresentMS),
	})
	if n := len(p.samples) - graphSamples; n > 0 {
		p.samples = append(p.samples[:0], p.samples[n:]...)
	}
	p.maxSteps = max(p.maxSteps, s.Updates)
	p.peak = 0
	for _, sm := range p.samples {
		p.peak = max(p.peak, sm.frame)
	}
}

// drawEngine shows the frame timing graph, the loop's catch-up and the
// draw counts.
func (c *Console) drawEngine(f Frame, area ui.Rect) {
	const graphH = 72
	c.drawGraph(f, ui.Rect{X: area.X, Y: area.Y, W: area.W, H: graphH})
	s := f.Stats
	body := ui.Rect{X: area.X, Y: area.Y + graphH + c.theme.Spacing, W: area.W, H: max(area.H-graphH-c.theme.Spacing, c.theme.RowHeight)}
	u := c.ui
	gs := gfx.FrameStats{}
	if f.Gfx != nil {
		gs = f.Gfx.Stats()
	}
	rows := 10 + len(s.Scopes)
	u.ScrollArea("engine", body, c.rowsH(rows), func() {
		u.Label(fmt.Sprintf("%.0f fps   %.2f ms/frame   frame %d", s.FPS, s.FrameMS, f.FrameCount))
		u.Label(fmt.Sprintf("update %.2f ms x%d   draw %.2f ms   present %.2f ms", s.UpdateMS, s.Updates, s.DrawMS, s.PresentMS))
		u.Label(fmt.Sprintf("updates this frame %d, most seen %d", s.Updates, c.panels.maxSteps))
		if f.TimeScale != nil {
			u.Label(fmt.Sprintf("time %.1f s, timescale %g", f.Time, f.TimeScale()))
		} else {
			u.Label(fmt.Sprintf("time %.1f s", f.Time))
		}
		u.Separator()
		u.Label(fmt.Sprintf("2D: %d draws, %d vertices, %d culled", gs.Draws2D, gs.Vertices2D, gs.Culled2D))
		u.Label(fmt.Sprintf("3D: %d draws, %d instances, %d culled", gs.Draws3D, gs.Instances, gs.Culled))
		total := gs.Draws2D + gs.Draws3D
		if s.DrawBudget > 0 {
			u.Progress(fmt.Sprintf("draws %d of budget %d", total, s.DrawBudget), float32(total)/float32(s.DrawBudget))
			if total > s.DrawBudget {
				u.Label("OVER DRAW BUDGET: batching is breaking somewhere")
			}
		} else {
			u.Label(fmt.Sprintf("%d draw calls, no budget set (Config.DrawBudget)", total))
		}
		if len(s.Scopes) > 0 {
			u.Separator()
			for _, sc := range s.Scopes {
				u.Label(fmt.Sprintf("  %s   %.2f ms", sc.Name, sc.MS))
			}
		}
	})
}

// drawGraph plots the frame times, each frame one column split into the
// time spent updating, drawing and presenting.
func (c *Console) drawGraph(f Frame, r ui.Rect) {
	g, th := f.Gfx, c.theme
	g.FillRect(r.X, r.Y, r.W, r.H, th.Field)
	p := &c.panels
	scale := max(p.peak, 20)
	// The 16.7 ms line: a frame that stays under it holds 60 per second.
	y60 := r.Y + r.H - r.H*(1000.0/60)/scale
	if y60 > r.Y {
		g.FillRect(r.X, y60, r.W, 1, th.TextDim.WithAlpha(0.5))
	}
	if len(p.samples) == 0 {
		return
	}
	w := r.W / float32(graphSamples)
	for i, s := range p.samples {
		x := r.X + float32(graphSamples-len(p.samples)+i)*w
		y := r.Y + r.H
		bar := func(ms float32, col gfx.Color) {
			h := r.H * ms / scale
			if h <= 0 {
				return
			}
			y -= h
			g.FillRect(x, max(y, r.Y), max(w-1, 1), min(h, r.H), col)
		}
		bar(s.update, gfx.RGB(120, 200, 120))
		bar(s.draw, gfx.RGB(120, 170, 255))
		bar(s.present, gfx.RGB(230, 190, 110))
		rest := s.frame - s.update - s.draw - s.present
		bar(rest, gfx.RGB(90, 90, 110))
	}
	label := fmt.Sprintf("%.1f ms peak   update  draw  present  idle", p.peak)
	g.DrawText(th.Font, label, r.X+4, r.Y+2, th.TextDim)
}

// drawGraphics edits the post-processing settings and lists the GPU
// resources.
func (c *Console) drawGraphics(f Frame, area ui.Rect) {
	u := c.ui
	if f.Gfx == nil {
		u.Label("no graphics context")
		return
	}
	g := f.Gfx
	post := g.Post()
	before := post
	res := g.Resources()
	rows := 16 + min(len(res), 1)
	u.ScrollArea("graphics", area, c.rowsH(rows)+180, func() {
		u.Label("post-processing")
		u.Slider("exposure", &post.Exposure, 0, 4)
		u.Slider("bloom", &post.Bloom, 0, 2)
		u.Slider("bloom threshold", &post.BloomThreshold, 0, 4)
		u.Slider("vignette", &post.Vignette, 0, 1)
		u.Slider("saturation", &post.Saturation, 0, 2)
		u.Slider("contrast", &post.Contrast, 0, 2)
		u.Slider("ambient occlusion", &post.AmbientOcclusion, 0, 1)
		u.Slider("occlusion radius", &post.OcclusionRadius, 0, 4)
		u.Row(2, func() {
			u.Checkbox("show occlusion", &post.ShowOcclusion)
			u.Checkbox("no anti-alias", &post.NoAntiAlias)
		})
		if u.Button("reset post settings") {
			post = gfx.DefaultPost()
		}
		if post != before {
			g.SetPost(post)
		}
		u.Separator()
		gs := g.Stats()
		u.Label(fmt.Sprintf("lights this frame %d of %d, %d dropped", gs.Lights, gfx.MaxLights, gs.LightsDropped))
		var bytes int
		counts := map[gfx.ResourceKind]int{}
		items := make([]string, 0, len(res))
		for _, r := range res {
			bytes += r.Bytes
			counts[r.Kind]++
			items = append(items, resourceLine(r))
		}
		u.Label(fmt.Sprintf("%d live resources, about %s of GPU memory", len(res), bytesText(bytes)))
		u.Label(fmt.Sprintf("%d textures, %d meshes, %d models, %d fonts, %d render textures, %d environments",
			counts[gfx.ResourceTexture], counts[gfx.ResourceMesh], counts[gfx.ResourceModel],
			counts[gfx.ResourceFont], counts[gfx.ResourceRenderTexture], counts[gfx.ResourceEnvironment]))
		at := -1
		u.ListBox("resources", 170, items, &at)
	})
}

// resourceLine describes one GPU resource in a line.
func resourceLine(r gfx.Resource) string {
	switch r.Kind {
	case gfx.ResourceMesh:
		return fmt.Sprintf("%-14s %d verts, %d indices   %s", r.Kind, r.Vertices, r.Indices, bytesText(r.Bytes))
	case gfx.ResourceModel:
		return fmt.Sprintf("%-14s %d parts", r.Kind, r.Parts)
	default:
		return fmt.Sprintf("%-14s %dx%d   %s", r.Kind, r.Width, r.Height, bytesText(r.Bytes))
	}
}

// bytesText renders a byte count in the largest unit that keeps it above
// one.
func bytesText(n int) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	}
	return strconv.Itoa(n) + " B"
}
