package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// Isolate regressions that block OpenMusic so a failure cannot leave a
// spinning decoder goroutine in the test process.
func TestMusicTinyTracks(t *testing.T) {
	if os.Getenv("BUNYIP_TEST_TINY_MUSIC") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMusicTinyTracks$", "-test.v")
		cmd.Env = append(os.Environ(), "BUNYIP_TEST_TINY_MUSIC=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("tiny tracks subprocess: %v (%v)\n%s", err, ctx.Err(), out)
		}
		return
	}
	for _, channels := range []int{1, 2} {
		for _, loop := range []bool{false, true} {
			t.Run(fmt.Sprintf("empty/%dch/loop=%t", channels, loop), func(t *testing.T) {
				data := encodeWAV16(PCM{Rate: 48000, Channels: channels})
				music, err := NewMixer(48000).OpenMusic(bytes.NewReader(data), loop)
				if err != nil {
					t.Fatal(err)
				}
				defer music.Close()
				if music.Read(make([]float32, 2)) != 0 {
					t.Fatal("empty track emitted a frame")
				}
			})
		}
	}
	t.Run("incomplete-stereo", func(t *testing.T) {
		data := encodeWAV16(PCM{Rate: 48000, Channels: 2, Samples: []float32{0.25}})
		music, err := NewMixer(48000).OpenMusic(bytes.NewReader(data), true)
		if music != nil {
			defer music.Close()
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("OpenMusic error = %v, want unexpected EOF", err)
		}
	})
	for _, rate := range []int{22050, 48000, 96000} {
		for _, channels := range []int{1, 2} {
			for _, frames := range []int{1, 2, 3} {
				for _, loop := range []bool{false, true} {
					t.Run(fmt.Sprintf("%dHz/%dch/%dframes/loop=%t", rate, channels, frames, loop), func(t *testing.T) {
						src := PCM{Rate: rate, Channels: channels}
						for i := range frames * channels {
							src.Samples = append(src.Samples, float32(i+1)/16)
						}
						data := encodeWAV16(src)
						pcm, err := DecodeWAV(data)
						if err != nil {
							t.Fatal(err)
						}
						music, err := NewMixer(48000).OpenMusic(bytes.NewReader(data), loop)
						if err != nil {
							t.Fatal(err)
						}
						defer music.Close()
						check := func(start int) {
							t.Helper()
							wantFrames := 64
							if !loop {
								wantFrames = int(math.Ceil(float64(frames-start) * 48000 / float64(rate)))
							}
							music.wait(func() bool { return music.count >= 2*wantFrames && loop })
							out := make([]float32, 2*(wantFrames+1))
							read := out
							if loop {
								read = out[:2*wantFrames]
							}
							if got := music.Read(read); got != wantFrames {
								t.Fatalf("Read = %d frames, want %d", got, wantFrames)
							}
							for i := range wantFrames {
								pos := float64(start) + float64(i)*float64(rate)/48000
								a := int(math.Floor(pos))
								b := a + 1
								fraction := float32(pos - math.Floor(pos))
								if loop {
									a, b = a%frames, b%frames
								} else {
									a, b = min(a, frames-1), min(b, frames-1)
								}
								for c := range 2 {
									av, bv := pcm.Samples[a*channels+c%channels], pcm.Samples[b*channels+c%channels]
									want := av + (bv-av)*fraction
									if got := out[2*i+c]; math.Abs(float64(got-want)) > 1e-5 {
										t.Fatalf("frame %d channel %d = %g, want %g", i, c, got, want)
									}
								}
							}
							if !loop && music.Read(out) != 0 {
								t.Fatal("EOF emitted another tail")
							}
						}
						check(0)
						for _, frame := range []int{frames - 1, 0} {
							if err := music.Seek(float64(frame) / float64(rate)); err != nil {
								t.Fatal(err)
							}
							check(frame)
						}
						music.Close()
						music.Close()
						if got := music.Read(make([]float32, 2)); got != 0 {
							t.Fatalf("Read after Close = %d", got)
						}
						if err := music.Seek(0); err == nil {
							t.Fatal("Seek after Close succeeded")
						}
					})
				}
			}
		}
	}
}

type tinyMusicDecoder struct {
	decoder
	read func([]float32) (int, error)
	seek func(int64) error
}

func (d tinyMusicDecoder) Read(out []float32) (int, error) { return d.read(out) }
func (d tinyMusicDecoder) SeekFrame(frame int64) error {
	if d.seek != nil {
		return d.seek(frame)
	}
	return d.decoder.SeekFrame(frame)
}

func startTinyMusic(t *testing.T, dec decoder, loop bool) *Music {
	t.Helper()
	music := &Music{dec: dec, loop: loop, rate: 48000, seek: -1, ring: make([]float32, 2),
		rs: resampler{step: float64(dec.Rate()) / 48000}}
	music.cond = sync.NewCond(&music.mu)
	done := make(chan struct{})
	go func() { defer close(done); music.fill() }()
	t.Cleanup(func() {
		music.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Close did not stop decoder worker")
		}
	})
	return music
}

func TestMusicTinyDecoderTermination(t *testing.T) {
	rewindErr := errors.New("rewind failed")
	for _, tc := range []struct {
		name string
		read func([]float32) (int, error)
		seek func(int64) error
		want error
	}{
		{name: "empty", read: func([]float32) (int, error) { return 0, io.EOF }},
		{name: "no-progress", read: func([]float32) (int, error) { return 0, nil }, want: io.ErrNoProgress},
		{name: "partial-frame", read: func([]float32) (int, error) { return 1, nil }, want: io.ErrUnexpectedEOF},
		{name: "failed-rewind", read: func([]float32) (int, error) { return 0, io.EOF },
			seek: func(int64) error { return rewindErr }, want: rewindErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dec := tinyMusicDecoder{decoder: &memoryDecoder{pcm: PCM{Channels: 2, Rate: 48000}}, read: tc.read, seek: tc.seek}
			music := startTinyMusic(t, dec, true)
			music.wait(func() bool { return false })
			if err := music.Err(); !errors.Is(err, tc.want) {
				t.Fatalf("Err = %v, want %v", err, tc.want)
			}
			if got := music.Read(make([]float32, 2)); got != 0 {
				t.Fatalf("Read = %d, want 0", got)
			}
		})
	}
}

func TestMusicSeekDiscardsBlockedLoopAndTail(t *testing.T) {
	for _, loop := range []bool{false, true} {
		t.Run(fmt.Sprint(loop), func(t *testing.T) {
			dec := &memoryDecoder{pcm: PCM{Channels: 1, Rate: 48000, Samples: []float32{0.25, 0.5, 0.75}}}
			music := startTinyMusic(t, dec, loop)
			music.wait(func() bool { return music.count == len(music.ring) })
			// The old generation is blocked in push with more samples.
			if err := music.Seek(2.0 / 48000); err != nil {
				t.Fatal(err)
			}
			music.wait(func() bool { return music.count == 2 })
			out := make([]float32, 2)
			if n := music.Read(out); n != 1 || out[0] != 0.75 || out[1] != 0.75 {
				t.Fatalf("after Seek: %d frames %v, want [0.75 0.75]", n, out)
			}
			if !loop {
				music.wait(func() bool { return false })
				if music.Read(out) != 0 {
					t.Fatal("terminal tail emitted twice")
				}
			}
		})
	}
}

func TestMusicTinySampleWithEOF(t *testing.T) {
	for _, loop := range []bool{false, true} {
		t.Run(fmt.Sprint(loop), func(t *testing.T) {
			memory := &memoryDecoder{pcm: PCM{Channels: 1, Rate: 48000, Samples: []float32{0.25}}}
			dec := tinyMusicDecoder{decoder: memory, read: func(out []float32) (int, error) {
				n, _ := memory.Read(out)
				return n, io.EOF
			}}
			music := startTinyMusic(t, dec, loop)
			for i := range 3 {
				music.wait(func() bool { return music.count == 2 })
				out := make([]float32, 2)
				n := music.Read(out)
				if !loop && i > 0 {
					if n != 0 {
						t.Fatalf("Read after EOF = %d, want 0", n)
					}
				} else if n != 1 || out[0] != 0.25 || out[1] != 0.25 {
					t.Fatalf("Read = %d frames %v, want [0.25 0.25]", n, out)
				}
			}
		})
	}
}
