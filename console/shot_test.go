package console

import (
	"image/png"
	"os"
	"testing"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// TestShot writes a picture of the console for eyeballing; it is skipped
// unless CONSOLE_SHOT names a file.
func TestShot(t *testing.T) {
	path := os.Getenv("CONSOLE_SHOT")
	if path == "" {
		t.Skip("set CONSOLE_SHOT to write a picture")
	}
	r := newRig(t, Options{})
	w := ecs.NewWorld()
	w.SpawnWith(gfx.At(1, 2, 3), position{Pos: lin.V3(1, 2, 3), Name: "hero", Health: 10})
	w.AddSystem("physics", func(*ecs.World, float64) {})
	r.con.Attach("world", w)
	mix := audio.NewMixer(48000)
	if snd, err := mix.NewSound(audio.Sine(440, 2, 48000)); err == nil {
		mix.Play(snd, audio.PlayOptions{Bus: mix.Music(), Loop: true})
	}
	r.frame.Audio = mix
	r.con.Float("player.speed", new(float32), "how fast the player runs")
	tab := os.Getenv("CONSOLE_TAB")
	if tab == "" {
		tab = "Entities"
		r.con.SetOpen(true)
		r.con.Run("help")
		r.con.text = "set player.spe"
	}
	r.con.panels.open = true
	r.con.panels.tab = tabIndex(t, tab)
	r.con.panels.selected = w.Entities()[0]
	r.drawN(t, 3)
	img := r.capture(t)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
