package audio

import (
	"encoding/binary"
	"testing"
)

// tinyWAV is a valid one-channel 16-bit file with a few samples.
func tinyWAV() []byte {
	samples := []int16{0, 1000, -1000, 32767, -32768, 0}
	dataLen := uint32(len(samples) * 2)
	b := make([]byte, 0, 44+dataLen)
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, 36+dataLen)
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1) // PCM
	b = binary.LittleEndian.AppendUint16(b, 1) // mono
	b = binary.LittleEndian.AppendUint32(b, 8000)
	b = binary.LittleEndian.AppendUint32(b, 16000)
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, dataLen)
	for _, s := range samples {
		b = binary.LittleEndian.AppendUint16(b, uint16(s))
	}
	return b
}

// FuzzDecode feeds the sound decoders corrupt files: they must return an
// error rather than panic.
func FuzzDecode(f *testing.F) {
	f.Add(tinyWAV())
	f.Add([]byte("RIFF"))
	f.Add([]byte("OggS"))
	f.Add([]byte{0xFF, 0xFB, 0x90, 0x00})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(data)
	})
}
