package audio

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"github.com/matjam/bunyip/internal/audioout"
)

// ErrNoDevice is returned by OpenCapture when the run has no audio
// device at all, which is what a headless run or Config.NoAudio asks
// for.
var ErrNoDevice = errors.New("audio: this run has no audio device")

// CaptureOptions shape a capture stream. Zero values mean the defaults
// noted.
type CaptureOptions struct {
	Rate     int     // samples per second; 0 is the mixer's rate
	Channels int     // 0 is 1, mono
	Buffer   float32 // seconds held before the oldest samples are dropped; 0 is 0.5
}

// Capture is a running recording from the machine's default input. The
// device fills a ring buffer from its own thread and the game drains it
// with Read, which never blocks and never waits for the device. Samples
// that are not read in time are dropped, oldest first, so a game that
// stops reading falls behind by at most the buffer.
//
// A Capture is not a Stream: to play what is recorded, feed the samples
// to a Stream of the game's own, or to a Sound built from them.
type Capture struct {
	rate      int
	channels  int
	dev       *audioout.CaptureDevice
	closeOnce sync.Once

	mu      sync.Mutex
	ring    []float32
	start   int   // where the oldest unread sample sits
	count   int   // how many samples are unread
	dropped int64 // samples the device overwrote before they were read

	level atomic.Uint32 // the last block's RMS, as float32 bits
}

// OpenCapture starts recording from the machine's default input, the one
// the desktop is set to. It returns an error when the run has no audio
// device, when the operating system has no capture backend, or when the
// device refuses to open, which is what happens on a machine with no
// microphone and on macOS when the player has not granted microphone
// access. Close the Capture when the game is done with it.
//
// The stream is separate from the mixer: what it records is not played
// back unless the game plays it.
func (m *Mixer) OpenCapture(opts CaptureOptions) (*Capture, error) {
	m.mu.Lock()
	noDevice, rate := m.noDevice, m.rate
	m.mu.Unlock()
	if noDevice {
		return nil, ErrNoDevice
	}
	if opts.Rate > 0 {
		rate = opts.Rate
	}
	channels := max(opts.Channels, 1)
	seconds := opts.Buffer
	if seconds <= 0 {
		seconds = 0.5
	}
	c := &Capture{rate: rate, channels: channels,
		ring: make([]float32, max(int(seconds*float32(rate))*channels, channels))}
	dev, err := audioout.OpenCapture(rate, channels, c.write)
	if err != nil {
		return nil, err
	}
	c.dev = dev
	return c, nil
}

// Rate is the sample rate the stream records at.
func (c *Capture) Rate() int { return c.rate }

// Channels is how many channels each frame has; 1 is mono.
func (c *Capture) Channels() int { return c.channels }

// write takes a block from the device's thread into the ring, dropping
// the oldest samples when the game has not kept up.
func (c *Capture) write(in []float32) {
	var sum float64
	for _, s := range in {
		sum += float64(s) * float64(s)
	}
	if len(in) > 0 {
		c.level.Store(math.Float32bits(float32(math.Sqrt(sum / float64(len(in))))))
	}
	c.mu.Lock()
	n := len(c.ring)
	for _, s := range in {
		if c.count == n {
			c.start = (c.start + 1) % n
			c.count--
			c.dropped++
		}
		c.ring[(c.start+c.count)%n] = s
		c.count++
	}
	c.mu.Unlock()
}

// Read copies at most len(dst) recorded samples into dst and reports how
// many it copied, which is 0 when nothing has arrived since the last
// call. It never blocks and never waits for the device. With more than
// one channel the samples are interleaved, so a caller that works in
// frames reads a multiple of Channels.
func (c *Capture) Read(dst []float32) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := min(len(dst), c.count)
	ring := c.ring
	first := min(n, len(ring)-c.start)
	copy(dst[:first], ring[c.start:c.start+first])
	copy(dst[first:n], ring[:n-first])
	c.start = (c.start + n) % len(ring)
	c.count -= n
	return n
}

// Level is the root mean square of the samples the device delivered
// most recently, from 0 to 1, which is what a level meter draws. It
// moves at the device's own block rate, a few hundred times a second,
// and holds its last value when the stream is closed.
func (c *Capture) Level() float32 {
	return math.Float32frombits(c.level.Load())
}

// Buffered is how many recorded samples are waiting to be read.
func (c *Capture) Buffered() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// Dropped counts the samples the device recorded and overwrote because
// the game did not read them in time. A rising count means either a
// longer CaptureOptions.Buffer or more frequent reads.
func (c *Capture) Dropped() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropped
}

// Close stops the stream and releases the device. Samples already
// buffered can still be read. Closing twice, or from two goroutines,
// does the work once; the second call waits for the first.
func (c *Capture) Close() {
	c.closeOnce.Do(func() {
		if c.dev != nil {
			// The device waits for its own reader, which takes the ring's
			// lock, so this must not be holding it.
			c.dev.Close()
		}
	})
}
