package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/jfreymuth/oggvorbis"
)

// DecodeWAV reads PCM (8/16/24/32-bit integer or 32-bit float) WAV data.
func DecodeWAV(data []byte) (PCM, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return PCM{}, fmt.Errorf("audio: not a WAV file")
	}
	var format uint16
	var channels, rate, bits int
	var samples []byte
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4:]))
		body := data[off+8 : min(off+8+size, len(data))]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return PCM{}, fmt.Errorf("audio: short fmt chunk")
			}
			format = binary.LittleEndian.Uint16(body)
			channels = int(binary.LittleEndian.Uint16(body[2:]))
			rate = int(binary.LittleEndian.Uint32(body[4:]))
			bits = int(binary.LittleEndian.Uint16(body[14:]))
			if format == 0xFFFE && len(body) >= 26 { // WAVE_FORMAT_EXTENSIBLE: real format in the GUID
				format = binary.LittleEndian.Uint16(body[24:])
			}
		case "data":
			samples = body
		}
		off += 8 + size + size%2
	}
	if samples == nil || channels == 0 {
		return PCM{}, fmt.Errorf("audio: WAV missing fmt or data chunk")
	}
	bytesPer := bits / 8
	if bytesPer == 0 || (format != 1 && format != 3) {
		return PCM{}, fmt.Errorf("audio: unsupported WAV format %d with %d bits", format, bits)
	}
	n := len(samples) / bytesPer
	out := make([]float32, n)
	for i := range n {
		b := samples[i*bytesPer:]
		switch {
		case format == 3 && bits == 32:
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b))
		case bits == 8:
			out[i] = (float32(b[0]) - 128) / 128
		case bits == 16:
			out[i] = float32(int16(binary.LittleEndian.Uint16(b))) / 32768
		case bits == 24:
			v := int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24) >> 8
			out[i] = float32(v) / 8388608
		case bits == 32:
			out[i] = float32(int32(binary.LittleEndian.Uint32(b))) / 2147483648
		default:
			return PCM{}, fmt.Errorf("audio: unsupported WAV bit depth %d", bits)
		}
	}
	return PCM{Samples: out, Channels: channels, Rate: rate}, nil
}

// DecodeOGG decodes a whole Ogg Vorbis file into memory.
func DecodeOGG(data []byte) (pcm PCM, err error) {
	defer func() { // a corrupt stream is an error, not a crash
		if r := recover(); r != nil {
			pcm, err = PCM{}, fmt.Errorf("audio: ogg: corrupt stream: %v", r)
		}
	}()
	samples, format, err := oggvorbis.ReadAll(bytes.NewReader(data))
	if err != nil {
		return PCM{}, fmt.Errorf("audio: ogg: %w", err)
	}
	return PCM{Samples: samples, Channels: format.Channels, Rate: format.SampleRate}, nil
}

// Decode picks a sampled-audio decoder (WAV, Ogg Vorbis, MP3, FLAC) from the
// data's magic bytes. Tracker modules are music, not clips; see PlayModule.
func Decode(data []byte) (PCM, error) {
	switch {
	case bytes.HasPrefix(data, []byte("RIFF")):
		return DecodeWAV(data)
	case bytes.HasPrefix(data, []byte("OggS")):
		return DecodeOGG(data)
	case bytes.HasPrefix(data, []byte("fLaC")):
		return DecodeFLAC(data)
	case isMP3(data):
		return DecodeMP3(data)
	}
	return PCM{}, fmt.Errorf("audio: unrecognised format")
}
