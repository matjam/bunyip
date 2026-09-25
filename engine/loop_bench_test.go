//go:build !windows

package engine

import (
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/vk"
)

// These benchmarks measure what the loop itself costs per frame, with a
// game that does nothing, through the real headless path (a Vulkan device,
// an offscreen swapchain stand-in, the fence per frame slot). They report
// process CPU time from getrusage, so driver threads are included. They
// are left out on Windows, which has no getrusage.
//
//	go test -run '^$' -bench LoopFrame -benchtime 2000x ./engine
//	BUNYIP_BENCH_SECONDS=10 go test -run '^$' -bench LoopPaced -benchtime 1x ./engine

func cpuTime() time.Duration {
	var r syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &r)
	return time.Duration(r.Utime.Nano() + r.Stime.Nano())
}

type loopSample struct {
	wall, cpu  time.Duration
	mallocs    uint64
	bytes      uint64
	gcs        uint32
	frames     int
	updateCall int
}

func sampleNow() loopSample {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return loopSample{wall: time.Duration(time.Now().UnixNano()), cpu: cpuTime(), mallocs: m.Mallocs, bytes: m.TotalAlloc, gcs: m.NumGC}
}

// benchGame counts frames and measures between frame warm and the end:
// either a frame count or a wall-clock duration.
type benchGame struct {
	warm, frames int
	dur          time.Duration
	init         func(*Context) error
	draw         int
	updates      int
	start, end   loopSample
	startAt      time.Time
}

func (g *benchGame) Init(ctx *Context) error {
	if g.init != nil {
		return g.init(ctx)
	}
	return nil
}

func (g *benchGame) Update(ctx *Context) error { g.updates++; return nil }

func (g *benchGame) Draw(ctx *Context) error {
	g.draw++
	switch {
	case g.draw == g.warm:
		g.updates = 0
		g.start = sampleNow()
		g.startAt = time.Now()
	case g.draw > g.warm:
		if (g.dur == 0 && g.draw == g.warm+g.frames) || (g.dur > 0 && time.Since(g.startAt) >= g.dur) {
			g.end = sampleNow()
			g.end.frames = g.draw - g.warm
			g.end.updateCall = g.updates
			ctx.Quit()
		}
	}
	return nil
}

func (g *benchGame) report(b *testing.B) {
	n := float64(g.end.frames)
	wall := g.end.wall - g.start.wall
	cpu := g.end.cpu - g.start.cpu
	b.ReportMetric(float64(wall.Microseconds())/n, "wall-us/frame")
	b.ReportMetric(float64(cpu.Microseconds())/n, "cpu-us/frame")
	b.ReportMetric(float64(g.end.mallocs-g.start.mallocs)/n, "allocs/frame")
	b.ReportMetric(float64(g.end.bytes-g.start.bytes)/n, "B/frame")
	b.ReportMetric(float64(g.end.gcs-g.start.gcs)/wall.Minutes(), "GC/min")
	b.ReportMetric(100*cpu.Seconds()/wall.Seconds(), "cpu%")
	b.ReportMetric(n/wall.Seconds(), "fps")
	b.ReportMetric(float64(g.end.updateCall)/n, "updates/frame")
}

func benchRun(b *testing.B, cfg Config, g *benchGame) {
	if err := vk.Load(); err != nil {
		b.Skipf("no Vulkan: %v", err)
	}
	cfg.Headless, cfg.NoAudio = true, true
	// The device's log line would land inside benchstat's input.
	cfg.Log = slog.New(slog.DiscardHandler)
	if cfg.Width == 0 {
		cfg.Width, cfg.Height = 1280, 720
	}
	if err := Run(cfg, g); err != nil {
		b.Fatal(err)
	}
	if g.end.frames == 0 {
		b.Fatal("the measured window never closed")
	}
	g.report(b)
}

// BenchmarkLoopFrame runs frames back to back (FixedClock turns the
// headless pacing sleep off), one Update and one empty Draw per frame.
func BenchmarkLoopFrame(b *testing.B) {
	cases := []struct {
		name string
		cfg  Config
		init func(*Context) error
	}{
		{"plain", Config{}, nil},
		{"console-closed", Config{Console: true}, nil},
		{"console-open", Config{Console: true}, func(c *Context) error { c.Console.SetOpen(true); return nil }},
		{"overlay", Config{Debug: true}, nil},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			g := &benchGame{warm: 120, frames: max(b.N, 200), init: tc.init}
			cfg := tc.cfg
			cfg.FixedClock = true
			benchRun(b, cfg, g)
		})
	}
}

func benchSeconds() time.Duration {
	if s, err := strconv.ParseFloat(os.Getenv("BUNYIP_BENCH_SECONDS"), 64); err == nil && s > 0 {
		return time.Duration(s * float64(time.Second))
	}
	return 3 * time.Second
}

// BenchmarkLoopPacedRealtime is the ordinary real-time loop paced by the
// headless sleep, for the frame rate it holds, CPU % and GC per minute.
func BenchmarkLoopPacedRealtime(b *testing.B) {
	for _, step := range []time.Duration{time.Second / 60, time.Second / 240} {
		b.Run(strconv.Itoa(int(time.Second/step))+"Hz", func(b *testing.B) {
			g := &benchGame{warm: 60, dur: benchSeconds()}
			benchRun(b, Config{FixedStep: step}, g)
		})
	}
}

// blockingEvents is a native-like event source for a turn-based loop:
// Poll(true) blocks until the benchmark's deadline, like
// nextEventMatchingMask with distantFuture, and then delivers a close.
type blockingEvents struct {
	eventSource
	wakeEvery time.Duration // zero: never wake before the deadline
	deadline  time.Time
}

func (a *blockingEvents) Poll(wait bool) []platform.Event {
	if !wait {
		return nil
	}
	now := time.Now()
	if !now.Before(a.deadline) {
		return []platform.Event{{Kind: platform.EventClose}}
	}
	sleep := a.deadline.Sub(now)
	if a.wakeEvery > 0 {
		sleep = min(sleep, a.wakeEvery)
	}
	time.Sleep(sleep)
	return nil // an OS event the platform layer did not translate
}
func (a *blockingEvents) Gamepads() []platform.GamepadState { return nil }

// BenchmarkLoopTurnIdle drives a turn-based loop with a blocking source.
// "idle" never wakes; "untranslated-wakes" returns an empty batch every
// 16 ms, as an AppKit event the layer ignores would, and counts the
// frames drawn.
func BenchmarkLoopTurnIdle(b *testing.B) {
	for _, tc := range []struct {
		name  string
		every time.Duration
	}{{"idle", 0}, {"untranslated-wakes", 16 * time.Millisecond}} {
		b.Run(tc.name, func(b *testing.B) {
			g := &pauseGame{}
			l := newRunningPauseLoop(Config{TurnBased: true, FixedStep: time.Second / 60}, g)
			src := &blockingEvents{wakeEvery: tc.every, deadline: time.Now().Add(time.Second)}
			l.app = src
			l.ctx.app = src
			c0, t0 := cpuTime(), time.Now()
			if err := l.run(); err != nil {
				b.Fatal(err)
			}
			wall := time.Since(t0)
			b.ReportMetric(100*(cpuTime()-c0).Seconds()/wall.Seconds(), "cpu%")
			b.ReportMetric(float64(g.draws)/wall.Seconds(), "draws/s")
			b.ReportMetric(float64(len(g.deltas))/wall.Seconds(), "updates/s")
		})
	}
}
