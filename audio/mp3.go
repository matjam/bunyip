package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/hajimehoshi/go-mp3"
)

// DecodeMP3 decodes a whole MP3 file into memory as stereo PCM.
//
// Encode game audio at 44.1 or 48 kHz: the decoder reproduces MPEG-2
// low-sample-rate files (22.05 kHz and below) with an uneven level.
func DecodeMP3(data []byte) (pcm PCM, err error) {
	// The MP3 library indexes past its tables on some corrupt streams;
	// a broken file is an error, not a crash.
	defer func() {
		if r := recover(); r != nil {
			pcm, err = PCM{}, fmt.Errorf("audio: mp3: corrupt stream: %v", r)
		}
	}()
	dec, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return PCM{}, fmt.Errorf("audio: mp3: %w", err)
	}
	raw, err := io.ReadAll(dec) // 16-bit little-endian stereo
	if err != nil {
		return PCM{}, fmt.Errorf("audio: mp3: %w", err)
	}
	n := len(raw) / 2
	samples := make([]float32, n)
	for i := range n {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768
	}
	return PCM{Samples: samples, Channels: 2, Rate: dec.SampleRate()}, nil
}

func isMP3(data []byte) bool {
	if bytes.HasPrefix(data, []byte("ID3")) {
		return true
	}
	return len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0
}
