package ktx2

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func TestMipLevelBounds(t *testing.T) {
	for _, tc := range []struct {
		name                string
		width, height, full int
	}{
		{"single texel", 1, 1, 1},
		{"square", 8, 8, 4},
		{"non power of two", 7, 7, 3},
		{"wide", 9, 2, 4},
		{"tall", 2, 9, 4},
		{"maximum width", maxDimension, 1, 17},
		{"maximum height", 1, maxDimension, 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for count := 1; count <= tc.full+1; count++ {
				t.Run(fmt.Sprintf("levels=%d", count), func(t *testing.T) {
					f := &File{Format: BC1RGBUnorm, Width: tc.width, Height: tc.height}
					w, h := tc.width, tc.height
					for range count {
						f.Levels = append(f.Levels, make([]byte, f.Format.LevelBytes(w, h)))
						w, h = max(w/2, 1), max(h/2, 1)
					}
					wantErr := count > tc.full
					if _, err := f.Bytes(); (err != nil) != wantErr {
						t.Errorf("Bytes error = %v, want error %v", err, wantErr)
					}
					got, err := Parse(mipContainer(f))
					if (err != nil) != wantErr {
						t.Fatalf("Parse error = %v, want error %v", err, wantErr)
					}
					if !wantErr && (got.Width != tc.width || got.Height != tc.height || len(got.Levels) != count) {
						t.Errorf("Parse = %dx%d with %d levels", got.Width, got.Height, len(got.Levels))
					}
				})
			}
		})
	}
}

func TestParseMipHeaderBounds(t *testing.T) {
	f := &File{Format: BC1RGBUnorm, Width: 1, Height: 1, Levels: [][]byte{make([]byte, 8)}}
	for _, tc := range []struct {
		name    string
		offset  int
		value   uint32
		wantErr bool
	}{
		{"zero levels reads one", 40, 0, false},
		{"huge level count", 40, ^uint32(0), true},
		{"zero width", 20, 0, true},
		{"zero height", 24, 0, true},
		{"width exceeds cap", 20, maxDimension + 1, true},
		{"height exceeds cap", 24, maxDimension + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mipContainer(f)
			binary.LittleEndian.PutUint32(data[tc.offset:], tc.value)
			got, err := Parse(data)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Parse error = %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && len(got.Levels) != 1 {
				t.Errorf("got %d levels, want one", len(got.Levels))
			}
		})
	}
}

// mipContainer builds the header, index and payload without Bytes, so
// serializer validation cannot prevent malformed inputs reaching Parse.
func mipContainer(f *File) []byte {
	data := make([]byte, headerSize+24*len(f.Levels))
	copy(data, identifier[:])
	binary.LittleEndian.PutUint32(data[12:], uint32(f.Format))
	binary.LittleEndian.PutUint32(data[16:], 1)
	binary.LittleEndian.PutUint32(data[20:], uint32(f.Width))
	binary.LittleEndian.PutUint32(data[24:], uint32(f.Height))
	binary.LittleEndian.PutUint32(data[36:], 1)
	binary.LittleEndian.PutUint32(data[40:], uint32(len(f.Levels)))
	for i, level := range f.Levels {
		for len(data)%levelAlign != 0 {
			data = append(data, 0)
		}
		at := headerSize + 24*i
		binary.LittleEndian.PutUint64(data[at:], uint64(len(data)))
		binary.LittleEndian.PutUint64(data[at+8:], uint64(len(level)))
		binary.LittleEndian.PutUint64(data[at+16:], uint64(len(level)))
		data = append(data, level...)
	}
	return data
}
