// Command audio is the sound tour: positional voices orbiting the
// listener with Doppler and occlusion on sliders, a shared reverb and a
// low-pass filter on sliders, fades, pitch, voice priorities under a
// small voice cap, a synthesised music stream, -music to stream an Ogg,
// MP3 or WAV file from disk, and -zone to put a reverb zone at the
// listener so the room changes as the orbiting sources pass through it.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/ui"
)

type source struct {
	voice *audio.Voice
	color gfx.Color
	speed float32
	dist  float32
	pos   lin.Vec3
}

type game struct {
	seconds   float64
	shot      string
	musicPath string
	zone      bool

	font    *gfx.Font
	ui      *ui.Context
	tone    *audio.Sound
	click   *audio.Sound
	pad     *audio.Voice
	sources []source
	music   *audio.Music
	stream  *audio.Voice

	reverb    float32
	room      float32
	cutoff    float32
	pitch     float32
	occlusion float32
	doppler   float32
	shotDone  bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	m := ctx.Audio
	m.SetMaxVoices(12) // small on purpose, so the burst button shows stealing
	if g.tone, err = m.NewSound(audio.Sine(440, 0.3, m.Rate())); err != nil {
		return err
	}
	if g.click, err = m.NewSound(audio.Sine(1200, 0.05, m.Rate())); err != nil {
		return err
	}
	pad, err := m.NewSound(audio.Sine(110, 2, m.Rate()))
	if err != nil {
		return err
	}
	g.reverb, g.room, g.cutoff, g.pitch = 0.3, 0.6, 4000, 1
	m.SetReverb(audio.ReverbSettings{RoomSize: g.room, Wet: g.reverb})
	// Doppler in pixels: a high speed of sound keeps the orbit's shift
	// to a semitone or so at full factor.
	g.doppler = 1
	m.SetDoppler(g.doppler)
	m.SetSpeedOfSound(3000)
	// A looping pad the sliders act on.
	g.pad = m.Play(pad, audio.PlayOptions{Loop: true, Volume: 0.35, LowPass: g.cutoff, Reverb: 1, FadeIn: 1.5, Priority: 10})
	// Three orbiting positional sources at different pitches.
	for i, f := range []float64{330, 495, 660} {
		snd, err := m.NewSound(audio.Sine(f, 1, m.Rate()))
		if err != nil {
			return err
		}
		v := m.Play(snd, audio.PlayOptions{Loop: true, Volume: 0.3, Positional: true, MinDistance: 60, MaxDistance: 500, Priority: 10})
		g.sources = append(g.sources, source{voice: v, speed: 0.4 + 0.3*float32(i), dist: 120 + 90*float32(i),
			color: gfx.RGB(uint8(255-80*i), uint8(120+60*i), uint8(80*i+60))})
	}
	// Music: a file if given, otherwise a synthesised arpeggio stream.
	if g.musicPath != "" {
		f, err := os.Open(g.musicPath)
		if err != nil {
			return err
		}
		if g.music, err = m.OpenMusic(f, true); err != nil {
			return err
		}
		g.stream = m.PlayStream(g.music, audio.PlayOptions{Volume: 0.5, Priority: 20})
	} else {
		g.stream = m.PlayStream(&arpeggio{rate: m.Rate()}, audio.PlayOptions{Volume: 0.18, Reverb: 0.6, Priority: 20})
	}
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	if g.music != nil {
		g.music.Close()
	}
	g.font.Destroy()
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	// The listener sits at the window centre; sources circle it. With
	// -zone, a large hall covers the left half of the window and the
	// listener is moved with the mouse so it can walk in and out.
	cx, cy := ctx.Width/2, ctx.Height/2
	lx, ly := cx, cy
	if g.zone {
		lx, ly = ctx.Input.Mouse()
		ctx.Audio.SetReverbZones([]audio.ReverbZone{{
			Center: lin.V3(cx/2, cy, 0), Radius: cx / 2, Fade: cx / 8,
			Settings: audio.ReverbSettings{RoomSize: 0.95, Damping: 0.2, Wet: 0.8},
		}})
	}
	ctx.Audio.SetListener2D(lx, ly)
	for i := range g.sources {
		s := &g.sources[i]
		a := float64(float32(ctx.Time) * s.speed)
		s.pos = lin.V3(cx+s.dist*float32(math.Cos(a)), cy+s.dist*float32(math.Sin(a)), 0)
		s.voice.SetPosition(s.pos)
		// The orbit's tangent, in pixels per second, drives Doppler.
		v := s.dist * s.speed
		s.voice.SetVelocity(lin.V3(-v*float32(math.Sin(a)), v*float32(math.Cos(a)), 0))
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	cx := ctx.Width / 2
	for _, s := range g.sources {
		for r := float32(3); r <= 9; r += 3 { // rings fade with distance
			c := s.color
			c.A = 0.25
			gr.FillRect(s.pos.X-r*3, s.pos.Y-r*3, r*6, r*6, c)
		}
		gr.FillRect(s.pos.X-6, s.pos.Y-6, 12, 12, s.color)
	}
	l := ctx.Audio.Listener().Position
	if g.zone {
		gr.FillRect(0, 0, cx, ctx.Height, gfx.RGBA(80, 90, 160, 40)) // the hall
		r := ctx.Audio.Reverb()
		gr.DrawText(g.font, fmt.Sprintf("hall: wet %.2f room %.2f (move the mouse)", r.Wet, r.RoomSize), 16, ctx.Height-30, gfx.RGB(200, 200, 210))
	}
	gr.FillRect(l.X-8, l.Y-8, 16, 16, gfx.RGB(240, 240, 250))
	gr.DrawText(g.font, "listener", l.X-24, l.Y+12, gfx.RGB(200, 200, 210))

	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Audio", ui.Rect{X: 16, Y: 16, W: 300, H: 480}, func() {
			u.Label(fmt.Sprintf("%d voices playing (cap 12)", ctx.Audio.Playing()))
			if u.Slider("Reverb wet", &g.reverb, 0, 1) || u.Slider("Room size", &g.room, 0, 1) {
				ctx.Audio.SetReverb(audio.ReverbSettings{RoomSize: g.room, Wet: g.reverb})
			}
			if u.Slider("Pad low-pass Hz", &g.cutoff, 200, 8000) {
				g.pad.SetLowPass(g.cutoff)
			}
			if u.Slider("Orbit occlusion", &g.occlusion, 0, 1) {
				for _, s := range g.sources {
					s.voice.SetOcclusion(g.occlusion)
				}
			}
			if u.Slider("Doppler factor", &g.doppler, 0, 3) {
				ctx.Audio.SetDoppler(g.doppler)
			}
			u.Slider("Click pitch", &g.pitch, 0.5, 2)
			u.Row(2, func() {
				if u.Button("Click") {
					ctx.Audio.Play(g.click, audio.PlayOptions{Volume: 0.6, Pitch: g.pitch, Reverb: g.reverb})
				}
				if u.Button("Burst 40") {
					for i := range 40 {
						// Low-priority voices; the cap makes the mixer steal the quietest.
						ctx.Audio.Play(g.tone, audio.PlayOptions{Volume: 0.2, Pitch: 0.8 + 0.02*float32(i), Pan: float32(i%5-2) / 2})
					}
				}
			})
			u.Row(2, func() {
				if u.Button("Fade pad out") {
					g.pad.FadeTo(0, 1.5)
				}
				if u.Button("Fade pad in") {
					g.pad.FadeTo(0.35, 1.5)
				}
			})
			u.Row(2, func() {
				if u.Button("Pause music") {
					g.stream.SetPaused(true)
				}
				if u.Button("Resume music") {
					g.stream.SetPaused(false)
				}
			})
			if g.music != nil {
				u.Label(fmt.Sprintf("Streaming %s, %.1f s buffered", g.musicPath, g.music.Buffered()))
			} else {
				u.Label("Music is a synthesised Stream; pass -music file.ogg to stream a file.")
			}
			u.Label("Orbiting sources pan and fade with distance from the listener.")
		})
	})
	return nil
}

// arpeggio is a Stream: it synthesises a slow chord arpeggio on demand,
// which is how procedural music or a custom decoder plugs into the mixer.
type arpeggio struct {
	rate  int
	t     float64
	phase float64
}

var notes = []float64{261.63, 329.63, 392.00, 523.25, 392.00, 329.63}

func (a *arpeggio) Read(out []float32) int {
	frames := len(out) / 2
	for i := range frames {
		step := int(a.t*3) % len(notes)
		f := notes[step]
		env := 1 - math.Mod(a.t*3, 1) // decays over each note
		a.phase += 2 * math.Pi * f / float64(a.rate)
		s := float32(math.Sin(a.phase)*0.6+math.Sin(2*a.phase)*0.25) * float32(env)
		out[i*2], out[i*2+1] = s, s
		a.t += 1 / float64(a.rate)
	}
	return frames
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	music := flag.String("music", "", "Ogg, MP3 or WAV file to stream")
	zone := flag.Bool("zone", false, "put a reverb zone over the left half and move the listener with the mouse")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip audio", Width: 900, Height: 600, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, musicPath: *music, zone: *zone})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio:", err)
		os.Exit(1)
	}
}
