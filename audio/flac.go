package audio

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
)

// DecodeFLAC decodes a native FLAC stream into interleaved float32 PCM.
// It supports 1..8 channels and 4..24-bit integer samples. Sound and
// Music playback accept mono or stereo. Frame CRCs are checked; the optional
// whole-stream MD5 is not verified. Ogg-encapsulated FLAC is not supported.
func DecodeFLAC(data []byte) (pcm PCM, err error) {
	defer func() {
		if p := recover(); p != nil {
			pcm = PCM{}
			err = fmt.Errorf("audio: corrupt FLAC: %v", p)
		}
	}()
	d, err := newFLACDecoder(bytes.NewReader(data))
	if err != nil {
		return PCM{}, err
	}
	pcm = PCM{Rate: d.Rate(), Channels: d.Channels()}
	buf := make([]float32, 4096*pcm.Channels)
	for {
		n, readErr := d.Read(buf)
		pcm.Samples = append(pcm.Samples, buf[:n]...)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return pcm, nil
			}
			return PCM{}, readErr
		}
	}
}

type flacDecoder struct {
	reader   io.ReadSeeker
	stream   *flac.Stream
	frame    *frame.Frame
	at       int
	position int64
	ended    bool
}

func newFLACDecoder(r io.ReadSeeker) (d *flacDecoder, err error) {
	defer func() {
		if p := recover(); p != nil {
			d = nil
			err = fmt.Errorf("audio: corrupt FLAC metadata: %v", p)
		}
	}()
	s, err := flac.New(r)
	if err != nil {
		return nil, fmt.Errorf("audio: FLAC: %w", err)
	}
	info := s.Info
	if info.SampleRate == 0 || info.NChannels < 1 || info.NChannels > 8 || info.BitsPerSample < 4 || info.BitsPerSample > 24 {
		return nil, errors.New("audio: invalid FLAC stream format")
	}
	return &flacDecoder{reader: r, stream: s}, nil
}

func (d *flacDecoder) Rate() int     { return int(d.stream.Info.SampleRate) }
func (d *flacDecoder) Channels() int { return int(d.stream.Info.NChannels) }
func (d *flacDecoder) Length() int64 { return int64(d.stream.Info.NSamples) }

func (d *flacDecoder) nextFrame() (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("audio: corrupt FLAC frame: %v", p)
		}
	}()
	if d.ended {
		return io.EOF
	}
	f, err := d.stream.Next()
	if err != nil {
		if errors.Is(err, io.EOF) {
			d.ended = true
			if d.Length() > 0 && d.position < d.Length() {
				return io.ErrUnexpectedEOF
			}
		}
		return err
	}
	// FLAC frame headers may inherit rate and sample width from STREAMINFO.
	if f.SampleRate == 0 {
		f.SampleRate = d.stream.Info.SampleRate
	}
	if f.BitsPerSample == 0 {
		f.BitsPerSample = d.stream.Info.BitsPerSample
	}
	if int(f.Channels.Count()) != d.Channels() || f.SampleRate != d.stream.Info.SampleRate || f.BitsPerSample != d.stream.Info.BitsPerSample || f.BlockSize == 0 {
		return errors.New("audio: FLAC frame format changed")
	}
	if err := f.Parse(); err != nil {
		return err
	}
	for _, sub := range f.Subframes {
		if sub == nil || len(sub.Samples) != int(f.BlockSize) {
			return errors.New("audio: invalid FLAC subframe length")
		}
	}
	if d.Length() > 0 && d.position+int64(f.BlockSize) > d.Length() {
		return errors.New("audio: FLAC exceeds declared sample count")
	}
	d.frame, d.at = f, 0
	return nil
}

func (d *flacDecoder) Read(out []float32) (int, error) {
	ch := d.Channels()
	want := len(out) / ch
	scale := float64(uint64(1) << (d.stream.Info.BitsPerSample - 1))
	for i := 0; i < want; i++ {
		if d.frame == nil || d.at == int(d.frame.BlockSize) {
			if err := d.nextFrame(); err != nil {
				return i * ch, err
			}
		}
		for c := range ch {
			out[i*ch+c] = float32(float64(d.frame.Subframes[c].Samples[d.at]) / scale)
		}
		d.at++
		d.position++
	}
	return want * ch, nil
}

// SeekFrame scans forward from the current frame, rewinding when needed.
// It keeps one decoded FLAC frame and never builds a whole-file seek table.
func (d *flacDecoder) SeekFrame(target int64) error {
	target = max(target, 0)
	if d.Length() > 0 && target >= d.Length() {
		d.position = d.Length()
		d.frame = nil
		d.ended = true
		return nil
	}
	if target < d.position || d.ended {
		if _, err := d.reader.Seek(0, io.SeekStart); err != nil {
			return err
		}
		next, err := newFLACDecoder(d.reader)
		if err != nil {
			return err
		}
		*d = *next
	}
	for d.position < target {
		if d.frame == nil || d.at == int(d.frame.BlockSize) {
			if err := d.nextFrame(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		}
		n := min(target-d.position, int64(int(d.frame.BlockSize)-d.at))
		d.at += int(n)
		d.position += n
	}
	return nil
}
