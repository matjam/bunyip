package audio

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeRecordCapture struct {
	mu             sync.Mutex
	samples        []float32
	pos            int
	rate, channels int
	dropped        int64
	once           sync.Once
	closed         chan struct{}
}

func (c *fakeRecordCapture) Rate() int     { return c.rate }
func (c *fakeRecordCapture) Channels() int { return c.channels }
func (c *fakeRecordCapture) Read(out []float32) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := copy(out, c.samples[c.pos:])
	c.pos += n
	return n
}
func (c *fakeRecordCapture) Dropped() int64 { c.mu.Lock(); defer c.mu.Unlock(); return c.dropped }
func (c *fakeRecordCapture) Close()         { c.once.Do(func() { close(c.closed) }) }

func recordingFixture(t *testing.T, samples []float32) (*Mixer, *fakeRecordCapture) {
	t.Helper()
	c := &fakeRecordCapture{samples: samples, rate: 1000, channels: 1, closed: make(chan struct{})}
	old := openRecordingCapture
	t.Cleanup(func() { openRecordingCapture = old })
	openRecordingCapture = func(_ *Mixer, opts CaptureOptions) (recordCapture, error) {
		if opts.Rate != c.rate || opts.Channels != c.channels {
			t.Fatal("capture format changed", opts)
		}
		return c, nil
	}
	return NewMixer(1000), c
}

func waitRecording(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recording worker did not finish")
	}
}

func TestRecorderStopOwnsAndCopiesPCM(t *testing.T) {
	m, c := recordingFixture(t, []float32{0.25, -0.5, 0.75})
	r, err := m.Record(RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Stop()
	if err != nil || p.Rate != 1000 || p.Channels != 1 || len(p.Samples) != 3 || p.Samples[1] != -0.5 {
		t.Fatal(p, err)
	}
	waitRecording(t, c.closed)
	waitRecording(t, r.Done())
	p.Samples[0] = 9
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := r.Stop()
	if err != nil || again.Samples[0] != 0.25 {
		t.Fatal("recorded data shared with result", again, err)
	}
}

func TestRecorderAutoStopsAtFrameLimit(t *testing.T) {
	m, c := recordingFixture(t, []float32{0, 0.125, 0.25, 0.375, 0.5})
	r, err := m.Record(RecordOptions{MaxDuration: 3 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	waitRecording(t, r.Done())
	p, err := r.Stop()
	if err != nil || len(p.Samples) != 3 || p.Samples[2] != 0.25 {
		t.Fatal(p, err)
	}
	waitRecording(t, c.closed)
}

func TestRecordingDeadlineClosesSilentCapture(t *testing.T) {
	m, c := recordingFixture(t, nil)
	r, err := m.Record(RecordOptions{MaxDuration: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	waitRecording(t, r.Done())
	waitRecording(t, c.closed)
	p, err := r.Stop()
	if err != nil || len(p.Samples) != 0 {
		t.Fatal(p, err)
	}
}

type blockedRecordWriter struct {
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedRecordWriter) Write(b []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(b), nil
}

func TestRecordingStopClosesCaptureThenJoinsBlockedWriter(t *testing.T) {
	m, c := recordingFixture(t, []float32{0.25, 0.5})
	w := &blockedRecordWriter{entered: make(chan struct{}), release: make(chan struct{})}
	r, err := m.RecordPCM(w, RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitRecording(t, w.entered)
	stopped := make(chan error, 1)
	go func() { stopped <- r.Stop() }()
	waitRecording(t, c.closed)
	select {
	case <-r.Done():
		t.Fatal("worker returned while Write was active")
	default:
	}
	close(w.release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not join")
	}
}

type failedRecordWriter struct{ err error }

func (w failedRecordWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRecordingReportsWriterAndDropErrors(t *testing.T) {
	m, c := recordingFixture(t, []float32{0.25})
	failure := errors.New("disk full")
	r, err := m.RecordPCM(failedRecordWriter{failure}, RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitRecording(t, r.Done())
	if !errors.Is(r.Err(), failure) || !errors.Is(r.Stop(), failure) {
		t.Fatal(r.Err())
	}
	waitRecording(t, c.closed)
	c.pos = 0
	c.dropped = 1
	r2, err := m.RecordPCM(io.Discard, RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitRecording(t, r2.Done())
	if !errors.Is(r2.Stop(), ErrCaptureDropped) {
		t.Fatal(r2.Err())
	}
}

func TestRecordingWAVOffsetAndBorrowedOwnership(t *testing.T) {
	m, _ := recordingFixture(t, []float32{0, 0.25, -0.5})
	f, err := os.CreateTemp(t.TempDir(), "borrowed-*.wav")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Write([]byte("prefix"))
	r, err := m.RecordWAV(f, RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if pos, err := f.Seek(0, io.SeekCurrent); err != nil || pos != 6+44+6 {
		t.Fatal(pos, err)
	}
	if _, err := f.Write([]byte("suffix")); err != nil {
		t.Fatal("borrowed writer closed", err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeWAV(data[6 : len(data)-6])
	if err != nil || len(p.Samples) != 3 || p.Samples[2] != -0.5 {
		t.Fatal(p, err)
	}
}

func TestRecordWAVFileFinalizedAndClosed(t *testing.T) {
	m, _ := recordingFixture(t, []float32{0.5, -0.5})
	path := filepath.Join(t.TempDir(), "capture.wav")
	r, err := m.RecordWAVFile(path, RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeWAV(data)
	if err != nil || len(p.Samples) != 2 {
		t.Fatal(p, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal("owned file remains open", err)
	}
}

func TestRecordValidationPrecedesCaptureAndFile(t *testing.T) {
	m := NewMixer(1000)
	old := openRecordingCapture
	t.Cleanup(func() { openRecordingCapture = old })
	openRecordingCapture = func(*Mixer, CaptureOptions) (recordCapture, error) {
		t.Error("invalid options opened capture")
		return nil, errors.New("unexpected capture")
	}
	for _, opts := range []RecordOptions{{MaxDuration: -1}, {MaxDuration: 1}, {Capture: CaptureOptions{Rate: -1}}, {Capture: CaptureOptions{Buffer: float32(math.NaN())}}, {Capture: CaptureOptions{Buffer: math.MaxFloat32}}, {Capture: CaptureOptions{DeviceID: "bad\x00id"}}, {Capture: CaptureOptions{Channels: 33}}, {MaxDuration: time.Duration(math.MaxInt64), Capture: CaptureOptions{Rate: math.MaxInt32}}} {
		if _, err := m.Record(opts); err == nil {
			t.Fatal("accepted", opts)
		}
	}
	driver{m}.SetDevice(false)
	path := filepath.Join(t.TempDir(), "must-not-exist.wav")
	if _, err := m.RecordWAVFile(path, RecordOptions{}); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("disabled recording created file", err)
	}
}

func TestWAVExportRoundTripAndErrors(t *testing.T) {
	p := PCM{Rate: 44100, Channels: 2, Samples: []float32{-1, 1, -0.5, 0.5, 0, 2}}
	var b bytes.Buffer
	if err := p.WriteWAV(&b); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWAV(b.Bytes())
	if err != nil || decoded.Rate != 44100 || decoded.Channels != 2 {
		t.Fatal(decoded, err)
	}
	for i, s := range decoded.Samples {
		if math.Abs(float64(s-max(-1, min(1, p.Samples[i])))) > 1.0/32768 {
			t.Fatal(i, s)
		}
	}
	failure := errors.New("write failed")
	if err := p.WriteWAV(failedRecordWriter{failure}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	for _, bad := range []PCM{{Rate: 0, Channels: 1}, {Rate: 1000, Channels: 2, Samples: []float32{1}}, {Rate: 1000, Channels: 1, Samples: []float32{float32(math.Inf(1))}}} {
		b.Reset()
		if err := bad.WriteWAV(&b); err == nil || b.Len() != 0 {
			t.Fatal("invalid PCM wrote header", err, b.Len())
		}
	}
}

type closeFunc func() error

func (f closeFunc) Close() error { return f() }

type failPCMWriter struct {
	*os.File
	calls   int
	failure error
}

func (w *failPCMWriter) Write(b []byte) (int, error) {
	w.calls++
	if w.calls == 2 {
		return 0, w.failure
	}
	return w.File.Write(b)
}

func TestRecordingJoinsWriteAndOwnedCloseErrors(t *testing.T) {
	m, _ := recordingFixture(t, []float32{0.25})
	f, err := os.CreateTemp(t.TempDir(), "failure.wav")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeErr, closeErr := errors.New("write failed"), errors.New("close failed")
	w := &failPCMWriter{File: f, failure: writeErr}
	opts, limit, err := m.recordOptions(RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	r, err := m.recordWAV(w, opts, limit, closeFunc(func() error { closed++; return closeErr }))
	if err != nil {
		t.Fatal(err)
	}
	waitRecording(t, r.Done())
	if err := r.Stop(); !errors.Is(err, writeErr) || !errors.Is(err, closeErr) {
		t.Fatal("lost recording errors", err)
	}
	r.Close()
	if closed != 1 {
		t.Fatal("owned output close count", closed)
	}
}

func TestRecordingRawPCMAndSoundExport(t *testing.T) {
	m, _ := recordingFixture(t, []float32{0.5, -0.5})
	var raw bytes.Buffer
	r, err := m.RecordPCM(&raw, RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw.Bytes(), []byte{0, 0x40, 0, 0xc0}) {
		t.Fatal(raw.Bytes())
	}
	s, err := m.NewSound(PCM{Rate: 1000, Channels: 1, Samples: []float32{0.5, -0.5}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sound.wav")
	if err := s.SaveWAV(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeWAV(b)
	if err != nil || p.Channels != 2 || p.Rate != 1000 || len(p.Samples) != 4 {
		t.Fatal(p, err)
	}
}
