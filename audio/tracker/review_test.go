package tracker_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/matjam/bunyip/audio/tracker"
)

// reviewMOD builds a one-sample, one-pattern MOD with the given cells in
// the first rows of channel 1.
func reviewMOD(cells [][4]byte, sample []int8) []byte {
	b := make([]byte, 1084)
	copy(b, "review")
	h := b[20:50] // sample 1 header
	binary.BigEndian.PutUint16(h[22:], uint16(len(sample)/2))
	h[25] = 64
	binary.BigEndian.PutUint16(h[28:], 1) // loop length of one word: no loop
	b[950] = 1
	copy(b[1080:], "M.K.")
	pat := make([]byte, 64*4*4)
	for r, row := range cells {
		copy(pat[r*16:], row[:])
	}
	b = append(b, pat...)
	for _, s := range sample {
		b = append(b, byte(s))
	}
	return b
}

// reviewCell encodes a MOD cell: period, sample, effect, parameter.
func reviewCell(period int, smp byte, eff byte, param byte) [4]byte {
	return [4]byte{(smp & 0xF0) | byte(period>>8&0x0F), byte(period), (smp&0x0F)<<4 | eff, param}
}

func reviewSample() []int8 {
	s := make([]int8, 200)
	for i := range s {
		s[i] = 100
	}
	return s
}

// A pattern delay (EEx) holds the row for x extra times and then moves
// on; it does not arm itself again on each repeat.
func TestPatternDelayEnds(t *testing.T) {
	m, err := tracker.Load(reviewMOD([][4]byte{reviewCell(428, 1, 0xE, 0xE1)}, reviewSample()))
	if err != nil {
		t.Fatal(err)
	}
	p := tracker.NewPlayer(m, 8000)
	out := make([]float32, 2*8000)
	for range 30 { // 30 seconds; the pattern lasts under 8
		p.Read(out)
	}
	if _, row := p.Position(); row == 0 && !p.Finished() {
		t.Error("the song never left the delayed row")
	}
}

// A delayed row's note plays once, not once per repeat.
func TestPatternDelayDoesNotRetrigger(t *testing.T) {
	m, err := tracker.Load(reviewMOD([][4]byte{reviewCell(428, 1, 0xE, 0xE2)}, reviewSample()))
	if err != nil {
		t.Fatal(err)
	}
	p := tracker.NewPlayer(m, 8000)
	out := make([]float32, 2*8000)
	n := p.Read(out)
	bursts, silent := 0, true
	for i := 0; i < n; i++ {
		on := out[i*2] != 0
		if on && silent {
			bursts++
		}
		silent = !on
	}
	if bursts != 1 {
		t.Errorf("the note started %d times during one delayed row, want 1", bursts)
	}
}

// A looping module whose orders name no playable pattern ends instead of
// scanning the order list forever under the mixer's lock.
func TestLoopWithoutPlayableOrderEnds(t *testing.T) {
	m := &tracker.Module{Channels: 1, Orders: []int{5}, Patterns: []tracker.Pattern{{}}, Pan: []float32{0}, Speed: 6, Tempo: 125, Format: tracker.FormatMOD}
	p := tracker.NewPlayer(m, 8000)
	p.Loop = true
	done := make(chan struct{})
	go func() { p.Read(make([]float32, 512)); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Read never returned")
	}
	if !p.Finished() {
		t.Error("player did not finish")
	}
}
