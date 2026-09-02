// Package audio mixes sounds in Go and hands float32 stereo frames to the
// platform's output device. A Sound holds decoded samples at the mixer's
// rate; Play starts a Voice that can be adjusted or stopped while it runs.
// Music streams from a decoder instead of being held in memory. Voices
// can be placed in the listener's world, filtered, sent to a shared reverb,
// faded, pitched and prioritised, and grouped on a Bus to be turned down
// or paused together. Mixing happens on the audio device's thread, so
// every method is safe to call from the game loop.
//
// Decoders (Decode, DecodeWAV, DecodeOGG, DecodeMP3) take the whole file
// as bytes; streams (OpenMusic) take readers, because they seek while
// they play.
package audio

import (
	"errors"
	"sync"

	"github.com/matjam/bunyip/lin"
)

// Mixer sums playing voices into the output stream.
type Mixer struct {
	mu        sync.Mutex
	rate      int
	voices    []*Voice
	master    float32
	paused    bool
	maxVoices int
	scratch   []float32
	sendBuf   []float32
	listener  Listener
	reverb    *reverb
	finished  []func() // OnDone callbacks to run once the lock is released

	buses                    map[string]*Bus
	music, effects, dialogue *Bus
}

// NewMixer makes a mixer for the given output sample rate.
func NewMixer(rate int) *Mixer {
	m := &Mixer{rate: rate, master: 1, maxVoices: 64, buses: map[string]*Bus{},
		listener: Listener{Forward: lin.Vec3{Z: -1}, Up: lin.Vec3{Y: 1}}}
	m.music = m.NewBus("music")
	m.effects = m.NewBus("effects")
	m.dialogue = m.NewBus("dialogue")
	return m
}

// Rate is the sample rate sounds are stored and mixed at.
func (m *Mixer) Rate() int { return m.rate }

// SetMasterVolume scales every voice; 1 is unity.
func (m *Mixer) SetMasterVolume(v float32) {
	m.mu.Lock()
	m.master = v
	m.mu.Unlock()
}

// SetMaxVoices caps how many voices play at once (default 64). When the
// mixer is full, a new voice takes the place of the quietest voice of the
// lowest priority no higher than its own, or is refused.
func (m *Mixer) SetMaxVoices(n int) {
	m.mu.Lock()
	m.maxVoices = max(n, 1)
	m.mu.Unlock()
}

// PlayOptions shape a voice at start. Zero values mean the defaults noted.
type PlayOptions struct {
	Volume   float32 // 1
	Pan      float32 // -1 left .. +1 right; ignored when Positional
	Loop     bool
	Pitch    float32 // playback rate multiplier for sounds; 1
	Priority int     // higher survives when the mixer is full
	FadeIn   float32 // seconds to rise from silence
	Reverb   float32 // 0..1 send into the mixer's reverb (see SetReverb)
	LowPass  float32 // low-pass cutoff in Hz; 0 leaves the sound unfiltered
	Bus      *Bus    // bus the voice plays through; nil is the master alone

	// Positional voices are heard from Position relative to the listener:
	// full volume within MinDistance (1), fading to silence at
	// MaxDistance (100), panned by direction.
	Positional  bool
	Position    lin.Vec3
	MinDistance float32
	MaxDistance float32
}

// Play starts a sound and returns its voice.
func (m *Mixer) Play(s *Sound, opts PlayOptions) *Voice {
	v := m.newVoice(opts)
	v.snd = s
	return m.add(v)
}

func (m *Mixer) newVoice(opts PlayOptions) *Voice {
	if opts.Volume == 0 {
		opts.Volume = 1
	}
	if opts.Pitch == 0 {
		opts.Pitch = 1
	}
	if opts.MinDistance == 0 {
		opts.MinDistance = 1
	}
	if opts.MaxDistance == 0 {
		opts.MaxDistance = 100
	}
	v := &Voice{m: m, bus: opts.Bus, vol: opts.Volume, pan: opts.Pan, loop: opts.Loop, pitch: opts.Pitch,
		priority: opts.Priority, reverb: opts.Reverb, positional: opts.Positional,
		position: opts.Position, minDist: opts.MinDistance, maxDist: opts.MaxDistance}
	if opts.LowPass > 0 {
		v.lp = newLowPass(opts.LowPass, m.rate)
	}
	if opts.FadeIn > 0 {
		total := int(opts.FadeIn * float32(m.rate))
		v.fade = &fade{from: 0, to: opts.Volume, left: total, total: total}
	}
	return v
}

// add places the voice in the mix, stealing a lower-priority voice when
// the mixer is full. A refused voice comes back already finished, so
// callers need not check.
func (m *Mixer) add(v *Voice) *Voice {
	m.mu.Lock()
	if len(m.voices) < m.maxVoices {
		m.voices = append(m.voices, v)
		m.mu.Unlock()
		return v
	}
	victim := -1
	for i, o := range m.voices {
		if o.priority > v.priority {
			continue
		}
		if victim < 0 || o.priority < m.voices[victim].priority ||
			(o.priority == m.voices[victim].priority && o.curL+o.curR < m.voices[victim].curL+m.voices[victim].curR) {
			victim = i
		}
	}
	if victim < 0 {
		v.done = true
	} else {
		m.finish(m.voices[victim])
		m.voices[victim] = v
	}
	done := m.takeFinished()
	m.mu.Unlock()
	run(done)
	return v
}

// StopAll silences every voice.
func (m *Mixer) StopAll() {
	m.mu.Lock()
	for _, v := range m.voices {
		m.finish(v)
	}
	m.voices = m.voices[:0]
	done := m.takeFinished()
	m.mu.Unlock()
	run(done)
}

// finish ends a voice under the lock, queueing its OnDone callback.
func (m *Mixer) finish(v *Voice) {
	v.done = true
	if v.onDone != nil {
		m.finished = append(m.finished, v.onDone)
	}
}

// takeFinished hands over the queued callbacks, to run once unlocked.
func (m *Mixer) takeFinished() []func() {
	done := m.finished
	m.finished = nil
	return done
}

func run(fns []func()) {
	for _, fn := range fns {
		fn()
	}
}

// Playing counts active voices.
func (m *Mixer) Playing() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.voices)
}

// Mix writes len(out)/2 stereo frames, called by the output device.
func (m *Mixer) Mix(out []float32) {
	clear(out)
	m.mu.Lock()
	frames := len(out) / 2
	if len(m.scratch) < len(out) {
		m.scratch = make([]float32, len(out))
		m.sendBuf = make([]float32, len(out))
	}
	scratch, send := m.scratch[:len(out)], m.sendBuf[:len(out)]
	clear(send)
	live := m.voices[:0]
	for _, v := range m.voices {
		if v.render(m, out, send, scratch, frames) {
			live = append(live, v)
		} else {
			m.finish(v)
		}
	}
	for i := len(live); i < len(m.voices); i++ {
		m.voices[i] = nil
	}
	m.voices = live
	if m.reverb != nil {
		m.reverb.process(send, out)
	}
	for i, s := range out {
		out[i] = max(-1, min(1, s))
	}
	done := m.takeFinished()
	m.mu.Unlock()
	run(done)
}

// Voice is one playing sound or stream.
type Voice struct {
	m        *Mixer
	bus      *Bus
	snd      *Sound
	stream   Stream
	pos      float64 // frames into the sound, fractional under pitch
	played   int64   // frames taken from a stream since it started or last sought
	onDone   func()
	vol      float32
	pan      float32
	pitch    float32
	loop     bool
	done     bool
	stop     bool
	paused   bool
	priority int
	reverb   float32
	lp       *lowPass
	fade     *fade

	positional       bool
	position         lin.Vec3
	minDist, maxDist float32

	// Gains ramp across each block so changes never click.
	curL, curR float32
	started    bool
}

// fade moves a voice's volume from one level to another over total frames.
type fade struct {
	from, to    float32
	left, total int
	stop        bool // end the voice when the fade completes
}

func (f *fade) value() float32 {
	if f.total == 0 {
		return f.to
	}
	t := 1 - float32(f.left)/float32(f.total)
	return f.from + (f.to-f.from)*t
}

func (v *Voice) set(fn func()) {
	v.m.mu.Lock()
	fn()
	v.m.mu.Unlock()
}

// Stop ends the voice.
func (v *Voice) Stop() { v.set(func() { v.stop = true }) }

// SetVolume changes the voice's gain; 1 is unity.
func (v *Voice) SetVolume(vol float32) { v.set(func() { v.vol = vol; v.fade = nil }) }

// SetPan moves the voice between -1 (left) and +1 (right).
func (v *Voice) SetPan(pan float32) { v.set(func() { v.pan = pan }) }

// SetPitch changes a sound's playback rate; 2 plays an octave up at
// double speed. Streams ignore it.
func (v *Voice) SetPitch(p float32) { v.set(func() { v.pitch = max(p, 0.01) }) }

// SetPaused holds the voice in place, silent, until resumed.
func (v *Voice) SetPaused(p bool) { v.set(func() { v.paused = p }) }

// SetPosition moves a positional voice.
func (v *Voice) SetPosition(p lin.Vec3) { v.set(func() { v.position = p; v.positional = true }) }

// SetReverb changes the send level into the mixer's reverb.
func (v *Voice) SetReverb(send float32) { v.set(func() { v.reverb = send }) }

// SetLowPass sets the low-pass cutoff in Hz; 0 removes the filter.
func (v *Voice) SetLowPass(cutoff float32) {
	v.set(func() {
		if cutoff <= 0 {
			v.lp = nil
		} else if v.lp == nil {
			v.lp = newLowPass(cutoff, v.m.rate)
		} else {
			v.lp.set(cutoff, v.m.rate)
		}
	})
}

// FadeTo moves the volume to vol over seconds.
func (v *Voice) FadeTo(vol, seconds float32) {
	v.set(func() {
		from := v.vol
		if v.fade != nil {
			from = v.fade.value()
		}
		total := int(seconds * float32(v.m.rate))
		v.fade = &fade{from: from, to: vol, left: total, total: total}
	})
}

// FadeOut fades to silence over seconds and then stops the voice.
func (v *Voice) FadeOut(seconds float32) {
	v.FadeTo(0, seconds)
	v.set(func() { v.fade.stop = true })
}

// Playing reports whether the voice is still audible.
func (v *Voice) Playing() bool {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	return !v.done
}

// Position is how far into the sound the voice is, in seconds. For a
// stream it counts the frames taken since the voice started or last
// sought, which for Music is what the listener has heard.
func (v *Voice) Position() float64 {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	if v.stream != nil {
		return float64(v.played) / float64(v.m.rate)
	}
	return v.pos / float64(v.m.rate)
}

// ErrNotSeekable is returned by Voice.Seek for a stream that cannot seek.
var ErrNotSeekable = errors.New("audio: stream cannot seek")

// Seeker is a Stream that can jump to a time, as Music can.
type Seeker interface {
	Stream
	// Seek moves playback to seconds from the start.
	Seek(seconds float64) error
}

// Seek moves a sound voice to seconds from the start, clamped to the
// sound's length. A stream voice is moved when its stream is a Seeker;
// otherwise it stays put and ErrNotSeekable is returned. The stream
// itself is asked to seek while the mixer is locked, so a Seeker must
// not call back into the mixer.
func (v *Voice) Seek(seconds float64) error {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	seconds = max(seconds, 0)
	if v.stream != nil {
		s, ok := v.stream.(Seeker)
		if !ok {
			return ErrNotSeekable
		}
		if err := s.Seek(seconds); err != nil {
			return err
		}
		v.played = int64(seconds * float64(v.m.rate))
		return nil
	}
	if v.snd != nil {
		v.pos = min(seconds*float64(v.m.rate), float64(v.snd.Frames()))
	}
	return nil
}

// OnDone registers fn to run when the voice ends, whether it played out,
// was stopped, faded out, or lost its slot to a higher priority voice. It
// is called on the mixer's thread, usually the audio device's, after the
// mixer has released its lock, so it may start another voice, but it must
// return quickly and must not block. A voice that has already ended runs
// fn at once on the calling goroutine. Only the last fn registered runs.
func (v *Voice) OnDone(fn func()) {
	v.m.mu.Lock()
	done := v.done
	if !done {
		v.onDone = fn
	}
	v.m.mu.Unlock()
	if done && fn != nil {
		fn()
	}
}

// render accumulates the voice into out (and its reverb send into send),
// using scratch for the dry signal, and reports whether it continues.
func (v *Voice) render(m *Mixer, out, send, scratch []float32, frames int) bool {
	if v.stop || v.done {
		return false
	}
	if v.paused || m.paused || (v.bus != nil && v.bus.paused) {
		v.curL, v.curR = 0, 0
		return true
	}
	var n int
	var more bool
	if v.stream != nil {
		n = v.stream.Read(scratch[:frames*2])
		more = n == frames
		v.played += int64(n)
	} else {
		n, more = v.readSound(scratch[:frames*2])
	}
	if n == 0 {
		return more
	}
	if v.lp != nil {
		v.lp.process(scratch[:n*2])
	}
	vol := v.vol
	if v.fade != nil {
		vol = v.fade.value()
		v.fade.left = max(v.fade.left-n, 0)
	}
	gain := vol * m.master
	if v.bus != nil {
		gain *= v.bus.vol
	}
	pan := v.pan
	if v.positional {
		att, p := m.listener.attenuate(v.position, v.minDist, v.maxDist)
		gain *= att
		pan = p
	}
	pan = max(-1, min(1, pan))
	tl := gain * sqrt32((1-pan)/2)
	tr := gain * sqrt32((1+pan)/2)
	if !v.started {
		v.curL, v.curR, v.started = tl, tr, true
	}
	dl := (tl - v.curL) / float32(n)
	dr := (tr - v.curR) / float32(n)
	l, r := v.curL, v.curR
	for i := range n {
		l += dl
		r += dr
		sl := scratch[i*2] * l
		sr := scratch[i*2+1] * r
		out[i*2] += sl
		out[i*2+1] += sr
		if v.reverb > 0 {
			send[i*2] += sl * v.reverb
			send[i*2+1] += sr * v.reverb
		}
	}
	v.curL, v.curR = tl, tr
	if v.fade != nil && v.fade.left == 0 {
		v.vol = v.fade.to
		stop := v.fade.stop
		v.fade = nil
		if stop {
			return false
		}
	}
	return more
}

// readSound copies the next frames of the sound into dst at the voice's
// pitch, and reports how many it wrote and whether more remain.
func (v *Voice) readSound(dst []float32) (int, bool) {
	s := v.snd
	if s == nil || len(s.samples) < 2 {
		return 0, false
	}
	total := len(s.samples) / 2
	frames := len(dst) / 2
	for i := range frames {
		if v.pos >= float64(total) {
			if !v.loop {
				return i, false
			}
			v.pos -= float64(total)
		}
		j := int(v.pos)
		t := float32(v.pos - float64(j))
		k := j + 1
		if k >= total {
			if v.loop {
				k = 0
			} else {
				k = j
			}
		}
		dst[i*2] = s.samples[j*2]*(1-t) + s.samples[k*2]*t
		dst[i*2+1] = s.samples[j*2+1]*(1-t) + s.samples[k*2+1]*t
		v.pos += float64(v.pitch)
	}
	return frames, true
}
