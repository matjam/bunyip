package tracker

import "testing"

// BenchmarkPlayerRead renders one mixer block of the bundled four
// channel MOD, which is the tick loop as the audio thread runs it.
func BenchmarkPlayerRead(b *testing.B) {
	m, err := Load(buildMOD())
	if err != nil {
		b.Fatal(err)
	}
	p := NewPlayer(m, 48000)
	p.Loop = true
	out := make([]float32, 512*2)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Read(out)
	}
}

// nnaPlayer is a player whose channels carry an instrument with a fading
// new-note action, so every new note detaches a background voice.
func nnaPlayer(b *testing.B) *Player {
	b.Helper()
	m, err := Load(buildMOD())
	if err != nil {
		b.Fatal(err)
	}
	m.Format = FormatIT
	m.Instruments = []Instrument{{NNA: NNAFade, Fadeout: 64}}
	inst := &m.Instruments[0]
	for i := range inst.SampleMap {
		inst.SampleMap[i] = 0
		inst.NoteMap[i] = i
	}
	p := NewPlayer(m, 48000)
	for i := range p.chans {
		ch := &p.chans[i]
		ch.voice.active = true
		ch.voice.inst = inst
		ch.voice.sample = &m.Samples[0]
		ch.voice.fade = 65536
		ch.inst = inst
	}
	return p
}

// BenchmarkNewNoteAction churns background voices: a new note on every
// channel, then a tick that retires the ones that have faded out. This is
// the audio thread allocating a voice per note.
func BenchmarkNewNoteAction(b *testing.B) {
	p := nnaPlayer(b)
	inst := p.chans[0].inst
	sample := p.chans[0].voice.sample
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for i := range p.chans {
			p.applyNNA(&p.chans[i], inst, sample, 48)
		}
		p.updateVoices()
	}
}
