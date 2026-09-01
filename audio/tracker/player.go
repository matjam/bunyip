package tracker

// Player renders a Module to stereo float32 at a fixed rate. It implements
// audio.Stream. Player state is only touched by Read, so it lives on the
// mixer's thread once playing; set Loop before that.
type Player struct {
	Loop bool

	mod   *Module
	rate  int
	chans []channel

	order, row    int
	speed, tempo  int
	tick          int
	tickFrames    int // frames per tick at the current tempo
	tickRemaining int
	globalVol     int
	patDelay      int // extra repeats of the current row
	jumpOrder     int // -1 none
	breakRow      int // -1 none
	loopRow       int
	loopCount     int
	loopJump      bool
	done          bool
	started       bool
	gain          float32
}

type channel struct {
	sample    *Sample
	pos       float64
	period    float64 // current base period
	outPeriod float64 // with vibrato/arpeggio applied for this tick
	note      int
	volume    int
	outVolume int
	pan       float32
	c4speed   int
	finetune  int
	playing   bool

	portaTarget float64
	portaSpeed  byte
	vibPos      int
	vibSpeed    byte
	vibDepth    byte
	vibWave     int
	tremPos     int
	tremSpeed   byte
	tremDepth   byte
	tremWave    int
	arpParam    byte
	volSlide    byte
	offset      byte
	retrig      byte
	retrigCount int
	tremorParam byte
	tremorCount int
	tremorOff   bool
	noteCut     int
	noteDelay   int
	delayed     Cell
	s3mMem      byte // shared D/E/F/K/L/Q/etc memory as ST3 does
}

// NewPlayer prepares a module for playback at rate Hz.
func NewPlayer(m *Module, rate int) *Player {
	p := &Player{mod: m, rate: rate, jumpOrder: -1, breakRow: -1}
	p.chans = make([]channel, m.Channels)
	for i := range p.chans {
		p.chans[i].pan = m.Pan[i]
		p.chans[i].noteCut, p.chans[i].noteDelay = -1, -1
	}
	p.speed, p.tempo = m.Speed, m.Tempo
	p.globalVol = m.GlobalVolume
	if p.globalVol <= 0 {
		p.globalVol = 64
	}
	p.gain = 0.5 / sqrtf(float32(max(m.Channels, 1)))
	p.setTempo(p.tempo)
	return p
}

// Position reports the current song position and row.
func (p *Player) Position() (order, row int) { return p.order, p.row }

// Finished reports whether a non-looping song has ended.
func (p *Player) Finished() bool { return p.done }

func (p *Player) setTempo(bpm int) {
	if bpm < 32 {
		bpm = 32
	}
	p.tempo = bpm
	p.tickFrames = max(1, int(float64(p.rate)*2.5/float64(bpm)))
}

// Read fills out and returns the frames written (audio.Stream).
func (p *Player) Read(out []float32) int {
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
		p.tick = 0
		p.processRow()
		return
	}
	p.tick++
	if p.tick < p.speed {
		p.processTick()
		return
	}
	p.tick = 0
	if p.patDelay > 0 {
		p.patDelay--
		p.processRow()
		return
	}
	p.nextRow()
	if !p.done {
		p.processRow()
	}
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
		pat := p.mod.Orders[p.order]
		if pat >= len(p.mod.Patterns) || p.row >= len(p.mod.Patterns[pat].Rows) {
			p.order++
			p.row = 0
			continue
		}
		return
	}
}

func (p *Player) currentRow() []Cell {
	return p.mod.Patterns[p.mod.Orders[p.order]].Rows[p.row]
}

func sqrtf(v float32) float32 {
	// Newton iterations are plenty for a gain constant.
	x := v
	for range 8 {
		x = 0.5 * (x + v/x)
	}
	return x
}
