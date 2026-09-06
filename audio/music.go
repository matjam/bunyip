package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
)

// Music plays WAV, Ogg Vorbis, MP3 or FLAC through a two-second PCM buffer
// filled on a decoder goroutine. Ogg, MP3 and FLAC decode incrementally; WAV
// is decoded completely at open and retained in memory.
// Open one with Mixer.OpenMusicFile or Mixer.OpenMusic, start it with
// Mixer.PlayStream, and Close it when the game is done with it.
type Music struct {
	dec    decoder
	loop   bool
	rs     resampler
	rate   int   // the mixer's
	length int64 // source frames, 0 when unknown

	mu                 sync.Mutex
	cond               *sync.Cond
	ring               []float32 // stereo samples at the mixer rate
	rd                 int
	count              int
	ended              bool
	close              bool
	err                error
	seek               float64 // pending Seek target in seconds; negative when none
	gen                int     // bumped by Seek so fill drops what it decoded before
	worker             sync.WaitGroup
	owned              io.Closer
	closeOwned         sync.Once
	loopStart, loopEnd int64   // source frames; zero end means the whole track
	playFrame          float64 // source position of the next buffered frame handed out
	decodeFrame        int64   // decoder goroutine only
	voiceBuffered      float64 // mixer-rate frames prefetched but not yet played
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
// the end. It sniffs the format from the start of r and waits for the
// first decoded chunk or an error. The reader must support seeking and
// remain available until Music.Close returns. Music does not close r.
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
	case bytes.Equal(head[:], []byte("fLaC")):
		dec, err = newFLACDecoder(r)
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
	mu.worker.Go(mu.fill)
	// Return with the first chunk in hand so playback starts immediately
	// rather than with a moment of silence.
	mu.wait(func() bool { return mu.count > 0 })
	if err := mu.Err(); err != nil {
		mu.Close()
		return nil, err
	}
	return mu, nil
}

// OpenMusicFile opens a WAV, Ogg Vorbis, MP3 or FLAC file for streaming.
// Music owns the file and closes it after the decoder stops. Failure
// closes the file before returning. Loop restarts playback at the end.
func (m *Mixer) OpenMusicFile(path string, loop bool) (*Music, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("audio: music: %w", err)
	}
	return m.openOwnedMusic(f, loop)
}

func (m *Mixer) openOwnedMusic(r io.ReadSeekCloser, loop bool) (*Music, error) {
	music, err := m.OpenMusic(r, loop)
	if err != nil {
		r.Close()
		return nil, err
	}
	music.owned = r
	return music, nil
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
// FLAC uses STREAMINFO's sample count, which may also be unknown (0).
func (mu *Music) Duration() float64 {
	return float64(mu.length) / float64(mu.dec.Rate())
}

// Seek moves playback to seconds from the start. It returns at once and
// the decoder catches up on its goroutine, so a few milliseconds of
// silence can precede the new position; anything buffered from the old
// position is dropped. Seeking past the end ends the music, or starts it
// over when it loops. A decoder that fails to seek ends the music and
// reports through Err. WAV and Ogg Vorbis seek exactly; MP3 decodes from
// the previous frame boundary. FLAC seeks exactly by scanning frames,
// rewinding when necessary, with memory bounded to one decoded frame.
// Prefer Voice.Seek on a playing voice so
// its Position follows. Music whose voice has already ended can be sought
// and played again with PlayStream.
func (mu *Music) Seek(seconds float64) error {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds >= float64(math.MaxInt64)/float64(mu.dec.Rate()) {
		return errors.New("audio: music: seek time must be finite")
	}
	mu.mu.Lock()
	defer mu.mu.Unlock()
	if mu.close {
		return errors.New("audio: music: closed")
	}
	if mu.err != nil {
		return mu.err
	}
	mu.setSeekLocked(max(seconds, 0))
	return nil
}

func (mu *Music) setSeekLocked(seconds float64) {
	mu.setSeekFrameLocked(math.Floor(seconds * float64(mu.dec.Rate())))
}

func (mu *Music) setSeekFrameLocked(frame float64) {
	if mu.length > 0 {
		frame = min(frame, float64(mu.length))
	}
	if mu.loop && mu.loopEnd > 0 && (frame < float64(mu.loopStart) || frame >= float64(mu.loopEnd)) {
		frame = float64(mu.loopStart)
	}
	mu.playFrame = math.Floor(frame)
	mu.seek = mu.playFrame / float64(mu.dec.Rate())
	mu.voiceBuffered = 0
	mu.gen++
	mu.rd, mu.count = 0, 0
	mu.ended = false
	mu.cond.Broadcast()
}

// Looping reports whether Music repeats. This is independent of whether
// any Voice is currently playing the music.
func (mu *Music) Looping() bool { mu.mu.Lock(); defer mu.mu.Unlock(); return mu.loop }

// SetLooping changes repetition and flushes prefetched samples at the
// next playback position, accounting for voice lookahead. Enabling it
// after all samples reach EOF restarts at the loop
// start; a Voice that already stopped must be played again. Closed or
// failed music is unchanged. The next read may briefly underrun.
func (mu *Music) SetLooping(loop bool) {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	if mu.close || mu.err != nil || mu.loop == loop {
		return
	}
	frame := mu.playFrame - mu.voiceBuffered*float64(mu.dec.Rate())/float64(mu.rate)
	end := mu.loopEnd
	if end == 0 {
		end = mu.length
	}
	if mu.loop && end > mu.loopStart {
		width := float64(end - mu.loopStart)
		frame = float64(mu.loopStart) + math.Mod(math.Mod(frame-float64(mu.loopStart), width)+width, width)
	}
	frame = max(frame, 0)
	mu.loop = loop
	if loop && mu.ended && mu.count == 0 && mu.voiceBuffered == 0 {
		frame = float64(mu.loopStart)
	}
	mu.setSeekFrameLocked(frame)
}

// SetLoopRange selects [start,end) and restarts decoding at start,
// flushing prefetched audio. (0,0) restores the whole track. Nonzero
// ranges must contain at least one source frame and fit a known duration;
// unknown-length music accepts a finite range and also wraps at early EOF.
// Boundaries round down to source frames. The range repeats only while
// Looping; without looping playback continues from start to the file's end.
func (mu *Music) SetLoopRange(start, end time.Duration) error {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	if mu.close {
		return errors.New("audio: music: closed")
	}
	if mu.err != nil {
		return mu.err
	}
	if start < 0 || end < 0 || ((start != 0 || end != 0) && end <= start) {
		return errors.New("audio: music: invalid loop range")
	}
	rate := float64(mu.dec.Rate())
	if end.Seconds() >= float64(math.MaxInt64)/rate {
		return errors.New("audio: music: loop range exceeds source frame limit")
	}
	a, b := int64(start.Seconds()*rate), int64(end.Seconds()*rate)
	if (start != 0 || end != 0) && (b <= a || a < 0 || b < 0 || (mu.length > 0 && b > mu.length)) {
		return errors.New("audio: music: loop range outside source frames")
	}
	mu.loopStart, mu.loopEnd = a, b
	mu.setSeekFrameLocked(float64(a))
	return nil
}

// LoopRange returns configured, source-frame-aligned boundaries; (0,0)
// means the whole track. Values round up to a nanosecond so passing the
// result back to SetLoopRange preserves source-frame boundaries.
func (mu *Music) LoopRange() (start, end time.Duration) {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	rate := float64(mu.dec.Rate())
	return time.Duration(math.Ceil(float64(mu.loopStart) / rate * float64(time.Second))), time.Duration(math.Ceil(float64(mu.loopEnd) / rate * float64(time.Second)))
}

func (mu *Music) streamRevision() uint64 { mu.mu.Lock(); defer mu.mu.Unlock(); return uint64(mu.gen) }

func (mu *Music) streamLookahead(revision uint64, frames float64) {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	if uint64(mu.gen) == revision {
		mu.voiceBuffered = frames
	}
}

// wait blocks until cond holds, the music ends, or it is closed.
func (mu *Music) wait(cond func() bool) {
	mu.mu.Lock()
	for !cond() && !mu.ended && !mu.close {
		mu.cond.Wait()
	}
	mu.mu.Unlock()
}

// Close stops and joins the decoder, then closes an owned file.
// Subsequent Read calls return zero; a voice ends when it next reads.
// Repeated and concurrent calls are safe. Readers supplied to OpenMusic
// remain borrowed and may be closed after this returns. Close waits for
// an in-flight Read or Seek, which a custom reader must unblock itself.
// Do not call Close from that reader or decoder, including its callbacks.
func (mu *Music) Close() {
	mu.mu.Lock()
	if !mu.close {
		mu.gen++
	}
	mu.close = true
	mu.cond.Broadcast()
	mu.mu.Unlock()
	mu.worker.Wait()
	mu.closeOwned.Do(func() {
		if mu.owned != nil {
			mu.owned.Close()
		}
	})
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
	n, _ := mu.readSourceLocked(out)
	return n
}

func (mu *Music) readSource(out []float32, revision uint64) (int, int) {
	mu.mu.Lock()
	defer mu.mu.Unlock()
	if uint64(mu.gen) != revision {
		// A control changed during the current mixer block. Do not consume
		// the new generation until the voice discards its old lookahead.
		clear(out)
		return len(out) / 2, 0
	}
	return mu.readSourceLocked(out)
}

func (mu *Music) readSourceLocked(out []float32) (int, int) {
	if mu.close {
		return 0, 0
	}
	want := len(out) &^ 1
	n := min(mu.count, want)
	first := min(n, len(mu.ring)-mu.rd)
	copy(out, mu.ring[mu.rd:mu.rd+first])
	copy(out[first:n], mu.ring[:n-first])
	mu.rd = (mu.rd + n) % len(mu.ring)
	mu.count -= n
	mu.playFrame += float64(n/2) * float64(mu.dec.Rate()) / float64(mu.rate)
	end := mu.loopEnd
	if end == 0 {
		end = mu.length
	}
	if mu.loop && end > mu.loopStart && mu.playFrame >= float64(end) {
		mu.playFrame = float64(mu.loopStart) + math.Mod(mu.playFrame-float64(mu.loopStart), float64(end-mu.loopStart))
	}
	mu.cond.Signal()
	if n < want {
		if mu.ended {
			return n / 2, n / 2
		}
		clear(out[n:want]) // underrun: the decoder is behind, keep going
	}
	return want / 2, n / 2
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
		mu.mu.Lock()
		loop, loopStart, loopEnd := mu.loop, mu.loopStart, mu.loopEnd
		mu.mu.Unlock()
		limit := len(src) / ch
		if loop && loopEnd > 0 {
			limit = min(limit, int(max(0, loopEnd-mu.decodeFrame)))
		}
		n, err := 0, error(io.EOF)
		if limit > 0 {
			n, err = mu.dec.Read(src[:limit*ch])
		}
		if n < 0 || n > limit*ch || n%ch != 0 {
			n, err = 0, io.ErrUnexpectedEOF
		}
		if n > 0 {
			mu.decodeFrame += int64(n / ch)
			rewound = false
			emptyReads = 0
			stereo = toStereo(src[:n], ch, stereo[:0])
			out = mu.rs.process(stereo, out[:0])
			if !mu.push(out, gen) {
				return
			}
		}
		if err == nil && loop && loopEnd > 0 && mu.decodeFrame >= loopEnd {
			err = io.EOF
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
		if errors.Is(err, io.EOF) && loop && !rewound {
			if rerr := mu.dec.SeekFrame(loopStart); rerr == nil {
				mu.decodeFrame = loopStart
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
	frame := int64(math.Round(target * float64(mu.dec.Rate())))
	if err := mu.dec.SeekFrame(frame); err != nil {
		mu.mu.Lock()
		mu.err = fmt.Errorf("audio: music: seek: %w", err)
		mu.ended = true
		mu.cond.Broadcast()
		mu.mu.Unlock()
		return 0, false
	}
	mu.rs.reset()
	mu.decodeFrame = frame
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
