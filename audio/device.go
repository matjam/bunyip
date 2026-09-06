package audio

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/matjam/bunyip/internal/audioout"
)

// DeviceInfo identifies an output or input endpoint. IDs are opaque OS
// local selection identifiers, not list indices or user identities. They can become unavailable after device,
// driver or configuration changes. Linux endpoints include configured PCMs.
type DeviceInfo struct {
	ID      string // pass to SetOutputDevice or CaptureOptions.DeviceID
	Name    string // display name; not necessarily unique
	Default bool   // the default endpoint when enumerated
}

// ErrDeviceUnavailable means the requested endpoint could not be opened.
var ErrDeviceUnavailable = audioout.ErrUnavailable

// ErrDeviceUnsupported means this operating system or installation has no backend.
var ErrDeviceUnsupported = audioout.ErrUnsupported

type outputCloser interface{ Close() }
type outputSession struct {
	dev  outputCloser
	info DeviceInfo
}

var openOutput = func(id string, rate int, cb audioout.Callback) (outputCloser, error) {
	return audioout.OpenDevice(id, rate, cb)
}
var listOutputs = audioout.OutputDevices
var listInputs = audioout.InputDevices

// OutputDevices lists available playback endpoints without opening a stream.
// Explicit NoAudio and headless runs return ErrNoDevice.
func (m *Mixer) OutputDevices() ([]DeviceInfo, error) { return m.devices(listOutputs) }

// InputDevices lists capture endpoints without starting recording or requesting
// microphone access. Explicit NoAudio and headless runs return ErrNoDevice.
func (m *Mixer) InputDevices() ([]DeviceInfo, error) { return m.devices(listInputs) }

func (m *Mixer) devices(list func() ([]audioout.DeviceInfo, error)) ([]DeviceInfo, error) {
	m.mu.Lock()
	disabled := m.noDevice
	m.mu.Unlock()
	if disabled {
		return nil, ErrNoDevice
	}
	items, err := list()
	if err != nil {
		return nil, err
	}
	out := make([]DeviceInfo, len(items))
	for i, d := range items {
		out[i] = DeviceInfo(d)
	}
	return out, nil
}

// SetOutputDevice opens id and switches this mixer to it. Empty selects the
// system default routing endpoint. Failure leaves the previous output active.
// A successful switch can briefly produce silence; voices keep their positions.
// NewMixer stays device-free until this is explicitly called. The engine opens
// the default itself, except in NoAudio/headless runs, which return ErrNoDevice.
// Calls serialize with CloseOutput. Do not call either from Stream.Read or an
// audio callback (including OnDone): switching waits for device callbacks to end.
func (m *Mixer) SetOutputDevice(id string) error {
	m.beginDeviceChange()
	defer m.endDeviceChange()
	m.mu.Lock()
	disabled := m.noDevice
	m.mu.Unlock()
	if disabled {
		return ErrNoDevice
	}
	if strings.IndexByte(id, 0) >= 0 {
		return fmt.Errorf("%w: invalid device ID", ErrDeviceUnavailable)
	}
	info := DeviceInfo{ID: "", Name: "System default", Default: true}
	if id != "" {
		items, err := m.OutputDevices()
		if err != nil {
			return err
		}
		found := false
		for _, d := range items {
			if d.ID == id {
				info, found = d, true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: output %q", ErrDeviceUnavailable, id)
		}
	}
	next := &outputSession{info: info}
	dev, err := openOutput(id, m.rate, func(out []float32) { m.mixOutput(out, next) })
	if err != nil {
		if dev != nil {
			dev.Close()
		}
		return fmt.Errorf("audio: open output: %w", err)
	}
	if dev == nil {
		return errors.New("audio: output backend returned no device")
	}
	next.dev = dev
	m.mixMu.Lock()
	m.activeOutput = next
	m.mixMu.Unlock()
	m.deviceMu.Lock()
	old := m.output
	m.output = next
	m.deviceMu.Unlock()
	if old != nil {
		old.dev.Close()
	}
	return nil
}

// OutputDevice reports the chosen endpoint and whether this mixer owns an
// output. Empty ID with Default true means system-default routing; it does not
// identify the physical destination, which the OS may change. The result is
// selection state, not a live device-health query.
func (m *Mixer) OutputDevice() (DeviceInfo, bool) {
	m.deviceMu.Lock()
	defer m.deviceMu.Unlock()
	if m.output == nil {
		return DeviceInfo{}, false
	}
	return m.output.info, true
}

// CloseOutput stops and releases this mixer's output. Voices remain available
// for a later SetOutputDevice call. Repeated and concurrent calls are safe.
// Do not call it from an audio callback; see SetOutputDevice.
func (m *Mixer) CloseOutput() {
	m.beginDeviceChange()
	defer m.endDeviceChange()
	m.mixMu.Lock()
	m.activeOutput = nil
	m.mixMu.Unlock()
	m.deviceMu.Lock()
	old := m.output
	m.output = nil
	m.deviceMu.Unlock()
	if old != nil {
		old.dev.Close()
	}
}

func (m *Mixer) beginDeviceChange() {
	m.deviceMu.Lock()
	if m.deviceCond == nil {
		m.deviceCond = sync.NewCond(&m.deviceMu)
	}
	for m.changingDevice {
		m.deviceCond.Wait()
	}
	m.changingDevice = true
	m.deviceMu.Unlock()
}

func (m *Mixer) endDeviceChange() {
	m.deviceMu.Lock()
	m.changingDevice = false
	m.deviceCond.Broadcast()
	m.deviceMu.Unlock()
}

func (m *Mixer) mixOutput(out []float32, session *outputSession) {
	m.mixMu.Lock()
	if m.activeOutput != session {
		clear(out)
		m.mixMu.Unlock()
		return
	}
	m.mixLocked(out)
}
