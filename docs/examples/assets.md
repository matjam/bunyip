---
title: Assets
example: assets
summary: a directory of images loaded on worker goroutines behind a progress bar, reloaded when they change on disk, packed into a pack file, with settings that persist between runs
---

This program is the [asset](../pkg/asset.html) package end to end. It
seeds a directory with twelve generated PNGs, opens it as an asset
filesystem, decodes the images on worker goroutines behind a progress
bar, creates the textures on the main thread as each decode finishes,
watches the files and reloads any that change, and packs the whole
directory into a pack file on demand. It also rewrites one image every
two seconds so the reload path can be seen working without anyone
editing a file.

Two smaller packages appear alongside it. [save](../pkg/save.html) keeps
a settings file that counts how many times the example has been run, and
[timer](../pkg/timer.html) schedules the rewrites. Both are covered in
[the game services guide](../guides/services.html).

The division of labour is the point. Decoding is slow and happens on
worker goroutines; creating a GPU texture must happen on the main
goroutine, so it happens in `Update` as each handle reports a value. A
game never creates or destroys a GPU resource from another goroutine.

Run it:

```bash
go run ./examples/assets -seconds 3 -shot out.png
```

The flags are `-seconds N`, `-shot file.png`, `-dir path`, which is
the asset directory; it defaults to a directory under the system
temporary directory and is created and seeded when empty, and `-seed N`,
which decides which image the timer rewrites, so a run can be repeated.
P packs the directory and Escape quits.

## Package and state

`settings` is the shape of the saved file, with a version number so a
later format can be told apart. `item` is one image: the name it is
loaded under, the handle while the load is in flight, and the texture
once it has been created. Exactly one of the two is set at a time, which
is what the reload logic tests.

```go
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
	seconds  float64
	shot     string
	dir      string
	randSeed uint64

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
```

## Init: settings, sources, loader and watcher

`save.Open` returns a store in the operating system's own data directory
for the named application. `save.Load` reads a value or returns the
default it is given when there is no file yet, which is how a first run
starts from a known state rather than from an error.

`asset.Open` takes any number of sources and searches them in order, so
listing the directory before the pack file means a loose file wins over
the packed copy of the same name. That is the shape a game ships with:
the pack for the release, loose files for whatever is being edited.

`asset.NewLoader(g.fs, 0)` starts the worker pool; zero means one worker
per CPU. `asset.Load` queues one file with the
function that decodes its bytes and returns an
`*asset.Handle[image.Image]`, a typed handle that reports when the value
is ready. `asset.NewWatcher` polls the filesystem at the interval given,
and `Add` names a file to watch.

`g.timers.Every(2, ...)` schedules a repeating callback on the game's own
scheduler, which is stepped from `Update`.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	// A seed from the flag rather than the clock, so a run can be
	// repeated: which image the timer rewrites is then the same every
	// time, which is what a test comparing frames needs.
	g.random = rng.New(g.randSeed)
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
```

`decode` is the loader function: bytes in, value out, on a worker
goroutine. It must not touch the graphics device, which is why it returns
an `image.Image` rather than a texture.

```go
func decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
```

## Seeding the directory

The rest of the setup is this program making itself something to load.
`seed` writes any of the twelve images that are missing, and `writeImage`
draws a circle, a square or a diamond in two colours and encodes it as a
PNG. A real game ships its assets instead.

```go
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
```

```go
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
```

```go
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
```

## Shutdown: closing in order

The watcher and the loader are closed before the filesystem they read
from, and every texture that was created is destroyed. `Loader.Close`
stops submissions but does not wait for queued work. This example omits
`Loader.Wait`; add it after `Close` and before closing the filesystem if
workers may still be reading at shutdown.

```go
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
```

## Update: finishing loads and reloads

`handle.Value()` returns the value, the error and whether the load has
finished. It never blocks, so calling it once per update is how a game
picks up finished work. A finished handle is dropped and the texture
created here, on the main goroutine, from the image the worker decoded.

`watcher.Changed()` returns the names whose files changed since the last
call. A changed item's texture is destroyed and a fresh load queued for
it, which puts the item back into the loading state the first branch
handles. Guarding on `it.handle == nil` keeps a second reload from
starting while one is still in flight.

`asset.Pack` writes a directory into a single pack file. The next run
finds it beside the loose files and reads both.

```go
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
```

## Draw: the grid and the panel

Each item is a sprite when its texture exists and a flat rectangle while
it is still loading. `gfx.Sprite` positions are view units from the top
left, `UV1: lin.V2(1, 1)` uses the whole texture, and `gfx.White` is the
untinted colour; leaving `Color` zero would mean the same thing, since a
zero colour where a tint is expected is white.

`u.Progress` draws a bar from a fraction, which comes straight from
`loader.Progress()`: the number of loads finished and the number queued.
The `max(total, 1)` avoids dividing by zero before anything is queued.

```go
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
	u.Begin(ctx.Input, func() {
		u.Panel("Assets", ui.Rect{X: 16, Y: 16, W: 800, H: 110}, func() {
			done, total := g.loader.Progress()
			u.Progress(fmt.Sprintf("Loaded %d of %d", done, total), float32(done)/float32(max(total, 1)))
			u.Label(g.status)
			if g.packed != "" {
				u.Label(g.packed)
			} else {
				u.Label("P packs the directory into assets.pak, read on the next run behind the loose files.")
			}
		})
	})
	return nil
}
```

## main

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	dir := flag.String("dir", filepath.Join(os.TempDir(), "bunyip-assets"), "asset directory (created and seeded when empty)")
	seed := flag.Uint64("seed", 5, "random seed, so a run can be repeated")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip assets", Width: 840, Height: 440, Validation: true},
		&game{seconds: *seconds, shot: *shot, dir: *dir, randSeed: *seed})
	if err != nil {
		fmt.Fprintln(os.Stderr, "assets:", err)
		os.Exit(1)
	}
}
```

## What to try

- Run it twice and watch the run counter in the panel, then delete the
  settings file the path in `Init` names.
- Press P and inspect the pack file. Loose files take precedence when
  `asset.Open` finds both sources; creating a pack alone does not switch
  these images away from the asset directory.
- Edit one of the PNGs in the asset directory with an image editor and
  watch the watcher in `Update` reload it within a quarter second.
- Change the watcher's interval in `Init` and see the reload latency
  follow it.
- Make `decode` return an error for one name and watch the status line
  report it without stopping the other loads.
