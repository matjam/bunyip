package asset

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/matjam/bunyip/gfx"
)

// Reloader keeps what a game loaded in step with the files it came from,
// so a texture repainted or a shader recompiled while the game runs
// appears the moment it is saved. To use one, load through it instead of
// the package's loaders and call Reload once a frame:
//
//	rel := asset.NewReloader(ctx.Gfx, fs, 0)
//	tex, err := rel.Texture("sprites/hero.png", gfx.TextureOptions{})
//	// ... hand tex to a material or draw it as a sprite ...
//
//	func (g *game) Update(ctx *bunyip.Context) error {
//		names, err := g.rel.Reload()
//		if err != nil {
//			ctx.Log.Warn("reload failed", "err", err)
//		}
//		for _, n := range names {
//			ctx.Log.Info("reloaded", "asset", n)
//		}
//		return nil
//	}
//
// Everything it loads keeps the pointer it handed back. A texture's
// image is swapped in place, so every material, sprite and shader slot
// that names it draws the new pixels without being told, even at a new
// size; a shader's pipelines are rebuilt, so every draw through it runs
// the new program. Nothing a game holds goes stale, so a reload needs no
// bookkeeping of its own.
//
// Only loose files change: a name that resolves into a pack file or an
// embedded file system is loaded once and never watched, so a shipped
// game pays for the polling goroutine and nothing else. Close stops it.
//
// Models and environments are not reloaded. Swapping a glTF file's
// contents would give back different meshes, a different skeleton and
// different animation clips, which every AnimPlayer, mesh pointer and
// node index the game holds refers to. A gfx.Environment is built by
// prefiltering a panorama into a cube map, and a reflection probe bakes
// and owns one of its own, so replacing the image behind one would mean
// rebuilding every level of that cube while the game runs. A game that
// wants either loads it again and rebinds what pointed at the old one.
type Reloader struct {
	g       *gfx.Graphics
	fs      *FS
	watcher *Watcher

	mu      sync.Mutex
	targets map[string][]func(data []byte) error
}

// NewReloader watches fs for changes and reloads through g. interval is
// how often loose files are checked; zero means half a second. Close it
// when the game ends.
func NewReloader(g *gfx.Graphics, fs *FS, interval time.Duration) *Reloader {
	return &Reloader{g: g, fs: fs, watcher: NewWatcher(fs, interval), targets: map[string][]func([]byte) error{}}
}

// Texture loads an image or a KTX2 texture and reloads it in place when
// the file changes. It is asset.Texture with the watching added, so the
// options and the errors are the same.
func (r *Reloader) Texture(name string, opts gfx.TextureOptions) (*gfx.Texture, error) {
	tex, err := Texture(r.g, r.fs, name, opts)
	if err != nil {
		return nil, err
	}
	r.Watch(name, func(data []byte) error { return replaceTexture(tex, name, data) })
	return tex, nil
}

// Shader compiles a sprite shader from a .spv file written by
// bunyip-shader and rebuilds it when the file changes. Its images and
// uniforms are kept across a reload.
func (r *Reloader) Shader(name string) (*gfx.Shader, error) {
	return r.shader(name, false)
}

// MeshShader is Shader for a shader that draws meshes rather than
// sprites.
func (r *Reloader) MeshShader(name string) (*gfx.Shader, error) {
	return r.shader(name, true)
}

func (r *Reloader) shader(name string, mesh bool) (*gfx.Shader, error) {
	data, err := r.fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	build := r.g.NewShader
	if mesh {
		build = r.g.NewMeshShader
	}
	sh, err := build(data)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	r.Watch(name, sh.Reload)
	return sh, nil
}

// Watch calls reload with the file's new contents whenever the named
// file changes, for an asset this package has no loader for: a level, a
// table of tuning values, a palette. The call happens inside Reload, on
// the goroutine that calls it, so it may touch the game and the GPU.
// Several targets may watch one name and each is called in turn.
func (r *Reloader) Watch(name string, reload func(data []byte) error) {
	if r.fs.Path(name) == "" {
		return // packed or embedded: the bytes cannot change
	}
	r.mu.Lock()
	r.targets[name] = append(r.targets[name], reload)
	r.mu.Unlock()
	r.watcher.Add(name)
}

// Reload swaps in every watched file that changed since the last call
// and returns the names it reloaded. Call it once a frame from Update.
// A file that fails to read or decode keeps the asset the game already
// has and its error is returned, while the other names still reload, so
// one bad save does not stop the rest; the file is tried again the next
// time it is written.
func (r *Reloader) Reload() ([]string, error) {
	changed := r.watcher.Changed()
	if len(changed) == 0 {
		return nil, nil
	}
	var done []string
	var errs []error
	for _, name := range changed {
		r.mu.Lock()
		targets := r.targets[name]
		r.mu.Unlock()
		if len(targets) == 0 {
			continue
		}
		data, err := r.fs.Read(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ok := true
		for _, reload := range targets {
			if err := reload(data); err != nil {
				errs = append(errs, fmt.Errorf("asset %s: %w", name, err))
				ok = false
			}
		}
		if ok {
			done = append(done, name)
		}
	}
	return done, errors.Join(errs...)
}

// Close stops watching. The assets it loaded are the game's to destroy.
func (r *Reloader) Close() { r.watcher.Close() }
