package audio

import (
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"time"
)

// RecordOptions bounds an explicit recording. Zero Capture values use the
// mixer's rate, mono, default input and a half-second capture ring.
type RecordOptions struct {
	Capture     CaptureOptions
	MaxDuration time.Duration // positive wall-clock and source-frame limit; zero means 30 seconds
}

// ErrCaptureDropped reports lost input samples. Recording stops rather than
// returning a silently incomplete recording as a success.
var ErrCaptureDropped = errors.New("audio: capture dropped samples")

// Recorder owns a capture stream and worker retaining a bounded PCM recording.
// It stops automatically at MaxDuration. Stop or Close joins the worker.
type Recorder struct{ state *recordState }

// Recording owns a capture stream and worker writing to an output. It retains
// only capture and conversion buffers, not the whole recording.
type Recording struct{ state *recordState }

type recordCapture interface {
	Rate() int
	Channels() int
	Read([]float32) int
	Dropped() int64
	Close()
}

var openRecordingCapture = func(m *Mixer, opts CaptureOptions) (recordCapture, error) { return m.OpenCapture(opts) }

type recordState struct {
	capture  recordCapture
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	err      error
	pcm      PCM
	limit    int64
	duration time.Duration
	write    func([]float32) error
	finish   func() error
}

func (m *Mixer) recordOptions(opts RecordOptions) (RecordOptions, int64, error) {
	m.mu.Lock()
	disabled, rate := m.noDevice, m.rate
	m.mu.Unlock()
	if disabled {
		return opts, 0, ErrNoDevice
	}
	var err error
	opts.Capture, err = normalizeCaptureOptions(opts.Capture, rate)
	if err != nil {
		return opts, 0, err
	}
	if opts.MaxDuration == 0 {
		opts.MaxDuration = 30 * time.Second
	}
	if opts.MaxDuration < 0 {
		return opts, 0, errors.New("audio: invalid recording duration, rate or channels")
	}
	rate = opts.Capture.Rate
	count := opts.MaxDuration.Seconds() * float64(rate)
	if count < 1 || count >= float64(math.MaxInt/4/opts.Capture.Channels) {
		return opts, 0, errors.New("audio: recording duration outside sample limits")
	}
	frames := int64(opts.MaxDuration/time.Second)*int64(rate) + int64(opts.MaxDuration%time.Second)*int64(rate)/int64(time.Second)
	return opts, frames, nil
}

// Record starts an explicit bounded in-memory recording. Reaching MaxDuration
// is a normal stop; dropped capture samples are an error. The caller owns it.
func (m *Mixer) Record(opts RecordOptions) (*Recorder, error) {
	opts, limit, err := m.recordOptions(opts)
	if err != nil {
		return nil, err
	}
	s, err := m.startRecording(opts, limit, nil, nil)
	if err != nil {
		return nil, err
	}
	return &Recorder{s}, nil
}

// RecordPCM streams interleaved signed 16-bit little-endian PCM without a
// container header. Rate and channels come from opts; the writer is borrowed.
// The worker can block in Write; its owner must unblock a blocked writer before
// Stop/Close can finish. Do not call Stop/Close from the writer's callback.
func (m *Mixer) RecordPCM(w io.Writer, opts RecordOptions) (*Recording, error) {
	opts, limit, err := m.recordOptions(opts)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errors.New("audio: nil recording writer")
	}
	encoded := make([]byte, 4096)
	s, err := m.startRecording(opts, limit, func(samples []float32) error { return writePCM16Buffer(w, samples, encoded) }, nil)
	if err != nil {
		return nil, err
	}
	return &Recording{s}, nil
}

// RecordWAV streams 16-bit PCM WAV at the borrowed writer's current offset.
// Stop finalizes the header and leaves the writer positioned after the WAV.
// The writer stays open. Its seek/write calls must not call Stop or Close.
// The configured maximum must fit RIFF's 32-bit size limit.
func (m *Mixer) RecordWAV(w io.WriteSeeker, opts RecordOptions) (*Recording, error) {
	opts, limit, err := m.recordOptions(opts)
	if err != nil {
		return nil, err
	}
	return m.recordWAV(w, opts, limit, nil)
}

// RecordWAVFile creates or replaces path and records 16-bit PCM WAV to it.
// It owns the file, finalizing and closing it on stop, including error paths.
func (m *Mixer) RecordWAVFile(path string, opts RecordOptions) (*Recording, error) {
	opts, limit, err := m.recordOptions(opts)
	if err != nil {
		return nil, err
	}
	if err := recordingWAVFormat(opts, limit); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	r, err := m.recordWAV(f, opts, limit, f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return r, nil
}

func recordingWAVFormat(opts RecordOptions, limit int64) error {
	if err := wavFormat(opts.Capture.Rate, opts.Capture.Channels); err != nil {
		return err
	}
	if uint64(limit)*uint64(opts.Capture.Channels)*2 > math.MaxUint32-36 {
		return errors.New("audio: recording maximum exceeds RIFF size")
	}
	return nil
}

func (m *Mixer) recordWAV(w io.WriteSeeker, opts RecordOptions, limit int64, owned io.Closer) (*Recording, error) {
	if w == nil {
		return nil, errors.New("audio: nil WAV recording writer")
	}
	if err := recordingWAVFormat(opts, limit); err != nil {
		return nil, err
	}
	base, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	if err := writeWAVHeader(w, opts.Capture.Rate, opts.Capture.Channels, 0); err != nil {
		return nil, err
	}
	var bytes uint32
	encoded := make([]byte, 4096)
	write := func(samples []float32) error {
		if err := writePCM16Buffer(w, samples, encoded); err != nil {
			return err
		}
		bytes += uint32(len(samples) * 2)
		return nil
	}
	finish := func() error {
		_, seekErr := w.Seek(base, io.SeekStart)
		var headerErr error
		if seekErr == nil {
			headerErr = writeWAVHeader(w, opts.Capture.Rate, opts.Capture.Channels, bytes)
		}
		_, endErr := w.Seek(base+44+int64(bytes), io.SeekStart)
		var closeErr error
		if owned != nil {
			closeErr = owned.Close()
		}
		return errors.Join(seekErr, headerErr, endErr, closeErr)
	}
	s, err := m.startRecording(opts, limit, write, finish)
	if err != nil {
		return nil, err
	}
	return &Recording{s}, nil
}

func (m *Mixer) startRecording(opts RecordOptions, limit int64, write func([]float32) error, finish func() error) (*recordState, error) {
	c, err := openRecordingCapture(m, opts.Capture)
	if err != nil {
		return nil, err
	}
	s := &recordState{capture: c, stop: make(chan struct{}), done: make(chan struct{}), limit: limit, duration: opts.MaxDuration, write: write, finish: finish,
		pcm: PCM{Rate: c.Rate(), Channels: c.Channels()}}
	go s.run()
	return s, nil
}

func (s *recordState) run() {
	var err error
	deadlineDone := make(chan struct{})
	deadline := time.AfterFunc(s.duration, func() { s.requestStop(); close(deadlineDone) })
	closed := false
	closeCapture := func() {
		if !closed {
			s.capture.Close()
			closed = true
		}
	}
	defer func() {
		closeCapture()
		if !deadline.Stop() {
			<-deadlineDone
		}
		if s.capture.Dropped() > 0 && !errors.Is(err, ErrCaptureDropped) {
			err = errors.Join(err, ErrCaptureDropped)
		}
		if s.finish != nil {
			err = errors.Join(err, s.finish())
		}
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	buf := make([]float32, 1024*s.pcm.Channels)
	left := s.limit
	for left > 0 {
		select {
		case <-s.stop:
			closeCapture()
		default:
		}
		if s.capture.Dropped() > 0 {
			err = ErrCaptureDropped
			return
		}
		want := min(int64(len(buf)), left*int64(s.pcm.Channels))
		n := s.capture.Read(buf[:want])
		if n < 0 || int64(n) > want || n%s.pcm.Channels != 0 {
			err = errors.New("audio: invalid capture sample count")
			return
		}
		if n > 0 {
			for _, sample := range buf[:n] {
				if !finite(sample) {
					err = errors.New("audio: non-finite captured sample")
					return
				}
			}
			if s.write != nil {
				err = s.write(buf[:n])
				if err != nil {
					return
				}
			} else {
				s.pcm.Samples = append(s.pcm.Samples, buf[:n]...)
			}
			left -= int64(n / s.pcm.Channels)
			continue
		}
		if closed {
			return
		}
		select {
		case <-s.stop:
			closeCapture()
		case <-ticker.C:
		}
	}
}

func (s *recordState) stopAndWait() {
	s.requestStop()
	<-s.done
}

func (s *recordState) requestStop() { s.stopOnce.Do(func() { close(s.stop); s.capture.Close() }) }
func (s *recordState) error() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// Stop stops capture, drains accepted buffered frames up to MaxDuration, and
// joins the worker. It returns an independent PCM copy, including partial data
// on error. Repeated calls return equivalent independent copies.
func (r *Recorder) Stop() (PCM, error) {
	r.state.stopAndWait()
	p := r.state.pcm
	p.Samples = append([]float32(nil), p.Samples...)
	return p, r.state.error()
}

// Close joins the recording. PCM remains available from later Stop calls.
func (r *Recorder) Close() error { r.state.stopAndWait(); return r.state.error() }

// Done closes after capture, output finalization and the worker have finished.
func (r *Recorder) Done() <-chan struct{} { return r.state.done }

// Err reports the terminal recording error after Done closes; before then nil.
func (r *Recorder) Err() error { return r.state.error() }

// Stop stops capture, drains accepted frames within the limit and joins the
// output worker, including WAV finalization and owned-file closure.
func (r *Recording) Stop() error { r.state.stopAndWait(); return r.state.error() }

// Close is equivalent to Stop and satisfies io.Closer.
func (r *Recording) Close() error { return r.Stop() }

// Done closes when capture and output processing have finished.
func (r *Recording) Done() <-chan struct{} { return r.state.done }

// Err reports the terminal recording error after Done closes; before then nil.
func (r *Recording) Err() error { return r.state.error() }

var _ io.Closer = (*Recorder)(nil)
var _ io.Closer = (*Recording)(nil)
