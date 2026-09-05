// Command bunyip-play plays an audio file through the engine's mixer and
// the native output device: WAV, Ogg Vorbis, MP3, MOD, S3M, XM or IT.
//
//	bunyip-play [-seconds N] [-volume 0.8] [-dump output.wav] file
//
// Zero seconds plays to the end. The volume defaults to 0.8. With -dump,
// the samples sent to the output device are also written as a WAV file;
// an audio output device is still required.
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
	"github.com/matjam/bunyip/internal/hook"
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
	// The driver from hook is what the output device pulls from; the
	// mixer it wraps is what this command sets up.
	pull := hook.NewMixer(rate)
	mixer := pull.Game().(*audio.Mixer)
	var voice *audio.Voice
	var player *tracker.Player
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mod", ".s3m", ".xm", ".it":
		m, err := tracker.Load(data)
		if err != nil {
			return err
		}
		fmt.Printf("%q: %d channels, %d samples, %d patterns, %d positions\n", m.Title, m.Channels, len(m.Samples), len(m.Patterns), len(m.Orders))
		player = tracker.NewPlayer(m, rate)
		voice = mixer.PlayStream(player, audio.PlayOptions{Volume: volume})
	case ".ogg", ".mp3":
		// Music streams from the file rather than being decoded up front.
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		music, err := mixer.OpenMusic(f, false)
		if err != nil {
			return err
		}
		defer music.Close()
		fmt.Printf("%s: streaming, %.1f s buffered\n", filepath.Base(path), music.Buffered())
		voice = mixer.PlayStream(music, audio.PlayOptions{Volume: volume})
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
	render := pull.Mix
	var tee *wavWriter
	if dump != "" {
		var err error
		if tee, err = newWAVWriter(dump, rate); err != nil {
			return err
		}
		defer tee.Close()
		render = func(out []float32) {
			pull.Mix(out)
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
