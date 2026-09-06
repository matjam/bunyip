package audio

import "github.com/matjam/bunyip/internal/hook"

// driver is how the output device pulls frames out of a Mixer. Mix is
// exported to satisfy hook.Audio, but the type is not, so the plumbing
// does not appear on the surface a game reads.
type driver struct{ m *Mixer }

func (d driver) Mix(out []float32) { d.m.mix(out) }
func (d driver) Game() any         { return d.m }
func (d driver) OpenOutput() error { return d.m.SetOutputDevice("") }
func (d driver) CloseOutput()      { d.m.CloseOutput() }

func (d driver) SetDevice(open bool) {
	d.m.mu.Lock()
	d.m.noDevice = !open
	d.m.mu.Unlock()
}

func init() {
	hook.NewMixer = func(rate int) hook.Audio { return driver{NewMixer(rate)} }
}
