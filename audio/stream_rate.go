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
	if sn.step == 1 && (r.phase == 0 || r.phase == 1) {
		return sn.copyStream(dst)
	}
	return sn.resampleStream(dst)
}

// start reads the first two frames, or reports false when the stream has
// none.
func (r *streamRate) start(s Stream) bool {
	if !r.started {
		var ok bool
		r.a, ok, r.aReal = r.frame(s)
		if !ok {
			return false
		}
		r.b, r.next, r.bReal = r.frame(s)
		r.started = true
	}
	return true
}

// copyStream is resampleStream at a step of exactly 1 from a whole phase,
// where every output frame is a source frame. Instead of taking one frame
// at a time from the lookahead buffer, it copies runs straight out of it,
// leaving the state exactly where resampleStream would. Each sample is
// still written as a + (b-a)*0, the resampler's interpolation at phase
// zero, so a negative zero or a nonfinite sample comes out bit for bit
// the same.
func (sn *voiceMix) copyStream(dst []float32) (int, bool) {
	r := &sn.v.streamRate
	if !r.start(sn.stream) {
		return 0, false
	}
	frames := len(dst) / 2
	i := 0
	for i < frames {
		if r.phase < 1 || r.read == r.count || !r.next {
			// The first frame of a stream, a refill of the buffer, or the
			// end: one frame the resampler's way.
			if r.phase >= 1 {
				if !r.next {
					return i, false
				}
				r.a, r.aReal = r.b, r.bReal
				r.b, r.next, r.bReal = r.frame(sn.stream)
				r.phase--
			}
			b := r.b
			if !r.next {
				b = r.a
			}
			dst[i*2] = r.a[0] + (b[0]-r.a[0])*0
			dst[i*2+1] = r.a[1] + (b[1]-r.a[1])*0
			r.phase++
			sn.pos++
			i++
			continue
		}
		// a has been played and b is next, with k frames buffered after
		// it: emit b and the k-1 frames after it, each followed by the
		// next, and leave the last two as a and b.
		k := min(frames-i, r.count-r.read)
		prev := r.b
		for j := range k {
			at := (r.read + j) * 2
			f0, f1 := r.buf[at], r.buf[at+1]
			dst[(i+j)*2] = prev[0] + (f0-prev[0])*0
			dst[(i+j)*2+1] = prev[1] + (f1-prev[1])*0
			if j < k-1 {
				prev = [2]float32{f0, f1}
			}
			sn.pos++
		}
		if k > 1 {
			r.a, r.aReal = prev, r.read+k-2 < r.realCount
		} else {
			r.a, r.aReal = r.b, r.bReal
		}
		last := (r.read + k - 1) * 2
		r.b, r.bReal = [2]float32{r.buf[last], r.buf[last+1]}, r.read+k-1 < r.realCount
		r.read += k
		i += k
	}
	return frames, true
}

// resampleStream interpolates the stream at the block's step.
func (sn *voiceMix) resampleStream(dst []float32) (int, bool) {
	r := &sn.v.streamRate
	if !r.start(sn.stream) {
		return 0, false
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
