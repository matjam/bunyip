package gfx

import (
	"bytes"
	"errors"
	"hash/fnv"
	"sync"
	"weak"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
)

// faceHeadBytes is how much of a font file identifies its contents: the
// collection header and the table directory, which carries a checksum of
// every table.
const faceHeadBytes = 4096

// faceSource identifies the bytes a face was parsed from. The array is
// held weakly, so a cache entry neither keeps a game's font bytes alive
// nor matches a later allocation that happens to reuse their address;
// the hash of the head catches bytes rewritten in place with another font.
type faceSource struct {
	data weak.Pointer[byte]
	size int
	head uint64
}

// parsedFaces maps font bytes to the tables parsed from them, held
// weakly, so every Font made from the same bytes (one per size, or the
// same emoji fallback behind several fonts) shares one parse while any of
// them is alive.
var parsedFaces struct {
	sync.Mutex
	fonts map[faceSource]weak.Pointer[font.Font]
}

// parseFace parses the first face of TrueType or OpenType bytes, or of a
// collection (.ttc) of them, reading only that face's tables. The tables
// are copied out of data, so data may be dropped afterwards. Bytes parsed
// before, while a face made from them is still alive, are not parsed
// again.
func parseFace(data []byte) (*font.Face, error) {
	if len(data) == 0 {
		return nil, errors.New("empty font data")
	}
	h := fnv.New64a()
	h.Write(data[:min(len(data), faceHeadBytes)])
	key := faceSource{data: weak.Make(&data[0]), size: len(data), head: h.Sum64()}
	parsedFaces.Lock()
	defer parsedFaces.Unlock()
	if ft := parsedFaces.fonts[key].Value(); ft != nil {
		return font.NewFace(ft), nil
	}
	// NewLoaders reads the collection's directories only; NewFont then
	// loads the tables of the one face used. Parsing every face of a
	// collection copies every face's tables, which for a colour emoji
	// collection is hundreds of megabytes.
	loaders, err := ot.NewLoaders(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(loaders) == 0 {
		return nil, errors.New("font collection has no faces")
	}
	ft, err := font.NewFont(loaders[0])
	if err != nil {
		return nil, err
	}
	if parsedFaces.fonts == nil {
		parsedFaces.fonts = map[faceSource]weak.Pointer[font.Font]{}
	}
	for k, w := range parsedFaces.fonts {
		if w.Value() == nil {
			delete(parsedFaces.fonts, k) // every face made from it is gone
		}
	}
	parsedFaces.fonts[key] = weak.Make(ft)
	return font.NewFace(ft), nil
}
