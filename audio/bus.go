package audio

// Bus groups voices so they can be turned up, down or paused together.
// Every voice plays through one bus, or straight through the master when
// PlayOptions.Bus is nil; its gain is master × bus × voice. A mixer starts
// with three buses, Music, Effects and Dialogue, and games make more with
// NewBus. Bus methods are safe to call from the game loop.
type Bus struct {
	m      *Mixer
	name   string
	vol    float32
	paused bool
	mute   bool
	solo   bool

	reverb  *reverb   // the bus's own reverb, nil to share the mixer's
	sendBuf []float32 // reverb send for this block
}

// NewBus makes a named bus; the name is how Mixer.Bus finds it again. If a
// bus with that name already exists, it is returned instead.
func (m *Mixer) NewBus(name string) *Bus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.buses[name]; ok {
		return b
	}
	b := &Bus{m: m, name: name, vol: 1}
	m.buses[name] = b
	return b
}

// Bus looks a bus up by name, or returns nil when none has it.
func (m *Mixer) Bus(name string) *Bus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buses[name]
}

// Music is the bus for soundtrack voices, named "music".
func (m *Mixer) Music() *Bus { return m.music }

// Effects is the bus for sound effects, named "effects".
func (m *Mixer) Effects() *Bus { return m.effects }

// Dialogue is the bus for speech, named "dialogue". The name avoids
// confusion with Voice, which is any playing sound.
func (m *Mixer) Dialogue() *Bus { return m.dialogue }

// Name is the name the bus was made with.
func (b *Bus) Name() string { return b.name }

// SetVolume scales every voice on the bus; 1 is unity. The change ramps
// across the next mixed block, so it never clicks.
func (b *Bus) SetVolume(v float32) {
	b.m.mu.Lock()
	b.vol = v
	b.m.mu.Unlock()
}

// Volume returns the bus gain.
func (b *Bus) Volume() float32 {
	b.m.mu.Lock()
	defer b.m.mu.Unlock()
	return b.vol
}

// SetPaused holds every voice on the bus in place, silent, until resumed;
// voices started while the bus is paused wait too. The pause fades out
// over the block it lands in, so it never clicks.
func (b *Bus) SetPaused(p bool) {
	b.m.mu.Lock()
	b.paused = p
	b.m.mu.Unlock()
}

// Paused reports whether the bus is paused.
func (b *Bus) Paused() bool {
	b.m.mu.Lock()
	defer b.m.mu.Unlock()
	return b.paused
}

// SetMute silences every voice on the bus while leaving it playing, so
// unmuting picks up where the sound has got to. The gain ramps over one
// block, so it never clicks.
func (b *Bus) SetMute(mute bool) {
	b.m.mu.Lock()
	b.mute = mute
	b.m.mu.Unlock()
}

// Muted reports whether the bus is muted.
func (b *Bus) Muted() bool {
	b.m.mu.Lock()
	defer b.m.mu.Unlock()
	return b.mute
}

// SetSolo makes the bus one of the few heard: while any bus is soloed,
// every other bus (and voices on no bus) fall silent, still playing, the
// way a mixing desk's solo button auditions one group. Clearing the last
// solo brings the rest back.
func (b *Bus) SetSolo(solo bool) {
	b.m.mu.Lock()
	b.solo = solo
	b.m.mu.Unlock()
}

// Soloed reports whether the bus is soloed.
func (b *Bus) Soloed() bool {
	b.m.mu.Lock()
	defer b.m.mu.Unlock()
	return b.solo
}

// SetPaused holds every voice on every bus, for when the game loses focus
// or a pause menu opens; the engine calls it, so a game rarely has to.
// The block in which the pause lands fades out and the block after the
// resume fades back in, so neither clicks. Bus and voice pauses are kept
// separately, so resuming the mixer leaves them as they were.
func (m *Mixer) SetPaused(p bool) {
	m.mu.Lock()
	m.paused = p
	m.mu.Unlock()
}

// Paused reports whether the whole mixer is paused.
func (m *Mixer) Paused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused
}
