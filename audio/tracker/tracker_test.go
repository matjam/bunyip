package tracker

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// squareWave is a 32-sample square, one full cycle, looping.
func squareWave() []byte {
	b := make([]byte, 32)
	for i := range b {
		if i < 16 {
			b[i] = 100
		} else {
			b[i] = byte(256 - 100)
		}
	}
	return b
}

// buildMOD makes a 4-channel MOD with one looping square sample and one
// pattern: C-2 on channel 0 at row 0, speed 6, tempo 125, then a break.
func buildMOD() []byte {
	var b []byte
	b = append(b, make([]byte, 20)...) // title
	for i := range 31 {
		h := make([]byte, 30)
		if i == 0 {
			copy(h, "square")
			binary.BigEndian.PutUint16(h[22:], 16) // length in words
			h[25] = 64                             // volume
			binary.BigEndian.PutUint16(h[26:], 0)  // loop start
			binary.BigEndian.PutUint16(h[28:], 16) // loop length
		}
		b = append(b, h...)
	}
	b = append(b, 1, 0) // song length, restart
	b = append(b, make([]byte, 128)...)
	b = append(b, "M.K."...)
	pattern := make([]byte, 64*4*4)
	// Row 0, channel 0: period 428 (C-2), instrument 1 (low nibble of
	// the instrument lives in the top of byte 2), no effect.
	pattern[0] = byte(428 >> 8)
	pattern[1] = byte(428 & 0xFF)
	pattern[2] = 0x10
	b = append(b, pattern...)
	b = append(b, squareWave()...)
	return b
}

// buildS3M makes a one-channel S3M with the same square sample at C4 speed
// 8363 and a C-4 note on row 0.
func buildS3M() []byte {
	hdr := make([]byte, 96)
	copy(hdr, "square")
	hdr[28] = 0x1A
	hdr[29] = 16
	le := binary.LittleEndian
	le.PutUint16(hdr[32:], 2) // orders: pattern 0 then the 255 end marker
	le.PutUint16(hdr[34:], 1) // instruments
	le.PutUint16(hdr[36:], 1) // patterns
	le.PutUint16(hdr[40:], 0x1320)
	le.PutUint16(hdr[42:], 2) // unsigned samples
	copy(hdr[44:], "SCRM")
	hdr[48] = 64  // global volume
	hdr[49] = 6   // speed
	hdr[50] = 125 // tempo
	hdr[51] = 0x80 | 48
	for i := range 32 {
		hdr[64+i] = 255
	}
	hdr[64] = 0 // channel 0 enabled, left
	// Layout after the header: order(1) + pad, ins ptr(2), pat ptr(2), then
	// instrument at 0x70 (paragraph 7), pattern at 0xC0 (paragraph 12),
	// sample data at 0x100 (paragraph 16).
	b := append([]byte{}, hdr...)
	b = append(b, 0, 0)        // order 0, pad to keep pointers aligned
	b = append(b, 7, 0, 12, 0) // instrument and pattern parapointers
	for len(b) < 0x70 {
		b = append(b, 0)
	}
	ins := make([]byte, 80)
	ins[0] = 1
	ins[14] = 16 // memseg low word: paragraph 16
	le.PutUint32(ins[16:], 32)
	le.PutUint32(ins[20:], 0)
	le.PutUint32(ins[24:], 32)
	ins[28] = 64
	ins[31] = 1 // loop
	le.PutUint32(ins[32:], 8363)
	copy(ins[76:], "SCRS")
	b = append(b, ins...)
	for len(b) < 0xC0 {
		b = append(b, 0)
	}
	// Pattern: length, then row 0: channel 0 with note+instrument, then 63 empty rows.
	pat := []byte{0, 0, 0x20, 0x40, 1, 0}
	for range 64 {
		pat = append(pat, 0)
	}
	le.PutUint16(pat, uint16(len(pat)-2))
	b = append(b, pat...)
	for len(b) < 0x100 {
		b = append(b, 0)
	}
	for _, s := range squareWave() {
		b = append(b, s+128) // unsigned
	}
	return b
}

func zeroCrossings(out []float32) int {
	n := 0
	for i := 2; i < len(out); i += 2 {
		if (out[i] >= 0) != (out[i-2] >= 0) {
			n++
		}
	}
	return n
}

func TestMODPitch(t *testing.T) {
	m, err := Load(buildMOD())
	if err != nil {
		t.Fatal(err)
	}
	if m.Channels != 4 || len(m.Patterns) != 1 || !m.Samples[0].loops() {
		t.Fatalf("module %+v", m)
	}
	p := NewPlayer(m, 48000)
	p.Loop = true
	out := make([]float32, 48000*2)
	if n := p.Read(out); n != 48000 {
		t.Fatalf("read %d frames", n)
	}
	// C-2 at period 428: 8287 Hz through a 32-sample cycle is 259 Hz, so
	// about 518 zero crossings per second.
	if zc := zeroCrossings(out); zc < 470 || zc > 570 {
		t.Errorf("zero crossings %d, want about 518", zc)
	}
}

func TestS3MPitch(t *testing.T) {
	m, err := Load(buildS3M())
	if err != nil {
		t.Fatal(err)
	}
	if m.Channels != 1 || len(m.Samples) != 1 || len(m.Samples[0].Data) != 32 {
		t.Fatalf("module channels %d samples %d data %d", m.Channels, len(m.Samples), len(m.Samples[0].Data))
	}
	cell := m.Patterns[0].Rows[0][0]
	if cell.Note != 4*12 || cell.Instrument != 1 {
		t.Fatalf("cell %+v", cell)
	}
	p := NewPlayer(m, 48000)
	p.Loop = true
	out := make([]float32, 48000*2)
	p.Read(out)
	// C-4 at 8363 Hz over 32 samples is 261 Hz: about 523 crossings.
	if zc := zeroCrossings(out); zc < 470 || zc > 580 {
		t.Errorf("zero crossings %d, want about 523", zc)
	}
}

func TestSongEnds(t *testing.T) {
	m, _ := Load(buildMOD())
	p := NewPlayer(m, 48000)
	// One pattern of 64 rows at speed 6, tempo 125: 64*6*0.02 s = 7.68 s.
	out := make([]float32, 48000*2)
	total := 0
	for range 20 {
		n := p.Read(out)
		total += n
		if n < 48000 {
			break
		}
	}
	if !p.Finished() || total < 48000*7 || total > 48000*8 {
		t.Errorf("song rendered %d frames, finished=%v", total, p.Finished())
	}
}

// level is the peak absolute sample in out.
func level(out []float32) float32 {
	var peak float32
	for _, s := range out {
		if s > peak {
			peak = s
		} else if -s > peak {
			peak = -s
		}
	}
	return peak
}

func TestSeekAndPosition(t *testing.T) {
	m, _ := Load(buildMOD())
	p := NewPlayer(m, 48000)
	if p.Length() != 1 || p.Rows(0) != 64 || p.Rows(1) != 0 || p.Rows(-1) != 0 || p.Channels() != 4 {
		t.Fatalf("Length %d Rows %d/%d/%d Channels %d", p.Length(), p.Rows(0), p.Rows(1), p.Rows(-1), p.Channels())
	}
	// Half way through the one pattern: 32 rows of 6 ticks at 960 frames.
	p.Seek(0, 32)
	if o, r := p.Position(); o != 0 || r != 32 {
		t.Fatalf("Position after Seek(0, 32) = %d/%d", o, r)
	}
	out := make([]float32, 48000*2)
	total := 0
	for range 20 {
		n := p.Read(out)
		total += n
		if n < 48000 {
			break
		}
	}
	if want := 32 * 6 * 960; !p.Finished() || total != want {
		t.Fatalf("from row 32 the song rendered %d frames, want %d, finished=%v", total, want, p.Finished())
	}
	// Seeking a finished song restarts it, and the note on row 0 sounds.
	p.Seek(0, 0)
	if p.Finished() {
		t.Fatal("still finished after Seek")
	}
	if n := p.Read(out[:4800*2]); n != 4800 || level(out[:4800*2]) < 0.05 {
		t.Fatalf("after restarting: %d frames at peak %.3f", n, level(out[:4800*2]))
	}
	if o, r := p.Position(); o != 0 || r != 0 {
		t.Fatalf("Position after a tenth of a second = %d/%d, want row 0", o, r)
	}
	// Out of range positions clamp to the song.
	p.Seek(7, 500)
	if o, r := p.Position(); o != 0 || r != 63 {
		t.Fatalf("Position after Seek(7, 500) = %d/%d, want 0/63", o, r)
	}
	p.Seek(-3, -3)
	if o, r := p.Position(); o != 0 || r != 0 {
		t.Fatalf("Position after Seek(-3, -3) = %d/%d, want 0/0", o, r)
	}
}

func TestMuteAndSolo(t *testing.T) {
	m, _ := Load(buildMOD()) // only channel 0 plays a note
	p := NewPlayer(m, 48000)
	p.Loop = true
	out := make([]float32, 4800*2)
	read := func() float32 {
		p.Read(out)
		return level(out)
	}
	if read() < 0.05 {
		t.Fatal("song silent before any mute")
	}
	p.Mute(0, true)
	if !p.Muted(0) || p.Muted(1) || p.Muted(9) {
		t.Fatal("Muted did not read back")
	}
	if l := read(); l != 0 {
		t.Fatalf("muted channel still sounds: %.3f", l)
	}
	p.Mute(0, false)
	if read() < 0.05 {
		t.Fatal("channel silent after unmuting")
	}
	p.Solo(1, true)
	if !p.Soloed(1) || p.Soloed(0) {
		t.Fatal("Soloed did not read back")
	}
	if l := read(); l != 0 {
		t.Fatalf("soloing an empty channel left channel 0 audible: %.3f", l)
	}
	p.Solo(0, true)
	if read() < 0.05 {
		t.Fatal("soloed channel 0 is silent")
	}
	p.Solo(0, false)
	p.Solo(1, false)
	p.Solo(1, false) // clearing twice must not count down past zero
	if read() < 0.05 {
		t.Fatal("channel silent after clearing every solo")
	}
	p.Mute(99, true) // ignored
	if read() < 0.05 {
		t.Fatal("muting a channel outside the module silenced the song")
	}
}

// TestRealSongs plays real modules from the user's Downloads when present,
// which exercises the loaders and every effect a real tune uses.
func TestRealSongs(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, name := range []string{"2nd_pm.s3m", "space_debris.mod", "unreeeal_superhero_3.xm", "bz_pif.it"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(home, "Downloads", name))
			if err != nil {
				t.Skipf("no test song: %v", err)
			}
			m, err := Load(data)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%q: %d channels, %d samples, %d patterns, %d orders, speed %d tempo %d",
				m.Title, m.Channels, len(m.Samples), len(m.Patterns), len(m.Orders), m.Speed, m.Tempo)
			p := NewPlayer(m, 48000)
			out := make([]float32, 4800*2)
			var peak float32
			nonSilent := 0
			for range 300 { // 30 seconds
				n := p.Read(out)
				if n == 0 {
					break
				}
				for _, s := range out[:n*2] {
					if s > peak {
						peak = s
					}
					if s > 0.01 || s < -0.01 {
						nonSilent++
					}
				}
			}
			order, row := p.Position()
			t.Logf("peak %.3f, non-silent samples %d, position %d/%d", peak, nonSilent, order, row)
			if peak < 0.05 || nonSilent < 48000 {
				t.Errorf("song is nearly silent: peak %.3f", peak)
			}
		})
	}
}
