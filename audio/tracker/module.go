// Package tracker loads and plays tracker music: ProTracker MOD (4, 6 and
// 8 channel variants) and ScreamTracker 3 S3M with sampled instruments.
// Player implements audio.Stream, so a module plays through the mixer like
// any other voice. Playback follows the fmoddoc2 description of ProTracker
// and ScreamTracker semantics: rows of cells, ticks per row, tempo in BPM,
// and the classic effect set.
package tracker

import (
	"bytes"
	"fmt"
)

// Module is a song in memory, in a form shared by every loader.
type Module struct {
	Title    string
	Channels int
	Samples  []Sample
	Patterns []Pattern
	Orders   []int // pattern index per song position
	Restart  int   // song position to loop back to
	Speed    int   // initial ticks per row
	Tempo    int   // initial BPM
	Pan      []float32
	Format   Format
	// GlobalVolume is 0..64 (S3M); MOD songs use 64.
	GlobalVolume int
}

// Format selects the period and effect semantics.
type Format int

const (
	FormatMOD Format = iota
	FormatS3M
)

// Sample is one instrument's PCM data, mono, -1..1.
type Sample struct {
	Name      string
	Data      []float32
	LoopStart int
	LoopEnd   int // > LoopStart when the sample loops
	Volume    int // 0..64
	Finetune  int // MOD: -8..7 eighths of a semitone
	C4Speed   int // S3M: playback rate of middle C
}

func (s *Sample) loops() bool { return s.LoopEnd > s.LoopStart+1 }

// Pattern is rows of cells, one cell per channel.
type Pattern struct {
	Rows [][]Cell
}

// Cell is one channel's entry in a row.
type Cell struct {
	Note       int // -1 none, NoteOff, or semitone index (octave*12+semi)
	Instrument int // 0 none
	Volume     int // -1 none, else 0..64
	Effect     effect
	Param      byte
}

// NoteOff stops the channel (S3M "^^").
const NoteOff = 254

// Load detects the format and parses a module.
func Load(data []byte) (*Module, error) {
	if len(data) > 48 && bytes.Equal(data[44:48], []byte("SCRM")) {
		return LoadS3M(data)
	}
	if m, err := LoadMOD(data); err == nil {
		return m, nil
	} else {
		return nil, fmt.Errorf("tracker: not a recognised module: %w", err)
	}
}
