package tiled

// Benchmarks for decoding base64 layer data with each compression Tiled
// writes, for one 256 by 256 layer and for the same cells split into 256
// chunks of 16 by 16, as an infinite map stores them.

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func benchCells(n int) []byte {
	raw := make([]byte, n*4)
	for i := range n {
		gid := uint32(1 + (i/7)%13 + (i%256)/64)
		binary.LittleEndian.PutUint32(raw[i*4:], gid)
	}
	return raw
}

func benchCompress(tb testing.TB, kind string, raw []byte) string {
	var buf bytes.Buffer
	switch kind {
	case "":
		buf.Write(raw)
	case "zlib":
		w := zlib.NewWriter(&buf)
		w.Write(raw)
		w.Close()
	case "gzip":
		w := gzip.NewWriter(&buf)
		w.Write(raw)
		w.Close()
	case "zstd":
		enc, err := zstd.NewWriter(nil)
		if err != nil {
			tb.Fatal(err)
		}
		buf.Write(enc.EncodeAll(raw, nil))
		enc.Close()
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// BenchmarkLayerDecode decodes one 256 by 256 layer.
func BenchmarkLayerDecode(b *testing.B) {
	raw := benchCells(256 * 256)
	for _, kind := range []string{"", "zlib", "gzip", "zstd"} {
		name := kind
		if name == "" {
			name = "none"
		}
		b.Run(name, func(b *testing.B) {
			text := benchCompress(b, kind, raw)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := decodeBase64Cells(text, kind); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkChunkDecode decodes the same cells as 256 chunks of 16 by 16.
func BenchmarkChunkDecode(b *testing.B) {
	for _, kind := range []string{"zlib", "zstd"} {
		b.Run(kind, func(b *testing.B) {
			texts := make([]string, 256)
			for i := range texts {
				texts[i] = benchCompress(b, kind, benchCells(16*16))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for _, t := range texts {
					if _, err := decodeBase64Cells(t, kind); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
