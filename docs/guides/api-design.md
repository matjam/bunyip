---
title: API design
group: Start
order: 4
summary: ownership, cleanup, scoped work, useful defaults and typed operations
---

Bunyip keeps the game loop and device lifetime in the engine. Game code
describes what to update and draw, owns its application state, and chooses
when resources can be released early. Closures handle short scopes;
concrete values and typed methods handle the state that survives them.

For the language choices behind this approach, see [Why Go?](why-go.html).

Input bindings retain physical `input.Key` values. For labels and logical
lookup, request a `ctx.KeyboardLayout()` snapshot when refreshing binding UI:
`layout.Label(key)` supplies a native label with a physical-name fallback, and
`layout.KeysFor(input.TextSymbol("a"))` returns every matching physical key.
These snapshots exclude modifiers, locks and pending text composition;
`Chars` and `Composition` remain the text-entry APIs. Gamepad snapshots include
`Info.HasButton` and `Info.HasAxis` for the controls mapped by the backend.
See [Input](input.html) for platform limits and examples.

## Let the engine manage the loop

`Run` creates the window, graphics, input and audio services and makes
them available through `Context`. Small programs can capture their state
in callbacks. Every `GameFuncs` callback is optional:

```go
var elapsed float64
err := engine.Run(engine.Config{Title: "Clock"}, engine.GameFuncs{
	UpdateFunc: func(ctx *engine.Context) error {
		elapsed += ctx.Delta
		return nil
	},
	DrawFunc: func(ctx *engine.Context) error {
		ctx.Gfx.DebugText(20, 20, fmt.Sprintf("%.1f seconds", elapsed))
		return nil
	},
})
if err != nil {
	panic(err)
}
```

A game type with methods suits larger programs whose state needs names
and structure. Both forms use the same loop. `Config.Console` also asks
the engine to draw that console after the game and debug overlay.

## Give each resource one owner

Graphics owns the GPU resources it creates: textures, fonts, meshes,
models, shaders, render textures and environments. It releases
them when its context closes, including after initialization or drawing
fails. They need no `Context.Cleanup` registration. Call `Destroy` when
unloading a level or otherwise releasing a resource early, then stop
using that resource. Graphics handles retirement of work still in flight.

Files, asset loaders and streaming music have their own lifetimes. When
one should live as long as the context, register its cleanup immediately
after acquiring it. Ordinary Go values, such as component structs and
decoded images, need no `Close` or `Destroy`.

This setup gives the loader access to the pack until its workers finish:

```go
type game struct {
	hero   *gfx.Texture
	menu   *ui.Context
	loader *asset.Loader
}

func (g *game) Init(ctx *engine.Context) error {
	files, err := asset.Open("assets.pak")
	if err != nil {
		return err
	}
	ctx.Cleanup(files.Close)

	g.loader = asset.NewLoader(files, 0)
	ctx.Cleanup(g.loader.Close)

	g.hero, err = asset.Texture(ctx.Gfx, files, "hero.png", gfx.TextureOptions{})
	if err != nil {
		return err
	}
	g.menu, err = ctx.NewUI(ui.Theme{})
	return err
}
```

On normal exit, cleanup closes the loader first and the pack second.
`Loader.Close` finishes accepted jobs and joins its workers before
returning. If texture loading or UI creation fails, those same callbacks
still run; Graphics releases any GPU resources already created.

`defer files.Close()` inside `Init` would close the pack when `Init`
returns. `ctx.Cleanup(files.Close)` keeps it alive for the context.

Callbacks run in reverse registration order after the game's optional
`Shutdown`, while graphics and audio are still available. `Shutdown`
runs only after successful `Init` or `Recover`; cleanup also runs when
either setup method fails. Remaining callbacks still run if one panics.
For caller-owned music, `ctx.Cleanup(music.Close)` follows the same pattern.

Each additional window has its own Graphics and Input. Create its textures,
fonts and other GPU resources through that window's context; a texture from
one output cannot be drawn by another. Ownership checks reject foreign
resources before using their GPU handles. The windows share one audio mixer.
Closing a parent closes its children, while closing a child leaves its parent
running. Native embedding borrows the host window or view and creates an
owned rendering child; keep the host alive until Bunyip finishes teardown.
See [Windows](window.html) for the lifecycle and platform restrictions.

Audio output belongs to the mixer, but recording is an explicit acquisition.
Register a recorder's `Close` with `ctx.Cleanup` when it should last for the
context, or stop it earlier. `Recorder.Stop` returns an independent PCM
snapshot. Writer-based recording borrows the writer; `RecordWAVFile` owns
and closes the file it creates. The default recording limit is 30 seconds,
so forgetting to stop a memory recording cannot grow it indefinitely.
See [Audio](audio.html) for capture, completion and error handling.

A reusable `TextLayout` owns CPU layout data and borrows its font atlases.
It needs no cleanup, and its measurement queries remain usable after font
destruction. Drawing it still requires live fonts from the current Graphics.
This distinction lets durable game data outlive a rendering resource without
pretending that the GPU resource is still available.

## Put temporary work inside a closure

A drawing scope makes the affected calls visible and restores the prior
state when the callback finishes, including when it panics:

```go
ctx.Gfx.Layered(2, func() {
	ctx.Gfx.WithCamera2D(camera, func() {
		ctx.Gfx.DrawTexture(g.hero, 100, 80)
	})
})
ctx.Gfx.DebugText(20, 20, "Camera and layer restored")
```

`Blended`, `CustomBlended`, `Transformed`, `Shaded`, `ColorMatrixed` and
`Clip` follow the same pattern. `WithView` scopes a destination viewport
and virtual size; `Masked` scopes stencil coverage. `DrawTo` scopes the
render target. These scopes restore
state; they do not undo drawing already queued.

UI uses closures to keep a frame and its layout together. Create the
interface once in `Init`, as above, and reuse it in `Draw`:

```go
g.menu.Begin(ctx.Input, func() {
	g.menu.Panel("Options", lin.R(20, 20, 260, 160), func() {
		g.menu.Slider("Volume", &volume, 0, 1) // volume is a float32
		if g.menu.Button("Quit") {
			ctx.Quit()
		}
	})
})
```

The zero theme passed to `Context.NewUI` selects a dark theme and the
engine's shared font, with clipboard and input-method support connected.
A custom theme keeps its settings; a missing font receives the shared
font. The engine owns that font, so do not destroy it separately.

For ECS iteration, a closure gives structural changes a clear application
point. Component fields can change during the walk; entity removal waits
until the whole walk finishes:

```go
type Lifetime struct{ Left float64 }

world.Defer(func(cmd *ecs.Commands) {
	world.Each(func(e ecs.Entity, life *Lifetime) {
		life.Left -= ctx.Delta
		if life.Left <= 0 {
			cmd.Despawn(e)
		}
	})
})
```

`World.Defer` applies its commands in order after a normal return and
discards pending commands on panic. It is not a transaction: direct
component writes and completed nested scopes are not rolled back.
Keep the command buffer inside its callback.

## Defaults should preserve intent

Omitting window dimensions selects 1280 by 720; each nonpositive dimension
defaults independently. `TextureOptions{}` selects nearest filtering for
pixel art, while `Linear: true` requests smoothing. Defaults are specific
to each API, rather than a rule that every numeric zero means "unset".

Graphics starts with `gfx.DefaultPost()`. To change selected settings,
edit the current values:

```go
ctx.Gfx.ConfigurePost(func(p *gfx.PostSettings) {
	p.Bloom = 0      // disable bloom
	p.Saturation = 0 // monochrome
})
```

Untouched settings retain their values. `SetPost` replaces the entire
configuration, preserving explicit zeros: zero saturation removes colour
and zero contrast flattens contrast. Use `DefaultPost` as the starting
value when constructing a complete replacement. Post-processing settings
are global to the submitted frame, including render textures;
`ConfigurePost` does not create a temporary drawing scope.

## Use typed operations and small interfaces

Operations live on the object they use. `world.Get[Lifetime](entity)`
returns a `*Lifetime` and a presence flag; `world.Each` infers the component
type from its callback. Similarly, `loader.Load` infers its result type
from the decoder. Callers work with concrete results without type assertions.

An interface is useful where implementations really vary. Asset helpers
accept the standard `fs.FS`, so this function works with embedded assets,
`os.DirFS`, an `asset.FS` overlay or a `fstest.MapFS` in a test:

```go
//go:embed assets
var embedded embed.FS

func loadHero(g *gfx.Graphics, files fs.FS) (*gfx.Texture, error) {
	return asset.Texture(g, files, "assets/hero.png", gfx.TextureOptions{})
}

// During Init:
hero, err := loadHero(ctx.Gfx, embedded)
```

`fs.FS` does not imply ownership or provide `Close`. Embedded filesystems
and `os.DirFS` need no shutdown action. An `asset.FS` with open packs does.
If you open an individual `fs.File`, close that handle before its backing
filesystem; helpers such as `fs.ReadFile` manage their own temporary handles.

Shader parameters follow the same principle: `Shader.SetUniforms` copies an
ordinary struct and inserts the GPU's required padding. Use exported fields
and the documented scalar, vector and matrix types; unsupported fields return
an error and preserve the previous parameters. Game code describes the
values, while the engine handles their byte layout. See [Shaders](shaders.html)
for matching Go and shader declarations.

## Keep device work on the game goroutine

Use graphics, UI and context operations from the game's callbacks.
Background loaders may read and decode CPU data; create GPU resources
from ready results on the game goroutine. `Context.Wake` is explicitly
safe to call from another goroutine to request work in an idle loop.
It does not make shared game state safe: synchronize that state yourself.

Device recovery creates a fresh context. A game implementing `Recover`
must rebuild its GPU resources and UI, restore mixer and console setup,
and register cleanup for new acquisitions. Cleanup for the old context
runs before rebuilding. Keep durable game state separate from device
resources so it can survive this boundary. `GameFuncs` does not implement
recovery; use a game type with an explicit `Recover` method when needed.
