package soft

import "testing"

// TestClothBatchesAreIndependent checks that no two links in one of a
// cloth's batches share a particle, which is what lets a batch be solved
// in pieces on several goroutines with the result of solving it in one.
func TestClothBatchesAreIndependent(t *testing.T) {
	for _, size := range [][2]int{{2, 2}, {3, 7}, {16, 16}, {33, 20}, {64, 64}} {
		c := NewCloth(ClothSpec{Width: size[0], Height: size[1]})
		if c.batches[clothBatches] != len(c.links) {
			t.Fatalf("%v: batches cover %d of %d links", size, c.batches[clothBatches], len(c.links))
		}
		seen := make([]int, len(c.pos))
		for g := range clothBatches {
			for i := range seen {
				seen[i] = -1
			}
			for k := c.batches[g]; k < c.batches[g+1]; k++ {
				l := c.links[k]
				for _, p := range [2]int32{l.a, l.b} {
					if seen[p] >= 0 {
						t.Fatalf("%v: batch %d links %d and %d share particle %d", size, g, seen[p], k, p)
					}
					seen[p] = k
				}
			}
		}
	}
}
