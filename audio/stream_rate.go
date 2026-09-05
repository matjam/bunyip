package audio

// streamRate owns the lookahead and fractional phase for one voice.
// Only the playback lock protects it; Stream.Read may call mixer setters.
type streamRate struct {
	buf           [1024]float32
	read, count   int
	eof           bool
	a, b          [2]float32
	started, next bool
	phase         float64
	revision      uint64
	realCount     int
	aReal, bReal  bool
}

// A Music seek or loop change invalidates samples buffered by a voice.
type streamRevision interface{ streamRevision() uint64 }

// Music distinguishes lookahead from playback when toggling a loop.
type streamLookahead interface{ streamLookahead(uint64, float64) }

// Music reports the source-backed prefix separately from underrun silence.
type streamSourceRead interface {
	readSource([]float32, uint64) (int, int)
}

func (r *streamRate) frame(s Stream) ([2]float32, bool, bool) {
	if r.read == r.count {
		if r.eof {
			return [2]float32{}, false, false
		}
		r.read = 0
		if source, ok := s.(streamSourceRead); ok {
			r.count, r.realCount = source.readSource(r.buf[:], r.revision)
		} else {
			r.count = s.Read(r.buf[:])
			r.realCount = r.count
		}
		if r.count < 0 || r.count > len(r.buf)/2 {
			r.count = 0
		}
		r.eof = r.count < len(r.buf)/2
		if r.count == 0 {
			return [2]float32{}, false, false
		}
	}
	f := [2]float32{r.buf[r.read*2], r.buf[r.read*2+1]}
	real := r.read < r.realCount
	r.read++
	return f, true, real
}

func (r *streamRate) sourceLookahead() float64 {
	if !r.started {
		return 0
	}
	n := max(0, float64(r.realCount-r.read)-max(0, r.phase-2))
	if r.aReal {
		n += max(0, 1-r.phase)
	}
	if r.next && r.bReal {
		n += min(1, max(0, 2-r.phase))
	}
	return n
}

func (sn *voiceMix) readStream(dst []float32) (int, bool) {
	r := &sn.v.streamRate
	if s, ok := sn.stream.(streamRevision); ok {
		revision := s.streamRevision()
		if revision != r.revision {
			*r = streamRate{revision: revision}
		}
	}
	if s, ok := sn.stream.(streamLookahead); ok {
		defer func() { s.streamLookahead(r.revision, r.sourceLookahead()) }()
	}
	if !r.started {
		var ok bool
		r.a, ok, r.aReal = r.frame(sn.stream)
		if !ok {
			return 0, false
		}
		r.b, r.next, r.bReal = r.frame(sn.stream)
		r.started = true
	}
	step := float64(sn.step)
	for i := range len(dst) / 2 {
		for r.phase >= 1 {
			if !r.next {
				return i, false
			}
			r.a = r.b
			r.aReal = r.bReal
			r.b, r.next, r.bReal = r.frame(sn.stream)
			r.phase--
		}
		b := r.b
		if !r.next {
			b = r.a
		}
		f := float32(r.phase)
		dst[i*2] = r.a[0] + (b[0]-r.a[0])*f
		dst[i*2+1] = r.a[1] + (b[1]-r.a[1])*f
		r.phase += step
		sn.pos += step
	}
	return len(dst) / 2, true
}
