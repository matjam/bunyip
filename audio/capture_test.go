package audio

import (
	"math"
	"os"
	"testing"
	"time"
)

func TestCaptureRingDropsOldest(t *testing.T) {
	c := &Capture{rate: 8, channels: 1, ring: make([]float32, 4)}
	c.write([]float32{1, 2, 3})
	dst := make([]float32, 8)
	if n := c.Read(dst); n != 3 || dst[0] != 1 || dst[2] != 3 {
		t.Fatalf("read %d samples: %v", n, dst[:3])
	}
	if n := c.Read(dst); n != 0 {
		t.Fatalf("read %d samples from an empty ring", n)
	}
	// Six samples into a ring of four: the two oldest are dropped.
	c.write([]float32{1, 2, 3, 4, 5, 6})
	if got := c.Buffered(); got != 4 {
		t.Fatalf("buffered %d, want 4", got)
	}
	if got := c.Dropped(); got != 2 {
		t.Fatalf("dropped %d, want 2", got)
	}
	n := c.Read(dst)
	if n != 4 || dst[0] != 3 || dst[3] != 6 {
		t.Fatalf("read %d samples: %v, want the newest four", n, dst[:n])
	}
	// A partial read leaves the rest in order.
	c.write([]float32{7, 8, 9})
	if n := c.Read(dst[:1]); n != 1 || dst[0] != 7 {
		t.Fatalf("partial read gave %d samples: %v", n, dst[:1])
	}
	if n := c.Read(dst); n != 2 || dst[0] != 8 || dst[1] != 9 {
		t.Fatalf("rest of the block gave %d samples: %v", n, dst[:n])
	}
	// Level is the RMS of the block the device delivered last.
	c.write([]float32{0.5, -0.5, 0.5, -0.5})
	if l := c.Level(); math.Abs(float64(l)-0.5) > 1e-6 {
		t.Errorf("Level %v, want 0.5", l)
	}
}

func TestCaptureRefusedWithoutADevice(t *testing.T) {
	m := NewMixer(48000)
	driver{m}.SetDevice(false)
	if _, err := m.OpenCapture(CaptureOptions{}); err != ErrNoDevice {
		t.Fatalf("OpenCapture without a device gave %v, want ErrNoDevice", err)
	}
}

// TestCaptureDevice records from the machine's default input for a
// moment. It skips when there is no capture device, which is the usual
// case on a build machine.
func TestCaptureDevice(t *testing.T) {
	if os.Getenv("BUNYIP_TEST_AUDIO_HARDWARE") != "1" || testing.Short() {
		t.Skip("microphone integration test requires BUNYIP_TEST_AUDIO_HARDWARE=1 without -short")
	}
	m := NewMixer(48000)
	c, err := m.OpenCapture(CaptureOptions{})
	if err != nil {
		t.Skip("no capture device:", err)
	}
	defer c.Close()
	if c.Rate() != 48000 || c.Channels() != 1 {
		t.Errorf("opened %d Hz, %d channels, want 48000 and 1", c.Rate(), c.Channels())
	}
	buf := make([]float32, 4096)
	got := 0
	deadline := time.Now().Add(2 * time.Second)
	for got == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		got += c.Read(buf)
	}
	if got == 0 {
		t.Fatal("the device delivered nothing in two seconds")
	}
	for i, s := range buf[:min(got, len(buf))] {
		if math.IsNaN(float64(s)) || s < -1.01 || s > 1.01 {
			t.Fatalf("sample %d is %v, want a normalised float", i, s)
		}
	}
}
