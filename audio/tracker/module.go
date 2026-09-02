// Package tracker loads and plays tracker music: ProTracker MOD (4 to 32
// channels), ScreamTracker 3 S3M, FastTracker 2 XM and Impulse Tracker IT.
// Player implements audio.Stream, so a module plays through the mixer like
// any other voice; while it plays the game can Seek to a song position
// and row, read the Position, and Mute or Solo pattern channels. The four
// formats share one Module model; format-specific behaviour (period
// tables, slide units, effect semantics) is selected by Module.Format and
// the flags the loaders set.
//
// Load reads a module from bytes, sniffing the format (LoadMOD, LoadS3M,
// LoadXM and LoadIT take one each); a Module carries the song's name,
// orders, patterns, samples and instruments, so a game can show what is
// playing or drive visuals from the pattern data.
// NewPlayer renders it at the mixer's rate; the player reports its
// position (order and row) for syncing gameplay to the music and takes
// seeks and per-channel mute and solo. Playback is deterministic, so a
// module renders the same bytes on every platform.
package tracker

import (
	"bytes"
	"fmt"
)

// Module is a song in memory, in a form shared by every loader.
type Module struct {
	Title       string
	Channels    int
	Samples     []Sample
	Instruments []Instrument // empty for sample-only formats (MOD, S3M)
	Patterns    []Pattern
	Orders      []int // pattern index per song position
	Restart     int   // song position to loop back to
	Speed       int   // initial ticks per row
	Tempo       int   // initial BPM
	Pan         []float32
	ChannelVol  []int // 0..64 per channel; nil means 64
	Format      Format

	GlobalVolume int  // 0..128
	MixVolume    int  // 0..128: the file's master/mixing volume; per-channel gain follows it
	LinearSlides bool // XM/IT: slides move pitch by fractions of a semitone
	OldEffects   bool // IT "old effects" flag
	CompatGxx    bool // IT: Gxx shares memory with Exx/Fxx
	// Amiga filter (MOD E0x) and other per-format quirks are handled by the player.
}

// Format selects the period and effect semantics.
type Format int

const (
	FormatMOD Format = iota
	FormatS3M
	FormatXM
	FormatIT
)

// String names the format, as in "MOD" or "IT".
func (f Format) String() string {
	return [...]string{"MOD", "S3M", "XM", "IT"}[f]
}

// LoopType says how a sample repeats.
type LoopType uint8

const (
	LoopNone LoopType = iota
	LoopForward
	LoopPingPong
)

// Sample is one instrument's PCM data, mono, -1..1.
type Sample struct {
	Name         string
	Data         []float32
	LoopStart    int
	LoopEnd      int
	Loop         LoopType
	SusLoopStart int // IT sustain loop, active until key off
	SusLoopEnd   int
	SusLoop      LoopType
	Volume       int     // 0..64
	GlobalVolume int     // 0..64 (IT); 64 elsewhere
	Finetune     float32 // semitones added to every note
	C4Speed      int     // playback rate of the reference note (S3M/IT)
	RelativeNote int     // XM: semitones added to every note
	Pan          float32 // default pan, -1..1
	HasPan       bool    // whether Pan applies on note start
	Vibrato      AutoVibrato
}

func (s *Sample) loops() bool { return s.Loop != LoopNone && s.LoopEnd > s.LoopStart+1 }

// AutoVibrato is instrument-level vibrato (XM instruments, IT samples).
type AutoVibrato struct {
	Type  int // 0 sine, 1 ramp, 2 square, 3 random
	Sweep int // ticks to reach full depth
	Depth int
	Rate  int
}

// Envelope is a volume, panning or pitch envelope.
type Envelope struct {
	Points       []EnvPoint
	Enabled      bool
	Sustain      bool
	Loop         bool
	SustainStart int // point indices
	SustainEnd   int
	LoopStart    int
	LoopEnd      int
}

// EnvPoint is one node: Tick and a value in 0..64 for volume, -32..32 for
// panning and pitch (IT stores signed values; XM pan is 0..64 shifted).
type EnvPoint struct {
	Tick  int
	Value float32
}

// NNA is Impulse Tracker's new-note action.
type NNA uint8

const (
	NNACut NNA = iota
	NNAContinue
	NNAOff
	NNAFade
)

// Instrument maps notes to samples and shapes them with envelopes.
type Instrument struct {
	Name            string
	SampleMap       [120]int // note -> sample index, -1 none
	NoteMap         [120]int // note -> note actually played (IT); identity elsewhere
	VolEnv          Envelope
	PanEnv          Envelope
	PitchEnv        Envelope
	PitchIsFilter   bool // IT: the pitch envelope drives the filter cutoff instead
	Fadeout         int  // subtracted from a 65536-scale fade each tick after key off
	NNA             NNA
	DCT             int     // duplicate check type: 0 off, 1 note, 2 sample, 3 instrument
	DCA             int     // duplicate check action: 0 cut, 1 note off, 2 fade
	GlobalVolume    int     // 0..128 (IT); 128 elsewhere
	Pan             float32 // default pan
	HasPan          bool
	FilterCutoff    int // IT: 0..127, -1 unset
	FilterResonance int // IT: 0..127, -1 unset
}

// Pattern is rows of cells, one cell per channel.
type Pattern struct {
	Rows [][]Cell
}

// Cell is one channel's entry in a row.
type Cell struct {
	Note       int // NoteNone, NoteOff, NoteCut, NoteFade, or a note index
	Instrument int // 0 none
	VolCmd     volCmd
	VolParam   int
	Effect     effect
	Param      byte
}

// Special note values.
const (
	NoteNone = -1
	NoteOff  = 1000 // key off: release envelopes, then fade
	NoteCut  = 1001 // stop immediately
	NoteFade = 1002 // start fading without releasing sustain
)

// Load detects the format and parses a module.
func Load(data []byte) (*Module, error) {
	switch {
	case len(data) > 48 && bytes.Equal(data[44:48], []byte("SCRM")):
		return LoadS3M(data)
	case bytes.HasPrefix(data, []byte("Extended Module: ")):
		return LoadXM(data)
	case bytes.HasPrefix(data, []byte("IMPM")):
		return LoadIT(data)
	}
	m, err := LoadMOD(data)
	if err != nil {
		return nil, fmt.Errorf("tracker: not a recognised module: %w", err)
	}
	return m, nil
}
