package ui

import (
	"fmt"
	"testing"
)

// benchFrame runs body as one interface frame, opening and closing the
// graphics frame the way the engine loop does.
func benchFrame(b *testing.B, c *Context, in *feeder, body func()) {
	in.EndUpdate()
	in.SetDrawing(true)
	ok, err := beginFrame(c)
	if err != nil {
		b.Fatal(err)
	}
	if ok {
		c.Begin(in.state, body)
	}
	if err := endFrame(c); err != nil {
		b.Fatal(err)
	}
	in.SetDrawing(false)
	in.EndFrame()
}

var richMarkup = []string{
	"Plain text with [b]bold[/b] and [i]italic[/i] words.",
	"A [#ff8800]coloured[/#] run and a [link=go]link[/link] to click.",
	"[u]Underlined[/u] then [b][i]both at once[/i][/b] and a tail.",
}

// BenchmarkRichLabels draws fifty rich labels in one frame, the cost of
// turning markup into runs, measuring it and drawing it.
func BenchmarkRichLabels(b *testing.B) {
	c := newContext(b)
	in := newFeeder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		_ = i
		benchFrame(b, c, in, func() {
			c.Panel("Rich", Rect{X: 0, Y: 0, W: 300, H: 240}, func() {
				for j := range 50 {
					c.RichLabel(richMarkup[j%len(richMarkup)])
				}
			})
		})
	}
}

// BenchmarkCaptions formats the captions and accessibility values two
// hundred mixed widgets write in a frame. Submitting a frame allocates
// far more than building one, so this measures the formatting on its own;
// BenchmarkCaptionsFmt is the same work through fmt, which is what the
// widgets used before.
func BenchmarkCaptions(b *testing.B) {
	c := &Context{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := range 50 {
			capSink = c.labelFloat("gain", float32(i)*0.01, 2)
			capSink = c.formatFloat(float32(i)*0.01, 2)
			capSink = c.labelInt("count", i)
			capSink = c.formatInt(i)
			capSink = c.formatInt(i)
			capSink = c.formatInt(i)
			capSink = c.formatPercent(float32(i))
		}
	}
}

func BenchmarkCaptionsFmt(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := range 50 {
			capSink = fmt.Sprintf("%s: %.2f", "gain", float32(i)*0.01)
			capSink = fmt.Sprintf("%.2f", float32(i)*0.01)
			capSink = fmt.Sprintf("%s: %d", "count", i)
			capSink = fmt.Sprint(i)
			capSink = fmt.Sprint(i)
			capSink = fmt.Sprint(i)
			capSink = fmt.Sprintf("%.0f%%", float32(i))
		}
	}
}

var capSink string

// TestCaptionsMatchFmt keeps the hand-rolled formatting producing what
// fmt produced before, digit for digit.
func TestCaptionsMatchFmt(t *testing.T) {
	c := &Context{}
	for _, v := range []float32{0, 1, -1, 0.005, 0.125, 12345.678, -0.4999} {
		if got, want := c.formatFloat(v, 2), fmt.Sprintf("%.2f", v); got != want {
			t.Errorf("formatFloat(%v) = %q, fmt gives %q", v, got, want)
		}
		if got, want := c.labelFloat("gain", v, 2), fmt.Sprintf("%s: %.2f", "gain", v); got != want {
			t.Errorf("labelFloat(%v) = %q, fmt gives %q", v, got, want)
		}
		if got, want := c.formatPercent(v*100), fmt.Sprintf("%.0f%%", v*100); got != want {
			t.Errorf("formatPercent(%v) = %q, fmt gives %q", v, got, want)
		}
	}
	for _, v := range []int{0, 7, -7, 1000000} {
		if got, want := c.formatInt(v), fmt.Sprint(v); got != want {
			t.Errorf("formatInt(%d) = %q, fmt gives %q", v, got, want)
		}
		if got, want := c.labelInt("count", v), fmt.Sprintf("%s: %d", "count", v); got != want {
			t.Errorf("labelInt(%d) = %q, fmt gives %q", v, got, want)
		}
	}
	for _, rgb := range [][3]uint8{{0, 0, 0}, {255, 128, 1}, {17, 34, 51}} {
		got := c.formatHex(rgb[0], rgb[1], rgb[2])
		want := fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
		if got != want {
			t.Errorf("formatHex(%v) = %q, fmt gives %q", rgb, got, want)
		}
	}
}

// BenchmarkMixedWidgets builds a frame of two hundred widgets whose
// captions carry numbers, the shape of a debug or settings panel.
func BenchmarkMixedWidgets(b *testing.B) {
	c := newContext(b)
	in := newFeeder()
	var (
		f    = float32(0.4)
		n    = 7
		spin = 3
		prog = float32(0.62)
	)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchFrame(b, c, in, func() {
			c.Panel("Mixed", Rect{X: 0, Y: 0, W: 300, H: 240}, func() {
				for range 50 {
					c.Slider("gain", &f, 0, 1)
					c.IntSlider("count", &n, 0, 10)
					c.Spinner("depth", &spin, 0, 10, 1)
					c.Progress("load", prog)
				}
			})
		})
	}
}
