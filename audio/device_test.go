package audio

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/matjam/bunyip/internal/audioout"
)

type fakeOutput struct {
	cb      audioout.Callback
	closed  int
	onClose func()
}

func (d *fakeOutput) Close() {
	d.closed++
	if d.onClose != nil {
		d.onClose()
	}
}

func mockOutputs(t *testing.T, m *Mixer) *[]*fakeOutput {
	t.Helper()
	oldOpen, oldList := openOutput, listOutputs
	t.Cleanup(func() { m.CloseOutput(); openOutput, listOutputs = oldOpen, oldList })
	listOutputs = func() ([]audioout.DeviceInfo, error) {
		return []audioout.DeviceInfo{{ID: "speakers", Name: "Speakers", Default: true}, {ID: "headset", Name: "Headset"}}, nil
	}
	var devices []*fakeOutput
	openOutput = func(id string, rate int, cb audioout.Callback) (outputCloser, error) {
		// Native implementations may call synchronously before Open returns.
		prime := []float32{1, 1, 1, 1}
		cb(prime)
		for _, s := range prime {
			if s != 0 {
				t.Error("uncommitted endpoint advanced mixer")
			}
		}
		d := &fakeOutput{cb: cb, onClose: func() { m.OutputDevice(); m.SetMasterVolume(1) }}
		devices = append(devices, d)
		return d, nil
	}
	return &devices
}

func TestOutputDeviceTransactionalSwitch(t *testing.T) {
	m := NewMixer(48000)
	devices := mockOutputs(t, m)
	v := m.PlayStream(&rampStream{count: 1000}, PlayOptions{})
	if _, ok := m.OutputDevice(); ok {
		t.Fatal("NewMixer opened hardware")
	}
	if err := m.SetOutputDevice("speakers"); err != nil {
		t.Fatal(err)
	}
	first := (*devices)[0]
	first.cb(make([]float32, 8))
	position := v.Position()
	if position == 0 {
		t.Fatal("active endpoint did not advance")
	}
	if err := m.SetOutputDevice("headset"); err != nil {
		t.Fatal(err)
	}
	if first.closed != 1 {
		t.Fatal("old endpoint not closed exactly once")
	}
	if got := v.Position(); got != position {
		t.Fatalf("candidate priming moved playhead: %v -> %v", position, got)
	}
	out := []float32{1, 1}
	first.cb(out)
	if out[0] != 0 || out[1] != 0 || v.Position() != position {
		t.Fatal("retired callback remained active")
	}
	(*devices)[1].cb(make([]float32, 8))
	if v.Position() <= position {
		t.Fatal("new callback did not advance")
	}
	if d, ok := m.OutputDevice(); !ok || d.ID != "headset" {
		t.Fatal(d, ok)
	}
	m.CloseOutput()
	m.CloseOutput()
	if (*devices)[1].closed != 1 {
		t.Fatal("repeated CloseOutput closed native device twice")
	}
	if _, ok := m.OutputDevice(); ok {
		t.Fatal("closed output still selected")
	}
	if err := m.SetOutputDevice(""); err != nil {
		t.Fatal(err)
	}
	if d, ok := m.OutputDevice(); !ok || d.ID != "" || !d.Default {
		t.Fatal("default routing identity", d, ok)
	}
}

func TestOutputDeviceFailurePreservesOld(t *testing.T) {
	m := NewMixer(48000)
	devices := mockOutputs(t, m)
	if err := m.SetOutputDevice("speakers"); err != nil {
		t.Fatal(err)
	}
	first := (*devices)[0]
	failure := errors.New("start failed")
	bad := &fakeOutput{}
	openOutput = func(_ string, _ int, cb audioout.Callback) (outputCloser, error) {
		cb(make([]float32, 8))
		return bad, failure
	}
	if err := m.SetOutputDevice("headset"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if first.closed != 0 || bad.closed != 1 {
		t.Fatal("incorrect failure cleanup")
	}
	if d, ok := m.OutputDevice(); !ok || d.ID != "speakers" {
		t.Fatal(d, ok)
	}
	if err := m.SetOutputDevice("missing"); !errors.Is(err, ErrDeviceUnavailable) {
		t.Fatal(err)
	}
	if err := m.SetOutputDevice("head\x00set"); !errors.Is(err, ErrDeviceUnavailable) {
		t.Fatal(err)
	}
}

func TestDisabledMixerNeverEnumeratesOrOpens(t *testing.T) {
	m := NewMixer(48000)
	mockOutputs(t, m)
	oldInputs := listInputs
	t.Cleanup(func() { listInputs = oldInputs })
	listOutputs = func() ([]audioout.DeviceInfo, error) { t.Error("enumerated disabled output"); return nil, nil }
	listInputs = func() ([]audioout.DeviceInfo, error) { t.Error("enumerated disabled input"); return nil, nil }
	openOutput = func(string, int, audioout.Callback) (outputCloser, error) {
		t.Error("opened disabled output")
		return nil, nil
	}
	driver{m}.SetDevice(false)
	if _, err := m.OutputDevices(); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
	if _, err := m.InputDevices(); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
	if err := m.SetOutputDevice(""); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
	if _, err := m.OpenCapture(CaptureOptions{DeviceID: "mic"}); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
}

func TestOutputSwitchWaitsForOldBlock(t *testing.T) {
	m := NewMixer(48000)
	devices := mockOutputs(t, m)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.PlayStream(callbackStream{read: func(out []float32) int {
		once.Do(func() { close(entered); <-release })
		clear(out)
		return len(out) / 2
	}}, PlayOptions{})
	if err := m.SetOutputDevice("speakers"); err != nil {
		t.Fatal(err)
	}
	blockDone := make(chan struct{})
	go func() { (*devices)[0].cb(make([]float32, 4)); close(blockDone) }()
	<-entered
	changed := make(chan error, 1)
	go func() { changed <- m.SetOutputDevice("headset") }()
	select {
	case err := <-changed:
		t.Fatalf("switched during old block: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	<-blockDone
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("switch did not complete")
	}
}

func TestCaptureDeviceSelectionAndValidation(t *testing.T) {
	old := openCaptureInput
	t.Cleanup(func() { openCaptureInput = old })
	opened := 0
	openCaptureInput = func(id string, rate, channels int, cb audioout.CaptureCallback) (*audioout.CaptureDevice, error) {
		opened++
		if id != "mic-uid" || rate != 24000 || channels != 2 {
			t.Fatalf("capture selection: %q %d %d", id, rate, channels)
		}
		cb([]float32{0.25, -0.25})
		return nil, nil
	}
	m := NewMixer(48000)
	c, err := m.OpenCapture(CaptureOptions{DeviceID: "mic-uid", Rate: 24000, Channels: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	out := make([]float32, 2)
	if c.Read(out) != 2 || out[0] != 0.25 || out[1] != -0.25 {
		t.Fatal("selected input did not deliver PCM", out)
	}
	for _, opts := range []CaptureOptions{{Rate: -1}, {Channels: -1}, {Channels: 33}, {Buffer: -1}, {Buffer: float32(math.NaN())}, {Buffer: float32(math.Inf(1))}, {Buffer: math.MaxFloat32}} {
		if _, err := m.OpenCapture(opts); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
	if opened != 1 {
		t.Fatal("invalid options reached device", opened)
	}
}
