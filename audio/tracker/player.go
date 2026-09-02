package tracker

import (
	"math"
	"sync"
)

// Player renders a Module to stereo float32 at a fixed rate. It implements
// audio.Stream. Read runs on the mixer's thread once the player is
// playing; Seek, Position, Mute and Solo take the same lock, so the game
// loop can call them while it plays. Set Loop, AmigaFilter and Cubic
// before playing.
type Player struct {
	Loop bool
	// AmigaFilter enables the ProTracker LED low-pass filter (E00/E01).
	// Off by default: most players and listeners expect the unfiltered mix.
	AmigaFilter bool
	// Cubic switches sample interpolation from linear to four-point
	// Hermite, which rounds off the aliasing that makes chip samples sound
	// harsher than in other players. Off by default to match the classic
	// sound.
	Cubic bool

	mu    sync.Mutex
	mod   *Module
	rate  int
	chans []channel
	bg    []*voice // voices detached from their channel by a new-note action
	// bgFree holds background voices that have faded out, so a new note
	// on the audio thread takes one instead of allocating. A voice is
	// overwritten whole when it is taken, so nothing carries over.
	bgFree []*voice
	mute   []bool // per channel
	solo   []bool // per channel
	solos  int    // how many channels are soloed

	order, row    int
	speed, tempo  int
	tick          int
	tickFrames    int
	tickRemaining int
	globalVol     int // 0..128
	patDelay      int
	jumpOrder     int
	breakRow      int
	loopRow       int
	loopCount     int
	loopJump      bool
	done          bool
	started       bool
	gain          float32
	amigaFilter   bool
	filterL       lowpass
	filterR       lowpass
	rows          int // rows in the current pattern
}

// voice is one sounding sample with its own envelopes and fade.
type voice struct {
	sample *Sample
	inst   *Instrument
	pos    float64
	dir    int // +1, or -1 while a ping-pong loop runs backwards
	active bool
	index  int // the channel it started on, for mute and solo

	period    float64 // base pitch after slides
	note      int     // note index as the format counts it
	volume    int     // 0..64 from the volume column and slides
	pan       float32
	keyOff    bool
	fading    bool
	fade      int // 65536 = full
	volEnv    envState
	panEnv    envState
	pitchEnv  envState
	autoVib   autoVibState
	cutoff    int // IT filter, 0..127; 127 means off
	resonance int
	filter    lowpass
	filterOn  bool
	sustainOn bool // IT sustain loop still engaged
}

type channel struct {
	voice
	pan      float32 // channel default pan
	chanVol  int     // IT channel volume 0..64
	lastInst int
	lastNote int

	outPeriod float64
	outVolume int
	outPan    float32

	portaTarget   float64
	portaSpeed    int
	vibPos        int
	vibSpeed      int
	vibDepth      int
	vibWave       int
	tremPos       int
	tremSpeed     int
	tremDepth     int
	tremWave      int
	panbrPos      int
	panbrSpeed    int
	panbrDepth    int
	arpParam      byte
	volSlide      byte
	panSlide      byte
	chanVolSlide  byte
	globalSlide   byte
	offset        byte
	offsetHigh    int
	retrig        byte
	retrigCount   int
	tremorParam   byte
	tremorCount   int
	tremorOff     bool
	noteCut       int
	noteDelay     int
	delayed       Cell
	keyOffTick    int
	mem           byte // shared effect memory (S3M/IT)
	portaMem      byte
	vibMem        byte
	pendingSample *Sample // MOD: instrument change waiting for the loop to wrap
	finetune      float32
	glissando     bool
}

type envState struct {
	tick  int
	value float32
	done  bool
}

type autoVibState struct {
	pos   int
	sweep int
}

// NewPlayer prepares a module for playback at rate Hz.
func NewPlayer(m *Module, rate int) *Player {
	p := &Player{mod: m, rate: rate, jumpOrder: -1, breakRow: -1,
		mute: make([]bool, m.Channels), solo: make([]bool, m.Channels)}
	p.chans = make([]channel, m.Channels)
	for i := range p.chans {
		ch := &p.chans[i]
		ch.index = i
		ch.pan = m.Pan[i]
		ch.voice.pan = m.Pan[i]
		ch.chanVol = 64
		if m.ChannelVol != nil {
			ch.chanVol = m.ChannelVol[i]
		}
		ch.noteCut, ch.noteDelay, ch.keyOffTick = -1, -1, -1
		ch.cutoff, ch.resonance = 127, 0
		ch.dir = 1
	}
	p.speed, p.tempo = m.Speed, m.Tempo
	p.globalVol = m.GlobalVolume
	if p.globalVol <= 0 {
		p.globalVol = 128
	}
	// Per-channel gain follows the file's mixing volume, as the reference
	// players do, rather than the channel count: 48/128 puts four channels
	// at full volume just short of clipping, which is the Amiga's balance.
	mix := m.MixVolume
	if mix <= 0 {
		mix = 48
	}
	p.gain = 2.0 / 3 * float32(mix) / 128
	switch m.Format {
	case FormatIT:
		// Impulse Tracker's mixing volume sits four times lower on the same
		// scale; matched against libopenmpt renders.
		p.gain /= 4
	case FormatS3M:
		p.gain *= 0.8 // likewise matched: ScreamTracker mixes about 2 dB lower
	}
	p.setTempo(p.tempo)
	p.filterL.setCutoff(3200, 0.7, rate)
	p.filterR.setCutoff(3200, 0.7, rate)
	return p
}

// Position reports the current song position (an index into the order
// list) and the row within its pattern.
func (p *Player) Position() (order, row int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.order, p.row
}

// Length is the number of song positions in the order list, the range
// Seek accepts.
func (p *Player) Length() int { return len(p.mod.Orders) }

// Rows is the number of rows in the pattern at a song position, or 0 for
// a position outside the song or one that names no pattern.
func (p *Player) Rows(order int) int {
	if order < 0 || order >= len(p.mod.Orders) {
		return 0
	}
	pat := p.mod.Orders[order]
	if pat < 0 || pat >= len(p.mod.Patterns) {
		return 0
	}
	return len(p.mod.Patterns[pat].Rows)
}

// Channels is the number of pattern channels, the range Mute and Solo
// accept.
func (p *Player) Channels() int { return len(p.chans) }

// Seek jumps to a song position and row and plays on from there, cutting
// whatever was sounding. Order is clamped to the song and row to the
// pattern; a position that names no pattern moves on to the next that
// does. Speed, tempo and global volume stay as they are, so a song that
// changes them along the way plays from the new place at whatever was in
// force. Seeking a finished song starts it again.
func (p *Player) Seek(order, row int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.mod.Orders) == 0 {
		return
	}
	order = max(0, min(order, len(p.mod.Orders)-1))
	row = max(0, min(row, max(p.Rows(order)-1, 0)))
	p.order, p.row = order, row
	p.tick, p.tickRemaining = 0, 0
	p.patDelay, p.jumpOrder, p.breakRow = 0, -1, -1
	p.loopRow, p.loopCount, p.loopJump = 0, 0, false
	p.done, p.started = false, false
	p.bg = p.recycle(p.bg)
	for i := range p.chans {
		ch := &p.chans[i]
		ch.active = false
		ch.pendingSample = nil
		ch.noteCut, ch.noteDelay, ch.keyOffTick = -1, -1, -1
	}
}

// Mute silences a pattern channel while the song plays on, as a tracker's
// channel mute does; notes it starts still run their course silently, so
// unmuting is seamless. Channels outside the module are ignored.
func (p *Player) Mute(channel int, mute bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if channel >= 0 && channel < len(p.mute) {
		p.mute[channel] = mute
	}
}

// Muted reports whether a channel is muted.
func (p *Player) Muted(channel int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return channel >= 0 && channel < len(p.mute) && p.mute[channel]
}

// Solo auditions a channel: while any channel is soloed, only soloed
// channels are heard. Clearing the last solo brings the rest back.
func (p *Player) Solo(channel int, solo bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if channel < 0 || channel >= len(p.solo) || p.solo[channel] == solo {
		return
	}
	p.solo[channel] = solo
	if solo {
		p.solos++
	} else {
		p.solos--
	}
}

// Soloed reports whether a channel is soloed.
func (p *Player) Soloed(channel int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return channel >= 0 && channel < len(p.solo) && p.solo[channel]
}

// audible reports whether a channel is heard under the mute and solo
// state. Callers hold the lock.
func (p *Player) audible(channel int) bool {
	if channel < 0 || channel >= len(p.mute) {
		return true
	}
	return !p.mute[channel] && (p.solos == 0 || p.solo[channel])
}

// Finished reports whether a non-looping song has ended.
func (p *Player) Finished() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

func (p *Player) setTempo(bpm int) {
	if bpm < 32 {
		bpm = 32
	}
	p.tempo = bpm
	p.tickFrames = max(1, int(float64(p.rate)*2.5/float64(bpm)))
}

// Read fills out and returns the frames written (audio.Stream).
func (p *Player) Read(out []float32) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	frames := len(out) / 2
	written := 0
	for written < frames && !p.done {
		if p.tickRemaining == 0 {
			p.advanceTick()
			if p.done {
				break
			}
			p.tickRemaining = p.tickFrames
		}
		n := min(frames-written, p.tickRemaining)
		p.render(out[written*2 : (written+n)*2])
		written += n
		p.tickRemaining -= n
	}
	return written
}

// advanceTick moves to the next tick, processing a new row when needed.
func (p *Player) advanceTick() {
	if !p.started {
		p.started = true
		if len(p.mod.Orders) == 0 || len(p.mod.Patterns) == 0 {
			p.done = true
			return
		}
		if !p.validPosition() {
			p.nextRow()
			if p.done {
				return
			}
		}
		p.tick = 0
		p.processRow()
		p.updateVoices()
		return
	}
	p.tick++
	if p.tick < p.speed {
		p.processTick()
		p.updateVoices()
		return
	}
	p.tick = 0
	if p.patDelay > 0 {
		p.patDelay--
		p.processRow()
		p.updateVoices()
		return
	}
	p.nextRow()
	if !p.done {
		p.processRow()
		p.updateVoices()
	}
}

func (p *Player) validPosition() bool {
	if p.order >= len(p.mod.Orders) {
		return false
	}
	pat := p.mod.Orders[p.order]
	return pat < len(p.mod.Patterns) && p.row < len(p.mod.Patterns[pat].Rows)
}

// nextRow applies pending jumps, breaks and loops, then steps the row.
func (p *Player) nextRow() {
	switch {
	case p.loopJump:
		p.loopJump = false
		p.row = p.loopRow
	case p.jumpOrder >= 0 || p.breakRow >= 0:
		if p.jumpOrder >= 0 {
			p.order = p.jumpOrder
		} else {
			p.order++
		}
		p.row = max(p.breakRow, 0)
		p.jumpOrder, p.breakRow = -1, -1
		p.loopRow, p.loopCount = 0, 0
	default:
		p.row++
	}
	for {
		if p.order >= len(p.mod.Orders) {
			if !p.Loop {
				p.done = true
				return
			}
			p.order = min(p.mod.Restart, len(p.mod.Orders)-1)
			p.row = 0
		}
		if p.validPosition() {
			return
		}
		p.order++
		p.row = 0
	}
}

func (p *Player) currentRow() []Cell {
	return p.mod.Patterns[p.mod.Orders[p.order]].Rows[p.row]
}

// slideUnit is one unit of portamento in the format's period space.
func (p *Player) slideUnit() float64 {
	if p.mod.Format == FormatMOD {
		return 1
	}
	return 4
}

// notePeriod converts a note index to the period of the channel's sample.
func (p *Player) notePeriod(v *voice, note int) float64 {
	s := v.sample
	if s == nil {
		return 0
	}
	m := p.mod
	switch m.Format {
	case FormatMOD:
		return modPeriod(note, s.Finetune)
	case FormatS3M:
		return s3mPeriod(note, s.C4Speed)
	}
	var semis float64
	if m.Format == FormatXM {
		semis = float64(note+s.RelativeNote) + float64(s.Finetune)
	} else {
		c5 := float64(s.C4Speed)
		if c5 <= 0 {
			c5 = 8363
		}
		semis = float64(note-12) + 12*math.Log2(c5/8363)
	}
	if m.LinearSlides {
		return linearPeriod(semis)
	}
	return amigaPeriodXM(semis)
}

// hasMemory reports whether a zero parameter recalls the previous one for
// the effect; ProTracker slides have no memory.
func (p *Player) hasMemory() bool { return p.mod.Format != FormatMOD }
