package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
)

// Music is a WAV, Ogg Vorbis or MP3 file decoded while it plays, a couple
// of seconds ahead of the output on its own goroutine, so a long track
// never sits in memory. Open one with Mixer.OpenMusic, start it with
// Mixer.PlayStream, and Close it when the game is done with it.
type Music struct {
	dec    decoder
	loop   bool
	rs     resampler
	rate   int   // the mixer's
	length int64 // source frames, 0 when unknown

	mu    sync.Mutex
	cond  *sync.Cond
	ring  []float32 // stereo samples at the mixer rate
	rd    int
	count int
	ended bool
	close bool
	err   error
	seek  float64 // pending Seek target in seconds; negative when none
	gen   int     // bumped by Seek so fill drops what it decoded before
}

// decoder yields interleaved samples at its own rate and channel count.
type decoder interface {
	Read(buf []float32) (int, error)
	// SeekFrame moves to the given frame; 0 rewinds.
	SeekFrame(frame int64) error
	// Length is the total frames, or 0 when the container does not say.
	Length() int64
	Channels() int
	Rate() int
}

// OpenMusic prepares a file for streaming; loop makes it start over at
// the end. The reader must stay open until Close.
func (m *Mixer) OpenMusic(r io.ReadSeeker, loop bool) (*Music, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, fmt.Errorf("audio: music: %w", err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("audio: music: %w", err)
	}
	var dec decoder
	var err error
	switch {
	case bytes.Equal(head[:], []byte("RIFF")):
		dec, err = newWAVDecoder(r)
	case bytes.Equal(head[:], []byte("OggS")):
		dec, err = newOggDecoder(r)
	case isMP3(head[:]):
		dec, err = newMP3Decoder(r)
	default:
		err = errors.New("audio: music: unrecognised format")
	}
	if err != nil {
		return nil, err
	}
	if dec.Channels() < 1 || dec.Channels() > 2 || dec.Rate() <= 0 {
		return nil, fmt.Errorf("audio: music: unsupported %d channels at %d Hz", dec.Channels(), dec.Rate())
	}
	mu := &Music{dec: dec, loop: loop, rate: m.rate, length: dec.Length(), seek: -1,
		ring: make([]float32, m.rate*2*2)} // two seconds
	mu.cond = sync.NewCond(&mu.mu)
	mu.rs = resampler{step: float64(dec.Rate()) / float64(m.rate)}
	go mu.fill()
	// Return with the first chunk in hand so playback starts immediately
	// rather than with a moment of silence.
	mu.wait(func() bool { return mu.count > 0 })
	return mu, mu.Err()
}

// Buffered reports how many seconds are decoded and waiting to play.
func (mu *Music) Buffered() float64 {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	return float64(mu.count/2) / float64(len(mu.ring)/4)
}

// Duration is the track's length in seconds, or 0 when unknown. WAV
// always knows it. Ogg Vorbis reads it from the last page at open; MP3
// scans the frame headers at open. Either reports 0 when its reader could
// not seek to find out.
func (mu *Music) Duration() float64 {
	return float64(mu.length) / float64(mu.dec.Rate())
}

// Seek moves playback to seconds from the start. It returns at once and
// the decoder catches up on its goroutine, so a few milliseconds of
// silence can precede the new position; anything buffered from the old
// position is dropped. Seeking past the end ends the music, or starts it
// over when it loops. A decoder that fails to seek ends the music and
// reports through Err. WAV and Ogg Vorbis seek exactly; MP3 decodes from
// the previous frame boundary. Prefer Voice.Seek on a playing voice so
// its Position follows. Music whose voice has already ended can be sought
// and played again with PlayStream.
func (mu *Music) Seek(seconds float64) error {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	if mu.close {
		return errors.New("audio: music: closed")
	}
	if mu.err != nil {
		return mu.err
	}
	mu.seek = max(seconds, 0)
	mu.gen++
	mu.rd, mu.count = 0, 0
	mu.ended = false
	mu.cond.Broadcast()
	return nil
}

// wait blocks until cond holds, the music ends, or it is closed.
func (mu *Music) wait(cond func() bool) {
	mu.mu.Lock()
	for !cond() && !mu.ended && !mu.close {
		mu.cond.Wait()
	}
	mu.mu.Unlock()
}

// Close stops decoding; a voice playing the music ends.
func (mu *Music) Close() {
	mu.mu.Lock()
	mu.close = true
	mu.cond.Broadcast()
	mu.mu.Unlock()
}

// Err reports a decoding error, if one ended the music early.
func (mu *Music) Err() error {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	return mu.err
}

// Read implements Stream for the mixer: it hands over buffered frames,
// pads a momentary shortfall with silence, and returns short only once
// the music has ended.
func (mu *Music) Read(out []float32) int {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	if mu.close {
		return 0
	}
	want := len(out) &^ 1
	n := min(mu.count, want)
	first := min(n, len(mu.ring)-mu.rd)
	copy(out, mu.ring[mu.rd:mu.rd+first])
	copy(out[first:n], mu.ring[:n-first])
	mu.rd = (mu.rd + n) % len(mu.ring)
	mu.count -= n
	mu.cond.Signal()
	if n < want {
		if mu.ended {
			return n / 2
		}
		clear(out[n:want]) // underrun: the decoder is behind, keep going
	}
	return want / 2
}

// fill decodes on its own goroutine, waiting whenever the ring is full.
// At the end of a track it parks until a Seek restarts it or Close ends
// it; a decoding error ends it for good.
func (mu *Music) fill() {
	ch := mu.dec.Channels()
	src := make([]float32, 4096*ch)
	stereo := make([]float32, 4096*2)
	var out []float32
	rewound := false // the end was reached and nothing has decoded since
	lastGen := -1
	emptyReads := 0
	for {
		gen, ok := mu.applySeek()
		if !ok {
			return
		}
		if gen != lastGen {
			rewound, emptyReads = false, 0
			lastGen = gen
		}
		n, err := mu.dec.Read(src)
		if n < 0 || n > len(src) || n%ch != 0 {
			n, err = 0, io.ErrUnexpectedEOF
		}
		if n > 0 {
			rewound = false
			emptyReads = 0
			stereo = toStereo(src[:n], ch, stereo[:0])
			out = mu.rs.process(stereo, out[:0])
			if !mu.push(out, gen) {
				return
			}
		}
		if err == nil {
			if n > 0 {
				continue
			}
			emptyReads++
			if emptyReads < 100 {
				continue
			}
			err = io.ErrNoProgress
		}
		// A looping track rewinds at its end, unless the rewind produced
		// nothing: a track with no frames would otherwise spin forever.
		if errors.Is(err, io.EOF) && mu.loop && !rewound {
			if rerr := mu.dec.SeekFrame(0); rerr == nil {
				// The next pass supplies the next interpolation frame. Keep
				// the phase and last frame, even for a one-frame track.
				rewound = true
				continue
			} else {
				err = fmt.Errorf("audio: music: rewind: %w", rerr)
			}
		}
		if errors.Is(err, io.EOF) {
			out = mu.rs.finish(out[:0])
			if !mu.push(out, gen) {
				return
			}
		}
		mu.mu.Lock()
		if !errors.Is(err, io.EOF) {
			mu.err = err
			mu.ended = true
			mu.cond.Broadcast()
			mu.mu.Unlock()
			return
		}
		if mu.gen == gen { // no Seek arrived while the tail was decoding
			mu.ended = true
			mu.cond.Broadcast()
		}
		for mu.seek < 0 && !mu.close {
			mu.cond.Wait()
		}
		mu.mu.Unlock()
	}
}

// applySeek performs a pending Seek, if any, and returns the generation
// the next decoded samples belong to; false means the music is closed or
// the decoder failed to seek.
func (mu *Music) applySeek() (int, bool) {
	mu.mu.Lock()
	target, gen := mu.seek, mu.gen
	mu.seek = -1
	closed := mu.close
	mu.mu.Unlock()
	if closed {
		return 0, false
	}
	if target < 0 {
		return gen, true
	}
	if err := mu.dec.SeekFrame(int64(target * float64(mu.dec.Rate()))); err != nil {
		mu.mu.Lock()
		mu.err = fmt.Errorf("audio: music: seek: %w", err)
		mu.ended = true
		mu.cond.Broadcast()
		mu.mu.Unlock()
		return 0, false
	}
	mu.rs.reset()
	return gen, true
}

// push appends stereo samples to the ring, blocking while it is full, and
// reports false once the music is closed. Samples decoded before a Seek
// (an older gen) are dropped instead.
func (mu *Music) push(samples []float32, gen int) bool {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	for len(samples) > 0 {
		for mu.count == len(mu.ring) && !mu.close && mu.gen == gen {
			mu.cond.Wait()
		}
		if mu.close {
			return false
		}
		if mu.gen != gen {
			return true
		}
		wr := (mu.rd + mu.count) % len(mu.ring)
		n := min(len(samples), len(mu.ring)-mu.count, len(mu.ring)-wr)
		copy(mu.ring[wr:wr+n], samples[:n])
		mu.count += n
		samples = samples[n:]
		mu.cond.Broadcast()
	}
	return true
}

// toStereo appends in as stereo frames to dst.
func toStereo(in []float32, channels int, dst []float32) []float32 {
	if channels == 2 {
		return append(dst, in...)
	}
	for _, s := range in {
		dst = append(dst, s, s)
	}
	return dst
}

// resampler converts stereo frames between rates with linear
// interpolation, carrying its position across calls so chunk boundaries
// are seamless.
type resampler struct {
	step   float64 // source frames per output frame
	pos    float64 // position within the current chunk, counting last as -1
	last   [2]float32
	primed bool
}

func (r *resampler) reset() { r.primed = false; r.pos = 0 }

// finish holds the last sample through its final source-frame interval.
// A loop supplies its first frame instead; only terminal EOF drains here.
func (r *resampler) finish(dst []float32) []float32 {
	if r.primed {
		dst = r.process(r.last[:], dst)
	}
	r.reset()
	return dst
}

// process appends the resampled chunk to dst.
func (r *resampler) process(in []float32, dst []float32) []float32 {
	frames := len(in) / 2
	if frames == 0 {
		return dst
	}
	if !r.primed {
		r.last = [2]float32{in[0], in[1]}
		r.pos = 0
		r.primed = true
	}
	// Frame -1 is last, frames 0..frames-1 are in; interpolate between
	// frame i and i+1 while i+1 exists.
	for ; r.pos < float64(frames-1); r.pos += r.step {
		// Floor, not truncation: a position carried over from the last
		// chunk is in (-1, 0) and must read frame -1, which is last.
		i := int(math.Floor(r.pos))
		t := float32(r.pos - float64(i))
		var a0, a1 float32
		if i < 0 {
			a0, a1 = r.last[0], r.last[1]
		} else {
			a0, a1 = in[i*2], in[i*2+1]
		}
		b0, b1 := in[(i+1)*2], in[(i+1)*2+1]
		dst = append(dst, a0+(b0-a0)*t, a1+(b1-a1)*t)
	}
	r.pos -= float64(frames)
	r.last = [2]float32{in[(frames-1)*2], in[(frames-1)*2+1]}
	return dst
}

// memoryDecoder serves already-decoded PCM, used for WAV whose files are
// small enough to hold.
type memoryDecoder struct {
	pcm PCM
	pos int
}

func newWAVDecoder(r io.Reader) (decoder, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("audio: music: %w", err)
	}
	pcm, err := DecodeWAV(data)
	if err != nil {
		return nil, err
	}
	return &memoryDecoder{pcm: pcm}, nil
}

func (d *memoryDecoder) Read(buf []float32) (int, error) {
	if d.pos >= len(d.pcm.Samples) {
		return 0, io.EOF
	}
	n := copy(buf, d.pcm.Samples[d.pos:])
	d.pos += n
	return n, nil
}

func (d *memoryDecoder) SeekFrame(frame int64) error {
	d.pos = int(min(frame, d.Length())) * d.pcm.Channels
	return nil
}
func (d *memoryDecoder) Length() int64 { return int64(len(d.pcm.Samples) / d.pcm.Channels) }
func (d *memoryDecoder) Channels() int { return d.pcm.Channels }
func (d *memoryDecoder) Rate() int     { return d.pcm.Rate }

type oggDecoder struct{ r *oggvorbis.Reader }

func newOggDecoder(r io.ReadSeeker) (decoder, error) {
	or, err := oggvorbis.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("audio: ogg: %w", err)
	}
	return &oggDecoder{r: or}, nil
}

func (d *oggDecoder) Read(buf []float32) (int, error) { return d.r.Read(buf) }
func (d *oggDecoder) SeekFrame(frame int64) error     { return d.r.SetPosition(frame) }
func (d *oggDecoder) Length() int64                   { return d.r.Length() }
func (d *oggDecoder) Channels() int                   { return d.r.Channels() }
func (d *oggDecoder) Rate() int                       { return d.r.SampleRate() }

type mp3Decoder struct {
	d   *mp3.Decoder
	raw []byte
}

func newMP3Decoder(r io.ReadSeeker) (decoder, error) {
	d, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("audio: mp3: %w", err)
	}
	return &mp3Decoder{d: d}, nil
}

// Read converts the decoder's 16-bit stereo output to float samples.
func (d *mp3Decoder) Read(buf []float32) (int, error) {
	want := (len(buf) &^ 1) * 2
	if len(d.raw) < want {
		d.raw = make([]byte, want)
	}
	n, err := io.ReadFull(d.d, d.raw[:want])
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	samples := (n / 2) &^ 1
	for i := range samples {
		buf[i] = float32(int16(binary.LittleEndian.Uint16(d.raw[i*2:]))) / 32768
	}
	if samples > 0 && errors.Is(err, io.EOF) {
		err = nil // deliver the tail first; the next call reports EOF
	}
	return samples, err
}

// SeekFrame positions the decoder by output bytes: four per stereo frame.
func (d *mp3Decoder) SeekFrame(frame int64) error {
	_, err := d.d.Seek(frame*4, io.SeekStart)
	return err
}

// Length is 0 when go-mp3 could not scan the file (it reports -1).
func (d *mp3Decoder) Length() int64 { return max(d.d.Length(), 0) / 4 }
func (d *mp3Decoder) Channels() int { return 2 }
func (d *mp3Decoder) Rate() int     { return d.d.SampleRate() }
