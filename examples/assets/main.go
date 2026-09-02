// Command assets shows the asset and save packages: a directory of
// images is decoded on worker goroutines behind a progress bar, textures
// are created on the main thread as each finishes, files that change on
// disk are reloaded live (the example rewrites one every two seconds to
// prove it), P packs the directory into assets.pak, and a settings file
// remembers how many times the example has run.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/asset"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/save"
	"github.com/matjam/bunyip/timer"
	"github.com/matjam/bunyip/ui"
)

const count = 12

type settings struct {
	Version int
	Runs    int
	Dir     string
}

type item struct {
	name   string
	handle *asset.Handle[image.Image]
	tex    *gfx.Texture
}

type game struct {
	seconds float64
	shot    string
	dir     string

	font     *gfx.Font
	ui       *ui.Context
	fs       *asset.FS
	loader   *asset.Loader
	watcher  *asset.Watcher
	items    []*item
	timers   timer.Scheduler
	random   *rng.Rand
	store    *save.Store
	settings settings
	status   string
	packed   string
	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	g.random = rng.New(uint64(time.Now().UnixNano()))
	// Settings persist between runs in the platform's data directory.
	if g.store, err = save.Open("bunyip-assets"); err != nil {
		return err
	}
	if g.settings, err = save.Load(g.store, "settings", settings{Version: 1}); err != nil {
		return err
	}
	g.settings.Runs++
	g.settings.Dir = g.dir
	if err := g.store.Write("settings", g.settings); err != nil {
		return err
	}
	if err := g.seed(); err != nil {
		return err
	}
	sources := []string{g.dir}
	if pack := filepath.Join(g.dir, "assets.pak"); exists(pack) {
		sources = append(sources, pack) // loose files win over the pack
	}
	if g.fs, err = asset.Open(sources...); err != nil {
		return err
	}
	g.loader = asset.NewLoader(g.fs, 0)
	g.watcher = asset.NewWatcher(g.fs, 250*time.Millisecond)
	for i := range count {
		name := fmt.Sprintf("images/shape%02d.png", i)
		g.items = append(g.items, &item{name: name, handle: asset.Load(g.loader, name, decode)})
		g.watcher.Add(name)
	}
	// Rewrite a random image now and then; the watcher reloads it.
	g.timers.Every(2, func() {
		i := g.random.Intn(count)
		g.writeImage(i, g.random)
		g.status = fmt.Sprintf("Rewrote %s on disk", g.items[i].name)
	})
	g.status = fmt.Sprintf("Run %d of this example; assets in %s", g.settings.Runs, g.dir)
	return nil
}

func decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// seed creates the asset directory with generated images when empty.
func (g *game) seed() error {
	if err := os.MkdirAll(filepath.Join(g.dir, "images"), 0o755); err != nil {
		return err
	}
	r := rng.New(5)
	for i := range count {
		if !exists(filepath.Join(g.dir, fmt.Sprintf("images/shape%02d.png", i))) {
			if err := g.writeImage(i, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *game) writeImage(i int, r *rng.Rand) error {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	bg := color.RGBA{uint8(r.Intn(200)), uint8(r.Intn(200)), uint8(r.Intn(200)), 255}
	fg := color.RGBA{255 - bg.R, 255 - bg.G, 255 - bg.B, 255}
	kind := r.Intn(3)
	for y := range 64 {
		for x := range 64 {
			dx, dy := x-32, y-32
			inside := false
			switch kind {
			case 0:
				inside = dx*dx+dy*dy < 22*22
			case 1:
				inside = abs(dx) < 20 && abs(dy) < 20
			default:
				inside = abs(dx)+abs(dy) < 24
			}
			c := bg
			if inside {
				c = fg
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(g.dir, fmt.Sprintf("images/shape%02d.png", i)), buf.Bytes(), 0o644)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.watcher.Close()
	g.loader.Close()
	g.fs.Close()
	for _, it := range g.items {
		if it.tex != nil {
			it.tex.Destroy()
		}
	}
	g.font.Destroy()
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	g.timers.Update(ctx.Delta)
	// Finished loads become textures here, on the main thread.
	for _, it := range g.items {
		if it.tex != nil || it.handle == nil {
			continue
		}
		if img, err, ok := it.handle.Value(); ok {
			it.handle = nil
			if err != nil {
				g.status = err.Error()
				continue
			}
			it.tex, err = ctx.Gfx.NewTexture(img, gfx.TextureOptions{})
			if err != nil {
				return err
			}
		}
	}
	for _, name := range g.watcher.Changed() {
		for _, it := range g.items {
			if it.name == name && it.handle == nil {
				if it.tex != nil {
					it.tex.Destroy()
					it.tex = nil
				}
				it.handle = asset.Load(g.loader, name, decode)
				g.status = "Reloaded " + name
			}
		}
	}
	if ctx.Input.KeyPressed(input.KeyP) {
		out := filepath.Join(g.dir, "assets.pak")
		if err := asset.Pack(filepath.Join(g.dir), out); err != nil {
			g.status = err.Error()
		} else {
			info, _ := os.Stat(out)
			g.packed = fmt.Sprintf("Packed %d bytes into %s", info.Size(), out)
		}
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	for i, it := range g.items {
		x := 40 + float32(i%6)*130
		y := 140 + float32(i/6)*130
		if it.tex != nil {
			gr.Draw(it.tex, gfx.Sprite{Pos: lin.V2(x, y), Size: lin.V2(96, 96), UV1: lin.V2(1, 1), Color: gfx.White})
		} else {
			gr.FillRect(x, y, 96, 96, gfx.RGB(40, 40, 50))
		}
		gr.DrawText(g.font, filepath.Base(it.name), x, y+100, gfx.RGB(170, 170, 190))
	}
	u := g.ui
	u.Begin(ctx.Input)
	u.Panel("Assets", ui.Rect{X: 16, Y: 16, W: 800, H: 100})
	done, total := g.loader.Progress()
	u.Progress(fmt.Sprintf("Loaded %d of %d", done, total), float32(done)/float32(max(total, 1)))
	u.Label(g.status)
	if g.packed != "" {
		u.Label(g.packed)
	} else {
		u.Label("P packs the directory into assets.pak, read on the next run behind the loose files.")
	}
	u.EndPanel()
	u.End()
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	dir := flag.String("dir", filepath.Join(os.TempDir(), "bunyip-assets"), "asset directory (created and seeded when empty)")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip assets", Width: 840, Height: 440, Validation: true},
		&game{seconds: *seconds, shot: *shot, dir: *dir})
	if err != nil {
		fmt.Fprintln(os.Stderr, "assets:", err)
		os.Exit(1)
	}
}
