package tiled

import (
	"encoding/base64"
	"encoding/binary"
	"sync"
	"testing"
)

// TestZstdSharedDecoder decodes zstd chunks from many goroutines through
// the one shared decoder, with corrupt data mixed in, and checks every
// good chunk decodes exactly and a bad one does not disturb the rest.
func TestZstdSharedDecoder(t *testing.T) {
	const chunks = 32
	texts := make([]string, chunks)
	want := make([][]uint32, chunks)
	for i := range texts {
		raw := make([]byte, 16*16*4)
		want[i] = make([]uint32, 16*16)
		for c := range want[i] {
			want[i][c] = uint32(i*1000 + c%17)
			binary.LittleEndian.PutUint32(raw[c*4:], want[i][c])
		}
		texts[i] = benchCompress(t, "zstd", raw)
	}
	bad := base64.StdEncoding.EncodeToString([]byte{0x28, 0xb5, 0x2f, 0xfd, 1, 2, 3})
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for round := range 20 {
				i := (g*7 + round) % chunks
				if round%5 == 0 {
					if _, err := decodeBase64Cells(bad, "zstd"); err == nil {
						t.Error("corrupt zstd data decoded")
					}
				}
				cells, err := decodeBase64Cells(texts[i], "zstd")
				if err != nil {
					t.Error(err)
					return
				}
				for c := range cells {
					if cells[c] != want[i][c] {
						t.Errorf("chunk %d cell %d: %d, want %d", i, c, cells[c], want[i][c])
						return
					}
				}
			}
		})
	}
	wg.Wait()
}
