package audio

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// The binaural spatialiser. A positional voice is rendered for each ear
// from a parametric head model rather than a measured head-related
// transfer function: the two ears hear the same signal at different
// times (the interaural time difference, from Woodworth's formula), at
// different levels (the interaural level difference), with the far ear
// dulled by the head between it and the source (a one-pole low-pass
// whose cutoff falls with the angle), and with a shelf that lifts or
// drops the high band as the source rises and falls, which is the only
// elevation cue the model has. Sources in front and behind at the same
// angle sound the same, as they do in any model without ear shape.
//
// Every parameter is worked out per block and interpolated across it, so
// a moving source glides. The state lives on the voice and is reused
// block to block, so mixing allocates nothing.

const (
	defaultHeadRadius = 0.0875 // metres, an average adult head
	maxHeadRadius     = 0.3    // metres, the largest the delay line has room for
	airSpeedOfSound   = 343.0  // metres per second, for the ear delays

	// The head shadow's cutoff falls from 22 kHz for a source at the ear
	// to 1.5 kHz for one on the far side, geometrically in between.
	shadowOpenHz  = 22000
	shadowClosedR = 0.07

	// The elevation shelf splits the signal at 4 kHz and scales the high
	// band by up to elevationDepth either way.
	shelfHz        = 4000
	elevationDepth = 0.6

	// The far ear loses this many decibels broadband; the rest of the
	// level difference comes from the head shadow.
	shadowLevelDB = 6
)

// earParams is one positional voice's head model for one block: what the
// mixer interpolates from the values the last block ended on.
type earParams struct {
	delayL, delayR   float32 // ear delays in frames, the near ear at zero
	shadowL, shadowR float32 // one-pole coefficients for the head shadow
	tilt             float32 // gain on the high band, for elevation
	gainL, gainR     float32 // level difference, folded into the block's gains
}

// binaural is a positional voice's head-model state, owned by the
// mixer's thread. The ring is the delay line the two ears tap at
// different distances.
type binaural struct {
	ring []float32
	w    int // where the next input sample goes

	lpL, lpR float32 // head-shadow filter state per ear
	low      float32 // the elevation shelf's low band
	shelf    float32 // the shelf's one-pole coefficient, fixed by the rate

	cur     earParams // where the last block's interpolation ended
	started bool      // cur holds real values
}

// newBinaural makes the state for one voice at the mixer's rate. The
// delay line has room for the largest head the settings allow.
func newBinaural(rate int) *binaural {
	frames := int(maxHeadRadius/airSpeedOfSound*(math.Pi/2+1)*float64(rate)) + 4
	return &binaural{ring: make([]float32, frames), shelf: onePole(shelfHz, rate)}
}

// onePole is the coefficient of a one-pole low-pass at cutoff Hz, which
// is 1 (no filtering) once the cutoff passes what the rate can carry.
func onePole(cutoff float32, rate int) float32 {
	return min(1, 1-float32(math.Exp(-2*math.Pi*float64(cutoff)/float64(rate))))
}

// tap reads the delay line d frames back from w, interpolating between
// the two samples either side so a moving source glides instead of
// stepping. Callers write the current sample at w first, so a delay of
// zero reads what was just written.
func tap(ring []float32, w int, d float32) float32 {
	n := len(ring)
	p := float32(w) - d
	i := int(p)
	frac := p - float32(i)
	if frac < 0 {
		i--
		frac++
	}
	if i %= n; i < 0 {
		i += n
	}
	j := i + 1
	if j == n {
		j = 0
	}
	return ring[i]*(1-frac) + ring[j]*frac
}

// headRadius is the modelled head's radius in metres.
func (s SpatialSettings) headRadius() float32 {
	if s.HeadRadius <= 0 {
		return defaultHeadRadius
	}
	return min(s.HeadRadius, maxHeadRadius)
}

// headModel works out the head model for a source at p. The lateral
// angle is measured from the plane through both ears, so a source
// overhead reaches the ears together however far forward it is, and the
// elevation is measured against the listener's up.
func (l Listener) headModel(p lin.Vec3, radius float32, rate int) earParams {
	var e earParams
	fwd := l.Forward.Norm()
	right := fwd.Cross(l.Up).Norm()
	up := right.Cross(fwd)
	var x, y float32
	if d := p.Sub(l.Position); d.Len() > 1e-6 {
		dir := d.Mul(1 / d.Len())
		x = max(-1, min(1, dir.Dot(right)))
		y = max(-1, min(1, dir.Dot(up)))
	}

	// Woodworth: the extra path around a sphere of this radius, up to
	// about 0.66 ms for an average head with the source at one ear.
	theta := float64(math.Asin(float64(x)))
	itd := float32(float64(radius) / airSpeedOfSound * (theta + math.Sin(theta)) * float64(rate))
	e.delayL, e.delayR = max(0, itd), max(0, -itd)

	// The far ear is quieter broadband and duller, the near ear is left
	// alone; both are scaled to match the pan law's level in front.
	far := float32(math.Pow(10, -shadowLevelDB/20*math.Abs(float64(x))))
	const centre = math.Sqrt2 / 2
	e.gainL, e.gainR = centre, centre
	if x >= 0 {
		e.gainL *= far
	} else {
		e.gainR *= far
	}
	e.shadowL = onePole(shadowOpenHz*pow32(shadowClosedR, max(0, x)), rate)
	e.shadowR = onePole(shadowOpenHz*pow32(shadowClosedR, max(0, -x)), rate)

	// Elevation: brighter above, darker below, nothing at ear level.
	e.tilt = 1 + elevationDepth*y
	return e
}

func pow32(base, exp float32) float32 {
	return float32(math.Pow(float64(base), float64(exp)))
}

// renderBinaural mixes n frames of the voice into out through the head
// model, ramping the block's gains and gliding every head parameter from
// where the last block left it. It allocates nothing.
func (sn *voiceMix) renderBinaural(scratch, out []float32, n int) {
	b := sn.bin
	t := sn.ear
	if !b.started {
		// The first block of a voice, or the first after the mode
		// changed, starts where it means to go and with an empty delay
		// line, so no stale audio and no glide from nowhere.
		clear(b.ring)
		b.cur, b.started = t, true
	}
	p := b.cur
	inv := 1 / float32(n)
	dDelayL, dDelayR := (t.delayL-p.delayL)*inv, (t.delayR-p.delayR)*inv
	dShadowL, dShadowR := (t.shadowL-p.shadowL)*inv, (t.shadowR-p.shadowR)*inv
	dTilt := (t.tilt - p.tilt) * inv
	gl, gr := sn.curL, sn.curR
	dl, dr := (sn.tl-gl)*inv, (sn.tr-gr)*inv
	send, rev := sn.send, sn.reverb
	ring, w, shelf := b.ring, b.w, b.shelf
	low, lpL, lpR := b.low, b.lpL, b.lpR
	for i := range n {
		p.delayL += dDelayL
		p.delayR += dDelayR
		p.shadowL += dShadowL
		p.shadowR += dShadowR
		p.tilt += dTilt
		gl += dl
		gr += dr

		// The head model is fed one signal, so a stereo source collapses.
		x := (scratch[i*2] + scratch[i*2+1]) * 0.5
		low += shelf * (x - low)
		ring[w] = low + p.tilt*(x-low)
		l, r := tap(ring, w, p.delayL), tap(ring, w, p.delayR)
		if w++; w == len(ring) {
			w = 0
		}
		lpL += p.shadowL * (l - lpL)
		lpR += p.shadowR * (r - lpR)
		sl, sr := lpL*gl, lpR*gr
		out[i*2] += sl
		out[i*2+1] += sr
		if rev > 0 {
			send[i*2] += sl * rev
			send[i*2+1] += sr * rev
		}
	}
	b.w, b.low, b.lpL, b.lpR = w, low, lpL, lpR
	b.cur = t
}
