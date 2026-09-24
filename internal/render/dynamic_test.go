package render

import (
	"bytes"
	"fmt"
	"math/bits"
	"testing"
)

// TestArenaOffsets checks that Add places each block at the next multiple
// of the alignment after the previous block, zeroes the padding between
// blocks and copies the block in, and that a reset arena reuses the same
// offsets for the same sequence of adds.
func TestArenaOffsets(t *testing.T) {
	cases := []struct {
		align int
		sizes []int
		want  []uint32
	}{
		{256, []int{64, 256, 1, 300, 16, 512, 3}, []uint32{0, 256, 512, 768, 1280, 1536, 2048}},
		{16, []int{4, 16, 20, 1, 33, 64}, []uint32{0, 16, 32, 64, 80, 128}},
		{64, []int{64, 64, 65, 63, 1}, []uint32{0, 64, 128, 256, 320}},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("align=%d", c.align), func(t *testing.T) {
			a := &Arena{align: c.align}
			for round := range 3 {
				a.Reset()
				var want []byte
				for i, size := range c.sizes {
					// A distinct fill per round and block, so stale bytes left
					// in the arena's storage by an earlier round would show.
					block := bytes.Repeat([]byte{byte(1 + round*16 + i)}, size)
					off, err := a.Add(block)
					if err != nil {
						t.Fatalf("round %d block %d: %v", round, i, err)
					}
					if off != c.want[i] {
						t.Errorf("round %d block %d (size %d): offset %d, want %d", round, i, size, off, c.want[i])
					}
					if off%uint32(c.align) != 0 {
						t.Errorf("round %d block %d: offset %d not a multiple of %d", round, i, off, c.align)
					}
					want = append(want, make([]byte, int(off)-len(want))...)
					want = append(want, block...)
				}
				if !bytes.Equal(a.Bytes(), want) {
					t.Errorf("round %d: arena bytes differ from the blocks at their offsets with zero padding", round)
				}
			}
		})
	}
}

// TestArenaAddErrors checks that an empty block and a block past the
// arena's cap are refused and leave the arena unchanged.
func TestArenaAddErrors(t *testing.T) {
	a := &Arena{align: 16}
	if _, err := a.Add(nil); err == nil {
		t.Error("empty block accepted")
	}
	if _, err := a.Add([]byte{1}); err != nil {
		t.Fatal(err)
	}
	big := &Arena{align: 1 << 30}
	if _, err := big.Add([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := big.Add([]byte{2}); err == nil {
		t.Error("block past the arena's cap accepted")
	}
	if len(big.Bytes()) != 1 {
		t.Errorf("refused block changed the arena to %d bytes", len(big.Bytes()))
	}
}

// TestArenaAddAmortised checks that filling an arena grows its storage a
// logarithmic number of times, and that a reset arena refilled to the
// same size allocates nothing, so a frame of many uniform blocks costs
// time linear in its size.
func TestArenaAddAmortised(t *testing.T) {
	const blocks, align = 1000, 256
	block := make([]byte, 64)
	fill := func(a *Arena) {
		for range blocks {
			if _, err := a.Add(block); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Doubling from one byte to the final size takes about log2(size)
	// reallocations; allow twice that for the runtime's size classes.
	limit := float64(2 * bits.Len(uint(blocks*align)))
	fresh := testing.AllocsPerRun(10, func() { fill(&Arena{align: align}) })
	if fresh > limit {
		t.Errorf("filling a new arena with %d blocks allocated %.0f times, want at most %.0f", blocks, fresh, limit)
	}
	a := &Arena{align: align}
	fill(a)
	reused := testing.AllocsPerRun(10, func() {
		a.Reset()
		fill(a)
	})
	if reused != 0 {
		t.Errorf("refilling a reset arena with %d blocks allocated %.0f times, want 0", blocks, reused)
	}
}

// BenchmarkArenaAdd measures one frame of uniform blocks: a reset and
// then one Add per block, at the worst common offset alignment.
func BenchmarkArenaAdd(b *testing.B) {
	block := make([]byte, 64)
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("blocks=%d", n), func(b *testing.B) {
			a := &Arena{align: 256}
			b.ReportAllocs()
			for b.Loop() {
				a.Reset()
				for range n {
					if _, err := a.Add(block); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
