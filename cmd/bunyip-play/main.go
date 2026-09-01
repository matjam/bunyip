// Command bunyip-play plays an audio file through the engine's mixer and
// the native output device: WAV, Ogg Vorbis, MP3, MOD or S3M.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/audio/tracker"
	"github.com/matjam/bunyip/internal/audioout"
)

func main() {
	seconds := flag.Float64("seconds", 0, "stop after this many seconds (0: play to the end)")
	volume := flag.Float64("volume", 0.8, "playback volume")
	dump := flag.String("dump", "", "also write what is sent to the device to this WAV file")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: bunyip-play [-seconds N] file")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *seconds, float32(*volume), *dump); err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-play:", err)
		os.Exit(1)
	}
}

func run(path string, seconds float64, volume float32, dump string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	const rate = 48000
	mixer := audio.NewMixer(rate)
	var voice *audio.Voice
	var player *tracker.Player
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mod", ".s3m":
		m, err := tracker.Load(data)
		if err != nil {
			return err
		}
		fmt.Printf("%q: %d channels, %d samples, %d patterns, %d positions\n", m.Title, m.Channels, len(m.Samples), len(m.Patterns), len(m.Orders))
		player = tracker.NewPlayer(m, rate)
		voice = mixer.PlayStream(player, audio.PlayOptions{Volume: volume})
	default:
		pcm, err := audio.Decode(data)
		if err != nil {
			return err
		}
		snd, err := mixer.NewSound(pcm)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %d channels at %d Hz, %.1f s\n", filepath.Base(path), pcm.Channels, pcm.Rate, float64(snd.Frames())/rate)
		voice = mixer.Play(snd, audio.PlayOptions{Volume: volume})
	}
	render := mixer.Mix
	var tee *wavWriter
	if dump != "" {
		var err error
		if tee, err = newWAVWriter(dump, rate); err != nil {
			return err
		}
		defer tee.Close()
		render = func(out []float32) {
			mixer.Mix(out)
			tee.Write(out)
		}
	}
	dev, err := audioout.Open(rate, render)
	if err != nil {
		return err
	}
	defer dev.Close()
	start := time.Now()
	for voice.Playing() {
		if seconds > 0 && time.Since(start).Seconds() >= seconds {
			break
		}
		if player != nil {
			o, r := player.Position()
			fmt.Printf("\rposition %02d row %02d  %5.1fs", o, r, time.Since(start).Seconds())
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println()
	return nil
}
