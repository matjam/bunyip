package ktx2

import (
	"fmt"
	"image"
)

// BC7 packs a 4 by 4 block into sixteen bytes under one of eight modes.
// This encoder writes two of them and the decoder reads the same two:
//
//   - Mode 6 is one subset over RGBA, endpoints of seven bits and a
//     shared parity bit each, and four-bit indices. It is the best mode
//     for a block whose colours lie on one line, and the only one here
//     that carries alpha.
//   - Mode 1 is two subsets over RGB, chosen from sixty-four fixed
//     partitions, endpoints of six bits and one parity bit a subset, and
//     three-bit indices. It is the better mode for an opaque block with
//     two distinct clusters of colour, such as an edge.
//
// An opaque block is encoded both ways and the cheaper is kept; a block
// with any translucency takes mode 6, since mode 1 decodes every texel
// opaque. A file written elsewhere that uses the other six modes is
// uploaded to the GPU as it stands, and only the software fallback for a
// device without BC7 cannot read it.

// bc7Bits writes and reads the little-endian bit stream of one block.
type bc7Bits struct {
	b  [16]byte
	at int
}

func (s *bc7Bits) write(v uint32, n int) {
	for i := range n {
		if v>>i&1 != 0 {
			s.b[s.at>>3] |= 1 << (s.at & 7)
		}
		s.at++
	}
}

func (s *bc7Bits) read(n int) uint32 {
	var v uint32
	for i := range n {
		v |= uint32(s.b[s.at>>3]>>(s.at&7)&1) << i
		s.at++
	}
	return v
}

// unq6 turns a six-bit endpoint component and its parity bit into the
// eight-bit value the decoder interpolates with.
func unq6(v, p int) int {
	c := v<<1 | p
	return c<<1 | c>>6
}

// unq7 does the same for mode 6, whose seven bits and parity bit already
// make eight.
func unq7(v, p int) int { return v<<1 | p }

// quantise finds the stored value closest to a target, given the parity
// bit it shares. It starts from the arithmetic guess and checks its
// neighbours, since the bit replication in unq6 shifts the answer by one
// here and there.
func quantise(target, p, bits int, unq func(v, p int) int) int {
	maxv := 1<<bits - 1
	// A stored value and its parity bit together are bits+1 wide and
	// stand for the whole zero to 255 range, so the target is scaled into
	// that width and then halved to drop the parity bit off the end.
	full := 1<<(bits+1) - 1
	c := (target*full + 127) / 255
	guess := min(max((c-p+1)>>1, 0), maxv)
	best, bestErr := guess, 1<<30
	for v := max(guess-1, 0); v <= min(guess+1, maxv); v++ {
		d := unq(v, p) - target
		if d < 0 {
			d = -d
		}
		if d < bestErr {
			best, bestErr = v, d
		}
	}
	return best
}

// fitEndpoints returns the two ends of the spread of the named texels
// along their principal axis, over the first n channels.
func fitEndpoints(b block, which []uint8, n int) (e0, e1 [4]float64) {
	var sub block
	for i, at := range which {
		sub[i] = b[at]
	}
	// principalAxis reads all sixteen entries, so a shorter subset
	// repeats its last texel, which pulls the axis towards nothing it
	// does not already contain.
	for i := len(which); i < 16; i++ {
		sub[i] = sub[len(which)-1]
	}
	mean, axis := principalAxis(sub, n)
	lo, hi := 1e30, -1e30
	for _, at := range which {
		d := 0.0
		for i := range n {
			d += (float64(b[at][i]) - mean[i]) * axis[i]
		}
		lo, hi = min(lo, d), max(hi, d)
	}
	for i := range n {
		e0[i] = min(max(mean[i]+lo*axis[i], 0), 255)
		e1[i] = min(max(mean[i]+hi*axis[i], 0), 255)
	}
	return e0, e1
}

// bc7Subset is one subset's chosen endpoints, the indices its texels
// take and what that cost.
type bc7Subset struct {
	v0, v1  [4]int // the stored endpoint components
	p0, p1  int    // their parity bits
	indices [16]int
	err     int
}

// fitSubset quantises a subset's endpoints and picks each texel's index,
// trying each parity bit the mode allows and keeping the cheaper. bits
// is the endpoint precision without the parity bit, channels is how many
// channels the mode carries, and shared says the two endpoints have one
// parity bit between them, as mode 1 does.
func fitSubset(b block, which []uint8, bits, channels int, weights []int, shared bool) bc7Subset {
	f0, f1 := fitEndpoints(b, which, channels)
	unq := unq7
	if bits == 6 {
		unq = unq6
	}
	best := bc7Subset{err: 1 << 30}
	try := func(p0, p1 int) {
		var s bc7Subset
		s.p0, s.p1 = p0, p1
		var e0, e1 [4]int
		for i := range channels {
			s.v0[i] = quantise(int(f0[i]+0.5), p0, bits, unq)
			s.v1[i] = quantise(int(f1[i]+0.5), p1, bits, unq)
			e0[i], e1[i] = unq(s.v0[i], p0), unq(s.v1[i], p1)
		}
		if channels == 3 {
			e0[3], e1[3] = 255, 255
		}
		for _, at := range which {
			bestK, bestErr := 0, 1<<30
			for k, w := range weights {
				sum := 0
				for c := range channels {
					d := int(bc7Lerp(e0[c], e1[c], w)) - int(b[at][c])
					sum += d * d
				}
				if sum < bestErr {
					bestK, bestErr = k, sum
				}
			}
			s.indices[at] = bestK
			s.err += bestErr
		}
		if s.err < best.err {
			best = s
		}
	}
	if shared {
		try(0, 0)
		try(1, 1)
	} else {
		for p0 := range 2 {
			for p1 := range 2 {
				try(p0, p1)
			}
		}
	}
	return best
}

// anchorFix orders a subset's endpoints so the index at its anchor has
// its top bit clear, which is what lets that bit be left out of the
// block. Swapping the endpoints and inverting the indices leaves the
// colours the subset stands for unchanged.
func (s *bc7Subset) anchorFix(anchor int, levels int, which []uint8) {
	if s.indices[anchor] < levels/2 {
		return
	}
	s.v0, s.v1 = s.v1, s.v0
	s.p0, s.p1 = s.p1, s.p0
	for _, at := range which {
		s.indices[at] = levels - 1 - s.indices[at]
	}
}

// allTexels is the subset every texel belongs to in a one-subset mode.
var allTexels = func() []uint8 {
	out := make([]uint8, 16)
	for i := range out {
		out[i] = uint8(i)
	}
	return out
}()

// encodeMode6 fits the whole block as one subset over RGBA.
func encodeMode6(b block) (bc7Subset, int) {
	s := fitSubset(b, allTexels, 7, 4, bc7Weights4[:], false)
	s.anchorFix(0, 16, allTexels)
	return s, s.err
}

// encodeMode1 tries every two-subset partition and returns the best,
// with the partition it chose. It is only worth calling for an opaque
// block, since mode 1 has no alpha.
func encodeMode1(b block) (part int, subs [2]bc7Subset, err int) {
	best := 1 << 30
	var members [2][]uint8
	for p := range 64 {
		members[0], members[1] = members[0][:0], members[1][:0]
		for i, s := range bc7Partition2[p] {
			members[s] = append(members[s], uint8(i))
		}
		if len(members[0]) == 0 || len(members[1]) == 0 {
			continue // the format has no such partition, but be safe
		}
		var cand [2]bc7Subset
		total := 0
		for s := range 2 {
			cand[s] = fitSubset(b, members[s], 6, 3, bc7Weights3[:], true)
			total += cand[s].err
			if total >= best {
				break
			}
		}
		if total < best {
			cand[0].anchorFix(0, 8, members[0])
			cand[1].anchorFix(int(bc7Anchor2[p]), 8, members[1])
			best, part, subs = total, p, cand
		}
	}
	return part, subs, best
}

// encodeBC7Block writes one block. fast keeps to mode 6, which is a good
// deal quicker because mode 1 fits sixty-four partitions.
func encodeBC7Block(b block, out []byte, fast bool) {
	opaque := true
	for _, t := range b {
		if t[3] != 255 {
			opaque = false
			break
		}
	}
	six, sixErr := encodeMode6(b)
	// Mode 1 cannot hold alpha, and a block mode 6 already fits exactly
	// has nothing to gain, so both cases skip the partition search.
	if fast || !opaque || sixErr == 0 {
		writeMode6(six, out)
		return
	}
	part, subs, oneErr := encodeMode1(b)
	if oneErr < sixErr {
		writeMode1(part, subs, out)
		return
	}
	writeMode6(six, out)
}

// writeMode6 packs a one-subset RGBA block.
func writeMode6(s bc7Subset, out []byte) {
	var w bc7Bits
	w.write(1<<6, 7) // six zeroes then a one names mode 6
	for c := range 4 {
		w.write(uint32(s.v0[c]), 7)
		w.write(uint32(s.v1[c]), 7)
	}
	w.write(uint32(s.p0), 1)
	w.write(uint32(s.p1), 1)
	for i := range 16 {
		n := 4
		if i == 0 {
			n = 3 // the anchor's top bit is known to be zero
		}
		w.write(uint32(s.indices[i]), n)
	}
	copy(out, w.b[:])
}

// writeMode1 packs a two-subset RGB block.
func writeMode1(part int, subs [2]bc7Subset, out []byte) {
	var w bc7Bits
	w.write(1<<1, 2) // a zero then a one names mode 1
	w.write(uint32(part), 6)
	for c := range 3 {
		w.write(uint32(subs[0].v0[c]), 6)
		w.write(uint32(subs[0].v1[c]), 6)
		w.write(uint32(subs[1].v0[c]), 6)
		w.write(uint32(subs[1].v1[c]), 6)
	}
	w.write(uint32(subs[0].p0), 1)
	w.write(uint32(subs[1].p0), 1)
	anchor1 := int(bc7Anchor2[part])
	for i := range 16 {
		s := bc7Partition2[part][i]
		n := 3
		if i == 0 || i == anchor1 {
			n = 2
		}
		w.write(uint32(subs[s].indices[i]), n)
	}
	copy(out, w.b[:])
}

// decodeBC7Block unpacks a block written in mode 1 or mode 6. Any other
// mode returns false, since this package reads back only what it writes.
func decodeBC7Block(in []byte) (block, bool) {
	var r bc7Bits
	copy(r.b[:], in)
	mode := 0
	for mode < 8 && r.read(1) == 0 {
		mode++
	}
	switch mode {
	case 1:
		return decodeMode1(&r), true
	case 6:
		return decodeMode6(&r), true
	}
	return block{}, false
}

// decodeMode6 reads a one-subset RGBA block, the bit cursor already past
// the mode bits.
func decodeMode6(r *bc7Bits) block {
	var v0, v1 [4]int
	for c := range 4 {
		v0[c] = int(r.read(7))
		v1[c] = int(r.read(7))
	}
	p0, p1 := int(r.read(1)), int(r.read(1))
	var e0, e1 [4]int
	for c := range 4 {
		e0[c], e1[c] = unq7(v0[c], p0), unq7(v1[c], p1)
	}
	var b block
	for i := range 16 {
		n := 4
		if i == 0 {
			n = 3
		}
		w := bc7Weights4[r.read(n)]
		for c := range 4 {
			b[i][c] = bc7Lerp(e0[c], e1[c], w)
		}
	}
	return b
}

// decodeMode1 reads a two-subset RGB block.
func decodeMode1(r *bc7Bits) block {
	part := int(r.read(6))
	var v [2][2][4]int
	for c := range 3 {
		v[0][0][c] = int(r.read(6))
		v[0][1][c] = int(r.read(6))
		v[1][0][c] = int(r.read(6))
		v[1][1][c] = int(r.read(6))
	}
	p := [2]int{int(r.read(1)), int(r.read(1))}
	var e [2][2][4]int
	for s := range 2 {
		for c := range 3 {
			e[s][0][c] = unq6(v[s][0][c], p[s])
			e[s][1][c] = unq6(v[s][1][c], p[s])
		}
		e[s][0][3], e[s][1][3] = 255, 255
	}
	anchor1 := int(bc7Anchor2[part])
	var b block
	for i := range 16 {
		s := bc7Partition2[part][i]
		n := 3
		if i == 0 || i == anchor1 {
			n = 2
		}
		w := bc7Weights3[r.read(n)]
		for c := range 3 {
			b[i][c] = bc7Lerp(e[s][0][c], e[s][1][c], w)
		}
		b[i][3] = 255
	}
	return b
}

// encodeBC7 compresses an image into sixteen bytes a block.
func encodeBC7(src *image.RGBA, fast bool) []byte {
	return encodeBlocks(src, 16, func(b block, out []byte) { encodeBC7Block(b, out, fast) })
}

// decodeBC7 expands a BC7 image. A block in a mode this package does not
// write is an error, which is what a file from another tool may hold.
func decodeBC7(data []byte, w, h int) (*image.RGBA, error) {
	var bad bool
	img, err := decodeBlocks(data, w, h, 16, func(in []byte) block {
		b, ok := decodeBC7Block(in)
		if !ok {
			bad = true
		}
		return b
	})
	if err != nil {
		return nil, err
	}
	if bad {
		return nil, fmt.Errorf("ktx2: the BC7 data uses a mode this package does not decode; only modes 1 and 6 are read")
	}
	return img, nil
}
