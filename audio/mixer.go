// Package audio mixes sounds in Go and passes float32 stereo frames to
// the platform's output device. A Sound holds decoded samples at the
// mixer's rate. To play one, call Play, which returns a Voice that can
// be adjusted or stopped while it runs. Music streams from a decoder
// instead of being held in memory. Voices can be placed in the
// listener's world (with distance, panning, Doppler and occlusion),
// filtered, sent to a reverb, faded, pitched, muted, soloed and
// prioritised, and grouped on a Bus to be turned down, muted or paused
// together. The mixer has one shared reverb, which a ReverbZone replaces
// while the listener is inside that zone, and a Bus can carry a reverb
// of its own. Mixing happens on the audio device's thread, so every
// method is safe to call from the game loop, and every change in gain
// ramps across a block so nothing clicks. A setter copies its value in
// under a short lock and the mixer applies it at the start of the next
// block, so the game loop never waits for a block; the mixer reads
// streams with no lock held, so a Stream may take locks of its own and
// may call back into the mixer. Voice.Seek is the one method that waits
// for the block in flight, because it moves the playhead being read.
//
// The decoders (Decode, DecodeWAV, DecodeOGG, DecodeMP3) take the whole
// file as bytes. The streams (OpenMusic) take readers, because they seek
// while they play.
package audio

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"github.com/matjam/bunyip/lin"
)

// Mixer sums playing voices into the output stream.
//
// Two locks divide the work. mu guards everything a game can change from
// its own goroutine, and is held only briefly: a setter takes it, and the
// mixer takes it twice a block, once to copy out what it is about to mix
// and once to write back where every voice reached. mixMu is held across
// the whole block and marks the playback state the mixer owns while it
// runs, so nothing else may move a voice through its sound mid-block.
// Streams are read with neither lock held, so music, a tracker player or
// game code called from Stream.Read may take locks of its own, and may
// call back into the mixer, without waiting on the game loop.
type Mixer struct {
	mu        sync.Mutex
	mixMu     sync.Mutex // held across a block; guards playback position
	rate      int
	voices    []*Voice
	master    float32
	paused    bool
	maxVoices int
	scratch   []float32
	sendBuf   []float32
	listener  Listener
	finished  []func() // OnDone callbacks to run once the lock is released

	// The block being mixed. Only the mixer's thread touches these, under
	// mixMu, and they are reused from block to block so mixing allocates
	// nothing.
	snap      []voiceMix
	revBuses  []busReverb
	blkReverb *reverb

	reverb     *reverb        // the shared reverb, nil when off
	baseReverb ReverbSettings // what SetReverb was given
	applied    ReverbSettings // what the shared reverb is using now
	pending    bool           // applied has not reached the reverb yet
	zones      []ReverbZone

	doppler      float32 // Doppler factor, 0 off
	speedOfSound float32

	buses                    map[string]*Bus
	busList                  []*Bus // the same buses in creation order, to walk without the map
	music, effects, dialogue *Bus
}

// NewMixer makes a mixer for the given output sample rate.
func NewMixer(rate int) *Mixer {
	m := &Mixer{rate: rate, master: 1, maxVoices: 64, buses: map[string]*Bus{}, speedOfSound: 343,
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
	Reverb   float32 // 0..1 send into the bus's reverb, or the mixer's (see SetReverb)
	LowPass  float32 // low-pass cutoff in Hz; 0 leaves the sound unfiltered
	Bus      *Bus    // bus the voice plays through; nil is the master alone

	// Occlusion is how much of the world is between the source and the
	// listener: 0 clear, 1 fully blocked, which drops the voice 20 dB and
	// muffles it to 400 Hz. A game sets it from a physics ray.
	Occlusion float32

	// Positional voices are heard from Position relative to the listener:
	// full volume within MinDistance (1), fading to silence at
	// MaxDistance (100), panned by direction. Velocity, in world units per
	// second, drives Doppler once SetDoppler turns it on.
	Positional  bool
	Position    lin.Vec3
	Velocity    lin.Vec3
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
		position: opts.Position, velocity: opts.Velocity, minDist: opts.MinDistance, maxDist: opts.MaxDistance}
	if opts.LowPass > 0 {
		v.lp, v.lpc = &lowPass{}, newBiquad(opts.LowPass, m.rate)
	}
	v.setOcclusion(opts.Occlusion)
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

// busReverb is a bus's reverb and its send buffer for one block, copied
// out under the lock because a game may drop the reverb at any moment.
type busReverb struct {
	r   *reverb
	buf []float32
}

// voiceMix is one voice's part in a block: everything the mixer needs
// copied out under the lock, and what it reached written back under the
// lock afterwards. Every gain, pan and pitch is worked out at snapshot
// time, so mixing itself reads samples, filters them and ramps them into
// the output and touches no state a game can change.
type voiceMix struct {
	v      *Voice
	stream Stream
	snd    *Sound
	send   []float32 // reverb send this voice feeds, the mixer's or its bus's

	pos    float64 // frames into the sound, carried across blocks
	step   float32 // source frames per output frame, pitch with Doppler
	loop   bool
	reverb float32

	lp   *lowPass // the voice's filter state, nil when unfiltered
	lpc  biquad
	occ  *lowPass // occlusion's filter state, nil at 0
	occc biquad

	curL, curR float32 // gains at the start of the block
	tl, tr     float32 // gains to ramp to by the end of it

	skip   bool // held silent: nothing to read, nothing to write back
	frames int  // frames written
	more   bool // the voice continues into the next block
	fade   *fade
}

// mix writes len(out)/2 stereo frames. The output device calls it from
// its own thread, through internal/hook.
func (m *Mixer) mix(out []float32) {
	m.mixMu.Lock()
	clear(out)
	frames := len(out) / 2
	send := m.snapshot(out)
	scratch := m.scratch[:len(out)]
	for i := range m.snap {
		m.snap[i].render(scratch, out, frames)
	}
	if m.blkReverb != nil {
		m.blkReverb.process(send, out)
	}
	for _, b := range m.revBuses {
		b.r.process(b.buf, out)
	}
	for i, s := range out {
		out[i] = max(-1, min(1, s))
	}
	fns := m.apply()
	// The callbacks run with the lock released, as promised, so one that
	// seeks a voice does not deadlock the audio thread.
	m.mixMu.Unlock()
	run(fns)
}

// snapshot copies the block's voices and their settled gains out from
// under the lock and returns the shared reverb send. Callers hold mixMu.
func (m *Mixer) snapshot(out []float32) []float32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.scratch) < len(out) {
		m.scratch = make([]float32, len(out))
		m.sendBuf = make([]float32, len(out))
	}
	send := m.sendBuf[:len(out)]
	clear(send)
	if m.reverb != nil && m.pending {
		// The reverb's own settings are applied here rather than in the
		// setter, because process runs on this thread without the lock.
		m.reverb.set(m.applied)
		m.pending = false
	}
	m.blkReverb = m.reverb
	soloVoices, soloBuses := false, false
	for _, v := range m.voices {
		soloVoices = soloVoices || v.solo
	}
	m.revBuses = m.revBuses[:0]
	for _, b := range m.busList {
		soloBuses = soloBuses || b.solo
		if b.reverb == nil {
			continue
		}
		if b.pending {
			b.reverb.set(b.applied)
			b.pending = false
		}
		if len(b.sendBuf) < len(out) {
			b.sendBuf = make([]float32, len(out))
		}
		clear(b.sendBuf[:len(out)])
		m.revBuses = append(m.revBuses, busReverb{r: b.reverb, buf: b.sendBuf[:len(out)]})
	}
	m.snap = m.snap[:0]
	for _, v := range m.voices {
		if sn, ok := m.snapVoice(v, send, soloVoices, soloBuses); ok {
			m.snap = append(m.snap, sn)
		}
	}
	return send
}

// snapVoice copies one voice's block, or reports false when the voice has
// ended and takes no part in it. Callers hold the lock.
func (m *Mixer) snapVoice(v *Voice, send []float32, soloVoices, soloBuses bool) (voiceMix, bool) {
	sn := voiceMix{v: v, stream: v.stream, snd: v.snd, send: send, pos: v.pos,
		loop: v.loop, reverb: v.reverb, lp: v.lp, lpc: v.lpc, occ: v.occ, occc: v.occc,
		curL: v.curL, curR: v.curR, fade: v.fade, more: true}
	if v.stop || v.done {
		sn.more = false
		return sn, true
	}
	// A pause fades the voice out over the block it lands in, then holds
	// it in place until resumed; a voice that is already silent holds at
	// once, so a pause before the first block costs nothing.
	held := v.paused || m.paused || (v.bus != nil && v.bus.paused)
	if held && v.curL == 0 && v.curR == 0 {
		sn.skip = true
		return sn, true
	}
	sn.step = v.pitch
	if v.positional && m.doppler > 0 {
		sn.step *= m.listener.doppler(v.position, v.velocity, m.doppler, m.speedOfSound)
	}
	vol := v.vol
	if v.fade != nil {
		vol = v.fade.value()
	}
	gain := vol * m.master
	if v.bus != nil {
		gain *= v.bus.vol
		if v.bus.reverb != nil {
			sn.send = v.bus.sendBuf
		}
	}
	if held || v.silenced(soloVoices, soloBuses) {
		gain = 0
	}
	if v.occlusion > 0 {
		gain *= occlusionGain(v.occlusion)
	}
	pan := v.pan
	if v.positional {
		att, p := m.listener.attenuate(v.position, v.minDist, v.maxDist)
		gain *= att
		pan = p
	}
	pan = max(-1, min(1, pan))
	sn.tl = gain * sqrt32((1-pan)/2)
	sn.tr = gain * sqrt32((1+pan)/2)
	if !v.started {
		sn.curL, sn.curR = sn.tl, sn.tr
	}
	return sn, true
}

// apply writes each voice's block back, retires the ones that ended and
// hands over their callbacks. Callers hold mixMu.
func (m *Mixer) apply() []func() {
	m.mu.Lock()
	ended := false
	for i := range m.snap {
		sn := &m.snap[i]
		v := sn.v
		if !sn.skip && sn.frames > 0 {
			v.pos = sn.pos
			v.posPub.Store(math.Float64bits(sn.pos))
			if sn.stream != nil {
				v.played += int64(sn.frames)
				v.playedPub.Store(v.played)
			}
			v.curL, v.curR, v.started = sn.tl, sn.tr, true
			// A fade the game replaced mid-block belongs to the new fade,
			// not this one, so only the fade that was mixed is advanced.
			if f := v.fade; f != nil && f == sn.fade {
				f.left = max(f.left-sn.frames, 0)
				if f.left == 0 {
					v.vol = f.to
					v.fade = nil
					if f.stop {
						sn.more = false
					}
				}
			}
		}
		if !sn.more && !v.done {
			m.finish(v)
			ended = true
		}
	}
	if ended {
		live := m.voices[:0]
		for _, v := range m.voices {
			if !v.done {
				live = append(live, v)
			}
		}
		for i := len(live); i < len(m.voices); i++ {
			m.voices[i] = nil
		}
		m.voices = live
	}
	done := m.takeFinished()
	m.mu.Unlock()
	return done
}

// Voice is one playing sound or stream.
type Voice struct {
	m      *Mixer
	bus    *Bus
	snd    *Sound
	stream Stream
	pos    float64 // frames into the sound, fractional under pitch
	played int64   // frames taken from a stream since it started or last sought
	onDone func()
	// pos and played belong to the mixer's thread while a block runs, so
	// Position reads these copies instead of taking a lock behind it.
	posPub    atomic.Uint64 // pos as float64 bits
	playedPub atomic.Int64

	vol      float32
	pan      float32
	pitch    float32
	loop     bool
	done     bool
	stop     bool
	paused   bool
	priority int
	reverb   float32
	lp       *lowPass // the filter's running state, nil when unfiltered
	lpc      biquad   // its coefficients, which SetLowPass replaces
	fade     *fade
	mute     bool
	solo     bool

	occlusion float32
	occ       *lowPass // occlusion's own filter state, nil at 0
	occc      biquad

	positional       bool
	position         lin.Vec3
	velocity         lin.Vec3
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

// SetPaused holds the voice in place, silent, until resumed. The block
// the pause lands in fades out, so it never clicks.
func (v *Voice) SetPaused(p bool) { v.set(func() { v.paused = p }) }

// SetMute silences the voice while it keeps playing, so unmuting resumes
// wherever the sound has reached. To stop the sound advancing, use
// SetPaused instead.
func (v *Voice) SetMute(mute bool) { v.set(func() { v.mute = mute }) }

// Muted reports whether the voice is muted.
func (v *Voice) Muted() bool {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	return v.mute
}

// SetSolo solos the voice. While any voice is soloed, every voice that
// is not soloed is silent and keeps playing. Clearing the last solo
// makes them audible again. Bus solos are separate and combine with it.
func (v *Voice) SetSolo(solo bool) { v.set(func() { v.solo = solo }) }

// Soloed reports whether the voice is soloed.
func (v *Voice) Soloed() bool {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	return v.solo
}

// SetPosition moves a positional voice.
func (v *Voice) SetPosition(p lin.Vec3) { v.set(func() { v.position = p; v.positional = true }) }

// SetVelocity sets a positional voice's velocity in world units per
// second, for Doppler. It only changes the pitch; the game moves the
// voice with SetPosition.
func (v *Voice) SetVelocity(vel lin.Vec3) { v.set(func() { v.velocity = vel }) }

// SetOcclusion sets how blocked the path from the source is: 0 clear, 1
// fully blocked (20 dB down and muffled to 400 Hz), in between for a
// half-open door. A game sets it from a physics ray each frame; the gain
// ramps, so the change never clicks.
func (v *Voice) SetOcclusion(o float32) { v.set(func() { v.setOcclusion(o) }) }

// Occlusion reports the voice's occlusion amount.
func (v *Voice) Occlusion() float32 {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	return v.occlusion
}

// setOcclusion updates the amount and its filter under the lock.
func (v *Voice) setOcclusion(o float32) {
	o = max(0, min(1, o))
	v.occlusion = o
	switch {
	case o <= 0:
		v.occ = nil
	case v.occ == nil:
		v.occ, v.occc = &lowPass{}, newBiquad(occlusionCutoff(o), v.m.rate)
	default:
		v.occc.set(occlusionCutoff(o), v.m.rate)
	}
}

// SetReverb changes the send level into the bus's reverb, or the mixer's.
func (v *Voice) SetReverb(send float32) { v.set(func() { v.reverb = send }) }

// SetLowPass sets the low-pass cutoff in Hz; 0 removes the filter.
func (v *Voice) SetLowPass(cutoff float32) {
	v.set(func() {
		if cutoff <= 0 {
			v.lp = nil
		} else if v.lp == nil {
			v.lp, v.lpc = &lowPass{}, newBiquad(cutoff, v.m.rate)
		} else {
			v.lpc.set(cutoff, v.m.rate)
		}
	})
}

// FadeTo moves the volume to vol over seconds.
func (v *Voice) FadeTo(vol, seconds float32) {
	v.set(func() { v.startFade(vol, seconds, false) })
}

// startFade replaces the current fade. The caller holds the mixer lock.
func (v *Voice) startFade(vol, seconds float32, stop bool) {
	from := v.vol
	if v.fade != nil {
		from = v.fade.value()
	}
	total := int(seconds * float32(v.m.rate))
	v.fade = &fade{from: from, to: vol, left: total, total: total, stop: stop}
}

// FadeOut fades to silence over seconds and then stops the voice.
func (v *Voice) FadeOut(seconds float32) {
	v.set(func() { v.startFade(0, seconds, true) })
}

// Playing reports whether the voice is still audible.
func (v *Voice) Playing() bool {
	v.m.mu.Lock()
	defer v.m.mu.Unlock()
	return !v.done
}

// Position is how far into the sound the voice is, in seconds. For a
// stream it counts the frames taken since the voice started or last
// sought, which for Music is what the listener has heard. It reads the
// position the last finished block reached, so it takes no lock and
// never waits on the mixer.
func (v *Voice) Position() float64 {
	if v.stream != nil {
		return float64(v.playedPub.Load()) / float64(v.m.rate)
	}
	return math.Float64frombits(v.posPub.Load()) / float64(v.m.rate)
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
// otherwise it stays put and ErrNotSeekable is returned. Seeking waits
// for the block being mixed to finish, because it moves the position the
// mixer is reading from, so it may block for the length of one block; a
// Seeker must not call back into the mixer.
func (v *Voice) Seek(seconds float64) error {
	v.m.mixMu.Lock()
	defer v.m.mixMu.Unlock()
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
		v.playedPub.Store(v.played)
		return nil
	}
	if v.snd != nil {
		v.pos = min(seconds*float64(v.m.rate), float64(v.snd.Frames()))
		v.posPub.Store(math.Float64bits(v.pos))
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

// render accumulates the voice into out and its reverb send, using
// scratch for the dry signal. It runs with no lock held, so Stream.Read
// may take locks of its own and may call back into the mixer.
func (sn *voiceMix) render(scratch, out []float32, frames int) {
	if sn.skip || !sn.more {
		return
	}
	var n int
	var more bool
	if sn.stream != nil {
		n = sn.stream.Read(scratch[:frames*2])
		more = n == frames
	} else {
		n, more = sn.readSound(scratch[:frames*2])
	}
	sn.frames, sn.more = n, more
	if n == 0 {
		return
	}
	if sn.lp != nil {
		sn.lp.process(sn.lpc, scratch[:n*2])
	}
	if sn.occ != nil {
		sn.occ.process(sn.occc, scratch[:n*2])
	}
	dl := (sn.tl - sn.curL) / float32(n)
	dr := (sn.tr - sn.curR) / float32(n)
	l, r := sn.curL, sn.curR
	send, rev := sn.send, sn.reverb
	for i := range n {
		l += dl
		r += dr
		sl := scratch[i*2] * l
		sr := scratch[i*2+1] * r
		out[i*2] += sl
		out[i*2+1] += sr
		if rev > 0 {
			send[i*2] += sl * rev
			send[i*2+1] += sr * rev
		}
	}
}

// silenced reports whether mute or solo state keeps the voice quiet this
// block: its own mute, another voice's solo, or its bus being muted or
// passed over by a bus solo.
func (v *Voice) silenced(soloVoices, soloBuses bool) bool {
	if v.mute || (soloVoices && !v.solo) {
		return true
	}
	if v.bus == nil {
		return soloBuses
	}
	return v.bus.mute || (soloBuses && !v.bus.solo)
}

// readSound copies the next frames of the sound into dst, advancing the
// block's step source frames per output frame (the pitch, with Doppler
// applied), and reports how many it wrote and whether more remain.
func (sn *voiceMix) readSound(dst []float32) (int, bool) {
	s := sn.snd
	if s == nil || len(s.samples) < 2 {
		return 0, false
	}
	total := len(s.samples) / 2
	frames := len(dst) / 2
	pos, step, loop := sn.pos, float64(sn.step), sn.loop
	for i := range frames {
		if pos >= float64(total) {
			if !loop {
				sn.pos = pos
				return i, false
			}
			// A step longer than the sound (a high pitch on a short
			// sample) can pass the end more than once.
			pos = math.Mod(pos, float64(total))
		}
		j := min(int(pos), total-1)
		t := float32(pos - float64(j))
		k := j + 1
		if k >= total {
			if loop {
				k = 0
			} else {
				k = j
			}
		}
		dst[i*2] = s.samples[j*2]*(1-t) + s.samples[k*2]*t
		dst[i*2+1] = s.samples[j*2+1]*(1-t) + s.samples[k*2+1]*t
		pos += step
	}
	sn.pos = pos
	return frames, true
}
