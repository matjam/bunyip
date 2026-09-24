package audio

// Performance benchmarks. They measure a 512-frame block with 64 voices
// in the configurations a game uses, the lock hold time on the audio
// thread, the worst block under setter contention, the denormal exposure
// of the filters and the reverb, and the music decoders' CPU per second
// of playback. The long-running reports are tests gated on
// BUNYIP_AUDIT=1 so the normal suite does not pay for them.

import (
	"bytes"
	"io"
	"math"
	"os"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matjam/bunyip/lin"
)

const perfVoices = 64

type perfMode struct {
	name       string
	positional bool
	binaural   bool
	reverb     bool
	occluded   bool
	doppler    bool
}

var perfModes = []perfMode{
	{name: "dry"},
	{name: "positional", positional: true},
	{name: "binaural", positional: true, binaural: true},
	{name: "positional_reverb_occluded", positional: true, reverb: true, occluded: true},
	{name: "binaural_reverb_occluded", positional: true, binaural: true, reverb: true, occluded: true},
	{name: "binaural_reverb_occluded_doppler", positional: true, binaural: true, reverb: true, occluded: true, doppler: true},
}

// perfMixer builds a mixer with 64 looping voices in the given mode.
func perfMixer(tb testing.TB, mode perfMode) (*Mixer, []*Voice) {
	tb.Helper()
	m := NewMixer(benchRate)
	snd, err := m.NewSound(Sine(440, 2, benchRate))
	if err != nil {
		tb.Fatal(err)
	}
	if mode.binaural {
		m.SetSpatial(SpatialSettings{Binaural: true})
	}
	if mode.reverb {
		m.SetReverb(ReverbSettings{RoomSize: 0.8, Wet: 0.3})
	}
	if mode.doppler {
		m.SetDoppler(1)
	}
	voices := make([]*Voice, 0, perfVoices)
	for i := range perfVoices {
		opts := PlayOptions{Loop: true, Bus: m.Effects(), Volume: 0.2}
		if mode.positional {
			a := float64(i) / perfVoices * 2 * math.Pi
			opts.Positional = true
			opts.Position = lin.Vec3{X: float32(10 * math.Cos(a)), Y: float32(i%5) - 2, Z: float32(10 * math.Sin(a))}
			opts.Velocity = lin.Vec3{X: 3}
		}
		if mode.reverb {
			opts.Reverb = 0.4
		}
		if mode.occluded {
			opts.Occlusion = 0.5
		}
		// Staggered pitches so no two voices read the same frames.
		opts.Pitch = 1 + float32(i%7)*0.01
		voices = append(voices, m.Play(snd, opts))
	}
	return m, voices
}

// BenchmarkMix64 mixes one 512-frame block with 64 voices. The block's
// budget at 48 kHz is 10.67 ms.
func BenchmarkMix64(b *testing.B) {
	for _, mode := range perfModes {
		b.Run(mode.name, func(b *testing.B) {
			m, _ := perfMixer(b, mode)
			out := make([]float32, benchBlock*2)
			m.mix(out)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				m.mix(out)
			}
		})
	}
}

// BenchmarkSnapshot64 times the part of a block that holds the settings
// lock (m.mu): snapshot and apply. A setter on the game thread can wait
// this long.
func BenchmarkSnapshot64(b *testing.B) {
	for _, mode := range perfModes {
		b.Run(mode.name, func(b *testing.B) {
			m, _ := perfMixer(b, mode)
			out := make([]float32, benchBlock*2)
			m.mix(out)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				m.mixMu.Lock()
				m.snapshot(out)
				for i := range m.snap {
					// Pretend each voice rendered the block so apply does
					// its real work.
					m.snap[i].frames = benchBlock
				}
				m.apply()
				m.mixMu.Unlock()
			}
		})
	}
}

// perfStream is a minimal generator Stream: a sine, no locks.
type perfStream struct{ phase, step float32 }

func (s *perfStream) Read(out []float32) int {
	for i := 0; i+1 < len(out); i += 2 {
		v := float32(math.Sin(float64(s.phase)))
		out[i], out[i+1] = v, v
		s.phase += s.step
		if s.phase > 2*math.Pi {
			s.phase -= 2 * math.Pi
		}
	}
	return len(out) / 2
}

// perfNullStream writes silence, so the benchmark isolates the mixer's
// stream path (the resampler and lookahead) from the generator.
type perfNullStream struct{}

func (perfNullStream) Read(out []float32) int { clear(out); return len(out) / 2 }

// BenchmarkStream64 mixes 64 stream voices at pitch 1.
func BenchmarkStream64(b *testing.B) {
	m := NewMixer(benchRate)
	for range perfVoices {
		m.PlayStream(perfNullStream{}, PlayOptions{Bus: m.Effects(), Volume: 0.2})
	}
	out := make([]float32, benchBlock*2)
	m.mix(out)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.mix(out)
	}
}

// BenchmarkNewBinauralVoice starts one positional voice per block with
// binaural rendering on, which needs head-model state for each voice.
func BenchmarkNewBinauralVoice(b *testing.B) {
	m := NewMixer(benchRate)
	m.SetSpatial(SpatialSettings{Binaural: true})
	m.SetMaxVoices(16)
	snd, err := m.NewSound(Sine(440, 0.05, benchRate))
	if err != nil {
		b.Fatal(err)
	}
	out := make([]float32, benchBlock*2)
	vs := make([]*Voice, 0, 1<<20)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		vs = append(vs[:0], m.Play(snd, PlayOptions{Positional: true, Position: lin.Vec3{X: 3}}))
		b.StartTimer()
		m.mix(out)
	}
}

// BenchmarkReverbTail runs the reverb over a silent send with its tail
// either at normal magnitudes or at subnormal ones. On arm64 the two cost
// the same; on amd64 without flush-to-zero the second is the state a
// decayed tail reached before the reverb flushed its stores.
func BenchmarkReverbTail(b *testing.B) {
	for _, sub := range []bool{false, true} {
		name := "normal"
		v := float32(1e-3)
		if sub {
			name = "subnormal"
			v = math.Float32frombits(0x00001000) // about 5.7e-42
		}
		b.Run(name, func(b *testing.B) {
			r := newReverb(benchRate)
			r.set(ReverbSettings{RoomSize: 0.8, Wet: 0.3})
			fill := func() {
				for i := range r.combL {
					for j := range r.combL[i].buf {
						r.combL[i].buf[j] = v
					}
					for j := range r.combR[i].buf {
						r.combR[i].buf[j] = v
					}
					r.combL[i].filt, r.combR[i].filt = v, v
				}
				for i := range r.apL {
					for j := range r.apL[i].buf {
						r.apL[i].buf[j] = v
					}
					for j := range r.apR[i].buf {
						r.apR[i].buf[j] = v
					}
				}
			}
			fill()
			send := make([]float32, benchBlock*2)
			out := make([]float32, benchBlock*2)
			b.ReportAllocs()
			b.ResetTimer()
			n := 0
			for b.Loop() {
				if n++; n%64 == 0 {
					b.StopTimer()
					fill()
					b.StartTimer()
				}
				r.process(send, out)
			}
		})
	}
}

// TestDenormalsReport plays two seconds of signal into every stateful
// path (occlusion biquad, binaural one-poles, the reverb), then silence,
// and reports how long each path's state holds subnormal values. Every
// multiply by a subnormal is a microcode assist on x86 without FTZ/DAZ.
func TestDenormalsReport(t *testing.T) {
	if os.Getenv("BUNYIP_AUDIT") == "" {
		t.Skip("set BUNYIP_AUDIT=1")
	}
	m := NewMixer(benchRate)
	m.SetSpatial(SpatialSettings{Binaural: true})
	m.SetReverb(ReverbSettings{RoomSize: 0.8, Wet: 0.3})
	snd, _ := m.NewSound(Sine(440, 2, benchRate))
	v := m.Play(snd, PlayOptions{Positional: true, Position: lin.Vec3{X: 3, Y: 1}, Reverb: 0.5, Occlusion: 0.6})
	out := make([]float32, benchBlock*2)
	blocks := 0
	var firstRev, lastRev, maxRev, revBlocks int
	var firstVoice, lastVoice, voiceBlocks int
	firstRev, firstVoice = -1, -1
	for blocks < 60*benchRate/benchBlock {
		m.mix(out)
		blocks++
		r := m.reverb
		n := 0
		for i := range r.combL {
			n += countSub(r.combL[i].buf) + countSub(r.combR[i].buf)
			if subnormal(r.combL[i].filt) {
				n++
			}
		}
		for i := range r.apL {
			n += countSub(r.apL[i].buf) + countSub(r.apR[i].buf)
		}
		if n > 0 {
			revBlocks++
			lastRev = blocks
			if firstRev < 0 {
				firstRev = blocks
			}
			maxRev = max(maxRev, n)
		}
		// The voice's own state: occlusion biquad and binaural filters.
		vn := 0
		m.mu.Lock()
		if v.occ != nil {
			for _, s := range []float32{v.occ.l0, v.occ.l1, v.occ.r0, v.occ.r1} {
				if subnormal(s) {
					vn++
				}
			}
		}
		if v.bin != nil {
			vn += countSub(v.bin.ring)
			for _, s := range []float32{v.bin.lpL, v.bin.lpR, v.bin.low} {
				if subnormal(s) {
					vn++
				}
			}
		}
		m.mu.Unlock()
		if vn > 0 {
			voiceBlocks++
			lastVoice = blocks
			if firstVoice < 0 {
				firstVoice = blocks
			}
		}
	}
	perSec := float64(benchRate) / benchBlock
	t.Logf("sound ends at %.2fs (then 1 ms ramp)", 2.0)
	t.Logf("reverb: subnormal state in %d blocks (%.1fs), from %.1fs to %.1fs, peak %d of %d delay cells",
		revBlocks, float64(revBlocks)/perSec, float64(firstRev)/perSec, float64(lastRev)/perSec, maxRev, reverbCells(m.reverb))
	t.Logf("voice (occlusion+binaural): subnormal state in %d blocks, first %.1fs last %.1fs", voiceBlocks, float64(firstVoice)/perSec, float64(lastVoice)/perSec)
}

// TestDenormalsSilentTail loops a sound that is 0.1 s of tone then 1.9 s
// of digital silence through an occluded binaural voice, the shape of
// many game sounds, and reports how much of the silence the voice's
// filter state spends subnormal.
func TestDenormalsSilentTail(t *testing.T) {
	if os.Getenv("BUNYIP_AUDIT") == "" {
		t.Skip("set BUNYIP_AUDIT=1")
	}
	m := NewMixer(benchRate)
	m.SetSpatial(SpatialSettings{Binaural: true})
	pcm := Sine(440, 2, benchRate)
	for i := benchRate / 10; i < len(pcm.Samples); i++ {
		pcm.Samples[i] = 0
	}
	snd, _ := m.NewSound(pcm)
	v := m.Play(snd, PlayOptions{Loop: true, Positional: true, Position: lin.Vec3{X: 3, Y: -1}, Occlusion: 0.6})
	out := make([]float32, benchBlock*2)
	blocks, sub := 0, 0
	for range 4 * benchRate / benchBlock {
		m.mix(out)
		blocks++
		m.mu.Lock()
		n := 0
		for _, s := range []float32{v.occ.l0, v.occ.l1, v.occ.r0, v.occ.r1, v.bin.lpL, v.bin.lpR, v.bin.low} {
			if subnormal(s) {
				n++
			}
		}
		n += countSub(v.bin.ring)
		m.mu.Unlock()
		if n > 0 {
			sub++
		}
	}
	t.Logf("occluded binaural voice, 5%% tone then silence, looped: subnormal filter state in %d of %d blocks (%.0f%%)",
		sub, blocks, 100*float64(sub)/float64(blocks))
}

func reverbCells(r *reverb) int {
	n := 0
	for i := range r.combL {
		n += len(r.combL[i].buf) + len(r.combR[i].buf)
	}
	for i := range r.apL {
		n += len(r.apL[i].buf) + len(r.apR[i].buf)
	}
	return n
}

// TestDenormalsBiquad reports how long a 400 Hz biquad's output and
// state stay subnormal on silence after a burst of signal.
func TestDenormalsBiquad(t *testing.T) {
	if os.Getenv("BUNYIP_AUDIT") == "" {
		t.Skip("set BUNYIP_AUDIT=1")
	}
	var f lowPass
	c := newBiquad(400, benchRate)
	buf := make([]float32, benchBlock*2)
	for i := range buf {
		buf[i] = float32(math.Sin(float64(i) * 0.05))
	}
	f.process(c, buf)
	first, last := -1, -1
	for blk := range 2000 {
		clear(buf)
		f.process(c, buf)
		n := countSub(buf)
		if subnormal(f.l0) || subnormal(f.l1) {
			n++
		}
		if n > 0 {
			if first < 0 {
				first = blk
			}
			last = blk
		}
	}
	t.Logf("400 Hz biquad on silence: subnormal output from block %d to %d (of 2000); state now l0=%g", first, last, f.l0)
}

// TestWorstCase mixes 10k blocks of the heaviest mode while a goroutine
// calls the setters a game calls each frame, and reports the mean, 99th
// percentile and maximum block time, the longest settings lock wait and
// hold on the audio thread, and the longest a setter batch took.
func TestWorstCase(t *testing.T) {
	if os.Getenv("BUNYIP_AUDIT") == "" {
		t.Skip("set BUNYIP_AUDIT=1")
	}
	// hammer: setters in a tight loop; burst: a game frame updating all
	// 64 voices and the listener every 16 ms; none: no setters, the
	// control for scheduler and machine noise.
	for _, hammer := range []string{"none", "burst", "hammer"} {
		for _, mode := range []perfMode{perfModes[0], perfModes[4]} {
			perfWorst(t, hammer, mode)
		}
	}
}

func perfWorst(t *testing.T, hammer string, mode perfMode) {
	m, voices := perfMixer(t, mode)
	out := make([]float32, benchBlock*2)
	m.mix(out)
	var stop atomic.Bool
	var setterMax atomic.Int64
	done := make(chan struct{})
	update := func(i int) {
		t0 := time.Now()
		v := voices[i%len(voices)]
		v.SetPosition(lin.Vec3{X: float32(i%20) - 10, Z: 5})
		v.SetVolume(0.1 + float32(i%5)/10)
		v.SetOcclusion(float32(i%10) / 10)
		m.SetListener(Listener{Position: lin.Vec3{X: float32(i % 3)}, Forward: lin.Vec3{Z: -1}, Up: lin.Vec3{Y: 1}})
		if d := time.Since(t0).Nanoseconds(); d > setterMax.Load() {
			setterMax.Store(d)
		}
	}
	go func() {
		defer close(done)
		i := 0
		for !stop.Load() {
			switch hammer {
			case "none":
				time.Sleep(time.Millisecond)
			case "burst":
				for range perfVoices {
					i++
					update(i)
				}
				time.Sleep(16 * time.Millisecond)
			default:
				i++
				update(i)
			}
		}
	}()
	var gc0 runtime.MemStats
	runtime.ReadMemStats(&gc0)
	const n = 10000
	times := make([]time.Duration, n)
	var lockMax time.Duration
	for i := range n {
		t0 := time.Now()
		m.mixMu.Lock()
		// mixLocked with the lock-held phases timed.
		clear(out)
		frames := len(out) / 2
		l0 := time.Now()
		send := m.snapshot(out)
		held := time.Since(l0)
		m.placeHeads()
		scratch := m.scratch[:len(out)]
		for j := range m.snap {
			m.snap[j].render(scratch, out, frames)
		}
		if m.blkReverb != nil {
			m.blkReverb.process(send, out)
		}
		for _, bb := range m.revBuses {
			bb.r.process(bb.buf, out)
		}
		for j, s := range out {
			if s != s || s > math.MaxFloat32 || s < -math.MaxFloat32 {
				out[j] = 0
				continue
			}
			out[j] = max(-1, min(1, s))
		}
		l1 := time.Now()
		fns := m.apply()
		held2 := time.Since(l1)
		m.mixMu.Unlock()
		m.run(fns)
		times[i] = time.Since(t0)
		lockMax = max(lockMax, held, held2)
	}
	stop.Store(true)
	<-done
	var gc1 runtime.MemStats
	runtime.ReadMemStats(&gc1)
	slices.Sort(times)
	var sum time.Duration
	for _, d := range times {
		sum += d
	}
	t.Logf("%s/%s: mean %v p99 %v p99.9 %v max %v (budget 10.67ms); longest mu wait+hold on audio thread %v; longest setter batch %v; GCs %d",
		hammer, mode.name, sum/n, times[n*99/100], times[n*999/1000], times[n-1], lockMax, time.Duration(setterMax.Load()), gc1.NumGC-gc0.NumGC)
}

// BenchmarkMusicDecode decodes a whole file through the Music decoder
// and resampler path (as the fill goroutine does, without the ring) and
// reports CPU per second of audio. Set BUNYIP_BENCH_OGG and
// BUNYIP_BENCH_MP3 to file paths.
func BenchmarkMusicDecode(b *testing.B) {
	for _, kind := range []string{"BUNYIP_BENCH_OGG", "BUNYIP_BENCH_MP3"} {
		path := os.Getenv(kind)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(kind, func(b *testing.B) {
			var seconds float64
			b.ReportAllocs()
			for b.Loop() {
				var dec decoder
				var err error
				if kind == "BUNYIP_BENCH_OGG" {
					dec, err = newOggDecoder(bytes.NewReader(data))
				} else {
					dec, err = newMP3Decoder(bytes.NewReader(data))
				}
				if err != nil {
					b.Fatal(err)
				}
				ch := dec.Channels()
				rs := resampler{step: float64(dec.Rate()) / 48000}
				src := make([]float32, 4096*ch)
				var stereo, out []float32
				frames := 0
				for {
					n, err := dec.Read(src)
					if n > 0 {
						frames += n / ch
						stereo = toStereo(src[:n], ch, stereo[:0])
						out = rs.process(stereo, out[:0])
					}
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				seconds = float64(frames) / float64(dec.Rate())
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/seconds/1e3, "µs/audio-second")
		})
	}
}

// subnormal reports whether v is a subnormal float32: exponent field
// zero and mantissa nonzero.
func subnormal(v float32) bool {
	b := math.Float32bits(v)
	return b&0x7f800000 == 0 && b&0x007fffff != 0
}

func countSub(s []float32) int {
	n := 0
	for _, v := range s {
		if subnormal(v) {
			n++
		}
	}
	return n
}
