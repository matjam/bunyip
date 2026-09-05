# Bunyip for language models

This file is for a model working in this repository or writing a game
with it. The first half is about changing the engine; the second half is
about using it. Everything here is also in the guides and the package
documentation, which are the authority when they disagree with this
file.

The documentation site is https://matjam.github.io/bunyip/. Every page
is also served as Markdown: the guides at `guides/<name>.md`, the API
reference at `pkg/<package>.md`, an index at `llms.txt` and the whole
set in one file at `llms-full.txt`. Read those rather than the HTML.

## What Bunyip is

A game engine in Go for real-time and turn-based games that put 2D
sprites and 3D models on one screen. Vulkan through a generated binding
with no cgo (`CGO_ENABLED=0` is a hard rule), native window, input and
audio layers per platform written against the OS APIs (no SDL, no GLFW),
an archetype entity component system, rigid-body physics, an
immediate-mode interface, an in-game debug console, an audio mixer with
a tracker player, and the
services a game needs (assets, saves, translation, networking). macOS is
the tested platform. The Linux window layer has run on real hardware,
both Wayland and X11, and so have Linux audio output and capture; Linux
gamepads, macOS capture and the whole Windows layer cross-compile but
have not. Linux picks Wayland or X11 at startup;
`BUNYIP_X11=1` forces
X11 and `platform.Backend()` says which was chosen.

# Part one: working on the engine

## Map of the repository

| Path | What lives there |
|---|---|
| `bunyip.go`, `run.go`, `headless.go`, `debug.go`, `flycam.go`, `url.go` | The root package: `Run`, `Config`, `Game`, `Context`, the loop (fixed step or turn-based), the fixed view and letterboxing, the F3 overlay, headless mode, the fly camera. |
| `gfx/` | Everything drawn. 2D: textures, sprites, sheets, tilemaps, atlases (`atlas.go` for the JSON forms, `aseprite.go` for Aseprite's binary one), paths, gradients, text (HarfBuzz shaping, atlases, SDF, colour glyphs from COLR, SVG and bitmap strikes, hyphenation, rich text), colour matrices, lit sprites with polar shadow maps built on the CPU (`shadow2d.go`). 3D: meshes, materials (including iridescence, anisotropy, specular tint and fur shells), models, skinning and animation players, lights, shadows, sky and environments (`hdr.go` for Radiance, `exr.go` for OpenEXR), fog, frustum and occlusion culling (`cull.go`, `occlude.go`), static batches with a bounding volume hierarchy (`batch.go`), LOD, impostors baked from a model (`impostor.go`), chunked terrain with per-chunk levels of detail and a splat map (`terrain.go`), billboards, decals, post-processing, render textures, picking, debug lines. Global illumination: reflection probes baked from the scene (`probe.go`), an irradiance grid (`lightprobe.go`) and screen-space reflections (`ssr.go`). `gfx/shaders/` holds the GLSL sources, the preludes game shaders are composed with, and the compiled SPIR-V. `gfx/ktx2/` reads and writes KTX2 files and encodes and decodes the BC block formats they carry. |
| `ui/` | Immediate-mode widgets, containers, navigation, drag and drop, themes, skins, the accessibility tree. |
| `console/` | The in-game debug console drawn with `ui`: the drop-down command line, commands, variables, key bindings, the `slog` tee, and the debug panels (engine, graphics, entities, physics, audio, input, services). `Config.Console` builds one; the game draws it last. |
| `ecs/` | The entity component system: archetype tables, queries, systems, resources, events, hierarchy, saves, prefabs, cloning, the scene document format (`scene.go`). |
| `phys/` | 2D and 3D rigid bodies on the ECS: shapes, GJK/EPA, contacts, joints, ragdolls, CCD, sleeping, character controllers, queries, the signed distance to a placed shape. |
| `phys/soft/` | Cloth, volumetric soft bodies and 2D fluids as particles under extended position-based dynamics, stepped by `soft.System` on the same world and the same static colliders. |
| `anim/` | Keyframe curves and clips for components, flipbooks, skeleton playback, IK, blend spaces. |
| `orbit/`, `orbit/sol/` | Celestial mechanics, unit-agnostic; real solar-system constants only in `sol`. |
| `audio/`, `audio/tracker/` | The mixer (voices, buses, reverb, occlusion, Doppler, binaural rendering, streams, microphone capture) and the MOD/S3M/XM/IT player. |
| `input/` | Key codes by physical position, the per-update `State`, gamepads, action maps. |
| `particle/`, `tiled/`, `gltf/`, `lin/`, `grid/`, `grid/autotile/`, `rng/`, `save/`, `locale/`, `timer/`, `tween/`, `asset/`, `network/` | Self-contained packages; each has a package comment that explains its model and conventions. `grid/autotile` picks tile frames from a terrain grid (blob, edge, dual-grid and Wang rules) on a square, hexagonal or isometric `Layout`; `tiled` parses Tiled terrain sets into it and `Map.Layout` names the matching layout. |
| `internal/vk/` | The Vulkan binding generated by `cmd/vkgen` from `third_party/vulkan/vk.xml`, with struct layout tests. Do not edit by hand. |
| `internal/render/` | The Vulkan backend: device, swapchain, frames in flight, images, buffers, pipelines, descriptor sets, targets, readback. |
| `internal/hook/` | The boundary between the engine loop and `gfx`, `input` and `audio`. Each of those packages keeps its plumbing (frame begin and end, resize, the event feed, the audio pull) on unexported methods, wraps the public value in an unexported driver implementing a `hook` interface, and registers a constructor from `init`. `run.go` builds all three through `hook` and hands the game the values `Game()` returns. |
| `internal/platform/`, `internal/audioout/` | Per-OS window, events, surface, audio output and microphone input. `*_darwin.go` is the reference; Windows and Linux mirror it. Linux has two window backends behind one `App` and `Window`: Wayland in `wayland_linux.go`, its hand-built protocol tables in `wayland_proto_linux.go` and its listener callbacks in `wayland_listen_linux.go`, and X11 in `x11_linux.go`. `app_linux.go` holds the shared structs, chooses the backend in `NewApp` and forwards every method to the live one. |
| `cmd/` | `bunyip-shader` (composes and compiles game shaders), `bunyip-tex` (compresses images to KTX2 with their mip chains), `bunyip-docs` (the documentation site), `bunyip-pack`, `bunyip-bundle`, `bunyip-play`, `bunyip-info`, `vkgen`. |
| `examples/` | One directory per example; every one takes `-seconds N` and `-shot file.png`. `examples_test.go` runs them all headless and compares each frame against `examples/testdata/<name>.png` on the GPU named in `examples/testdata/gpu.txt` (elsewhere it only checks the frame is not blank), and each has a walkthrough in `docs/examples/`. |
| `docs/guides/` | The guides, Markdown with front matter (`title`, `group`, `order`, `summary`); images sit beside them. The groups are Start (introduction, getting started, Tetris), Engine (the window, input, entities and systems, game services, the debug console), Graphics (2D graphics, 3D graphics, shaders, animation, the interface), Simulation (physics, orbits) and Audio. `docs/design/` holds design notes and `gaps.md`, the list of what is missing. |
| `docs/examples/` | One walkthrough per example, `<name>.md`, with front matter (`title`, `example`, `summary`) and a screenshot `<name>.png` beside it. The body quotes the whole program in source order, verbatim, with a section explaining each part. `cmd/bunyip-docs` renders these as the Example programs group; the examples are not rendered as packages. |

## Core concepts

**The loop.** `Run` owns the window, renderer, mixer and clock. In
real-time mode `Update` runs at `Config.FixedStep` and `Draw` once per
frame; `Context.Delta` is the step and `Context.Alpha` the interpolation
fraction. Turn-based mode blocks in the OS until events arrive. Input
edges are per update and are latched for the whole frame during `Draw`,
so an interface built in `Draw` sees every press.

**Drawing is queued.** `Graphics` records into a `drawQueue` per output
(the screen, each render texture). 2D drawing is one vertex stream
(`vertex2D`: position, UV, premultiplied colour) sorted by layer and
merged into draws by texture, shader, uniforms, blend, clip and
projection. 3D draws go into `meshDraw` records that `prepareDraws`
resolves, culls, sorts and uploads as an instance stream, in three
groups: opaque, order-independent translucent, and sorted translucent.
`renderScene` runs the shadow atlas pass, the HDR pass (sky, opaque, the
order-independent transparency pass and its resolve, sorted blended,
debug lines), decals and the velocity pass, then the post chain and the
composite. Colours are linear and non-premultiplied in the API,
premultiplied in the 2D stream.

**The post chain.** `postChain` in `gfx/posteffects.go` runs the effects
that read the scene and its depth, each through `chainPass`: a
fullscreen pass from `t.hdr` into `t.pong`, copied back over `t.hdr`.
Every pass therefore has one input set to keep and the composite always
finds its scene in `t.hdr`. The optional images (velocity, history,
pong, rays, the second LDR image) are made by the `need*` methods the
first time a setting asks for them and freed with the target set, so a
game that turns none of them on pays nothing. The composite reads four
samplers (scene, bloom, occlusion, rays) plus the LUT, with a set per
combination of bloom and rays in `sceneTargets.finals`.

**Multisampling.** `PostSettings.Samples` (1, 2, 4 or 8, clamped by
`Device.SampleCount`) multisamples the HDR scene pass. A `render.Target`
then holds `MSColor` and `MSDepth` beside `Color` and `Depth`, renders
into the multisampled pair and resolves into the single-sample pair as
the pass ends: colour by averaging, depth by taking sample zero, both
declared on the attachment rather than as a separate command. Everything
downstream (ambient occlusion, decals, reflections, the transmission
snapshot, `RenderTexture.ReadDepth`) reads the single-sample images and
needs no cases of its own. The shadow atlas and every post pass stay
single-sample, and so does the order-independent transparency pass: its
accumulation and revealage images are its own, and `PassDesc.Depth`
lends it the resolved scene depth to test against, so only its edges are
unsmoothed. A pipeline must match its pass's attachments, so
`PipelineDesc.Samples` carries the count, `pipeKey.out` carries an
`outKey` (colour format, whether there is depth, sample count), and the
fixed pipelines are built per output through `pipeCache` in
`gfx/pipes.go`. The window's own format at one sample is the zero
`outKey`, so a plain render texture shares the screen's pipelines.
Changing the sample count rebuilds the scene targets at the start of the
next frame's rendering, in `Graphics.end`, where no pass is open.

**Instanced particles** (`gfx/particles.go`) are their own path, not the
sprite stream. `ParticleQuad` is exactly the GPU instance layout, so a
slice of them uploads as a memcpy and the fragment program does the
premultiply; the quad's six vertices come from `gl_VertexIndex`, so
there is no vertex buffer. `DrawParticles` records a batch into the
queue and `flush2D` interleaves the batches with the 2D stream by layer
and then by call order, which is why `draw2D` carries the layer and the
submission offset of its first item and why `item2D` carries `breaks`:
two sprite runs with a batch between them must not merge into one draw.
`DrawParticles3D` records into `parts.scene`, which `renderScene` draws
after decals in a `NoDepth` pass over `t.hdr`, sampling `t.depthSet` and
doing the depth test in the fragment program, which is what gives the
soft fade. Its push block is its own 128 bytes carrying the camera
basis, so the shared `Frame` block is untouched. That pass writes the
scene's colour attachment, so its pipeline is built per sample count
through `pipeCache` like the sky and the decals.

**Descriptor sets for meshes.** Set 0 is the material: seventeen
`SAMPLED_IMAGE` bindings (five material textures, four shader images,
the environment cube, the thickness map, the scene copy for
transmission, the transmission map, then the iridescence, anisotropy,
specular and fur maps) at bindings 0 to 16, then one array
of four `SAMPLER` bindings at binding 17, immutable in the layout:
linear repeat, linear clamp, nearest repeat, nearest clamp, in that
order (`samplerIndex` in `gfx/mesh_draw.go`). A texture's own filtering
and edge handling pick its sampler, and `materialSet` packs one index
per texture slot, two bits each, into the instance stream's `atten.w`,
for the first eleven slots only (`packedSamplerSlots`); the four maps
after them are always read linear and repeating, because a float's
mantissa has no room for more index bits.
The shader reads the index back with `texSampler(slot)` and the GLSL preludes
`#define` the old names (`albedoTex`, `image0`) as
`sampler2D(image, samplers[...])` pairs, so game shaders are unchanged.
Every instance of a draw shares set 0, so the index is the same across
the draw, which is what `shaderSampledImageArrayDynamicIndexing` needs;
`Device.ArrayIndexing` reports it and `initMeshPass` refuses a device
without it. A draw inside a reflection probe's volume binds that probe's
cube map at binding 9 instead of the light's environment, so probes cost
no image: `materialSet` is keyed on the environment already. Set 1 is the
per-frame block: the `Frame` uniform block at binding 0, the light probe
grid's harmonics as a growable storage buffer at binding 1, and the
frame's light records, cluster table and light index list as fixed-size
storage buffers at bindings 2 to 4 (`Device.NewFrameSets`, sized by
`frameStorage`; the fixed ones never grow, so a frame writes them
without a wait). The queue's own sets are bound and the mesh pass's
layout is what the pipelines are built against, so both are made the
same way. Set 2 is the shadow atlas, the one comparison sampler.
Set 3 is joint matrices. Set 4 is a game shader's uniform block. Set 5 is
one model's morph target deltas, bound by every mesh draw because the
vertex prelude names the buffer whether or not the draw reads it; six
bound sets is inside every desktop driver's and MoltenVK's limit, which
`initMeshPass` checks. Metal allows sixteen samplers a stage,
which the four plus the shadow atlas's stay well under, and 31 sampled
images a stage on Intel Macs under MoltenVK (128 on Apple silicon),
which is the budget the seventeen images and the atlas spend from: a new
material texture costs an image and no sampler. The shadow maps still
share one atlas image so the shadow pass costs one binding.

**The instance stream** (`meshInstance` in `gfx/mesh.go`) is fifteen
material `vec4`s at vertex attribute locations 5 to 16 and 19 to 21,
then the previous frame's three model rows, which only the velocity
programs declare, then the morph block at 22 to 24 and a `uvec2` of
packed morph target numbers at 25; 17 and 18 are a skinned mesh's joints
and weights (`meshVertexLayout`, `skinVertexLayout` and
`velocityVertexLayout`, which all read the one stride). Adding a field
means adding it at the end of the struct, at the next free location, and
to the declarations in `vert_common.glsl` and the varyings the postludes
in `gfx/shaders/shaders.go` write and `prelude_mesh.glsl` reads.

**The Frame block** (`frameUniforms` in `gfx/mesh_draw.go`) is declared
in seven GLSL files: `prelude_mesh.glsl`, `vert_common.glsl`,
`skyparam.frag`, `outline.vert`, `decal.vert`, `decal.frag`, `ssr.frag`.
Changing a field before the end means changing all of them and
regenerating every shader; appending at the end only needs the files that
read the new field. The global illumination fields (the probe volumes,
the grid's shape, the reflection settings) are the tail, read by
`prelude_mesh.glsl` and `ssr.frag`. The lights themselves are not in the
block: they live in set 1's storage buffers, and the block carries the
shadow projections and the cluster grid's mapping.

**Lights are clustered.** `gfx/cluster.go` cuts the view into 16 by 9
tiles and 24 exponential depth slices each frame, sorts the frame's
lights into the clusters they reach on the CPU, and writes the records,
the per-cluster table and the index list into set 1. The fragment
prelude finds its cluster from `gl_FragCoord` and the view depth and
loops over that cluster's lights. A cluster keeps 64 lights and a frame
1024 (`MaxLights`).

**Shadow maps share one atlas** (`shadowRegion` in `gfx/mesh_draw.go`,
mirrored by the fragment prelude): three cascades of 2048 in the square
top's quadrants, four spot maps of 1024 in the fourth, and the six cube
faces of up to four point lights, 512 each, in the strip below. The
atlas takes a depth-only format where the device has one. The shadow
pass draws one map per viewport, and the vertex program picks the
projection from the push constant: cascades, then spot maps, then cube
faces.

**Shaders are compiled offline.** `glslangValidator` must be on the
path. `go generate ./gfx/shaders/` rebuilds the engine's SPIR-V and
`go generate ./examples/shaders/` the examples'. Game shaders are
composed from a prelude, the game's `fragment` or `surface` function and
a postlude by `shaders.Compose`; `cmd/bunyip-shader` drives it. A new
`//go:embed` of a `.spv` needs a placeholder file to exist before
`bunyip-shader` can even build, because the tool imports `gfx/shaders`.

**The ECS** stores components in dense typed columns per archetype.
Entities are generational handles. Queries cache the tables they match
and walk rows last to first, so the visited entity may be despawned in
the callback; other structural changes go through `Commands`. Systems
are `func(w *World, dt float64)` run in registration order.

**Physics** bodies and colliders are ECS components on `gfx.Transform`
or `gfx.Transform2`. Names carry the dimension: `Body2`/`Body3`,
`Collider2`/`Collider3`, `Box2`/`Box3`; shapes that only exist in one
dimension have no suffix (`Circle`, `Sphere`, `Capsule`). Convex 3D
pairs use GJK and EPA; boxes keep a dedicated path. `phys/soft` adds
deformable components (`Cloth`, `SoftBody3`, `Fluid2`) stepped by
`soft.System`, solved by extended position-based dynamics; they read the
static and kinematic colliders through `phys.SignedDistance2` and
`SignedDistance3` and never write back to a rigid body.

**Zero values mean the default.** A zero `Roughness` is 0.6, a zero
`Color` where a tint is expected is white, a zero field of view is 60
degrees. A field whose zero must mean something of its own is named for
that zero (`NoMipmaps`, `NoDepthTest`, `Sky.Vacuum`). Keep this when
adding fields.

**Containers take closures** in the interface (`Panel(title, rect,
body)`, `Begin(input, body)`); there is no exported `End`.

## Commands

```
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test ./...                 # gfx and ui tests need a GPU; they skip without one
CGO_ENABLED=0 go test -short ./...          # skips the examples run
go test -fuzz=Fuzz ./gltf/                  # likewise audio, audio/tracker, tiled, gfx
go generate ./gfx/shaders/ ./examples/shaders/
go run ./examples/terrain -seconds 3 -shot /tmp/terrain.png
BUNYIP_HEADLESS=1 go run ./examples/tetris -seconds 2 -shot /tmp/t.png
CGO_ENABLED=0 go test ./examples -run TestExamplesRun -update          # rerecord the golden images
CGO_ENABLED=0 go test ./examples -run TestExamplesRun -update -docs    # and the walkthrough screenshots
go run ./cmd/bunyip-docs -out site
go run ./cmd/bunyip-tex -v -format bc7 art/hero.png      # also bc1, bc3, bc4, bc5
go run ./cmd/bunyip-tex -format bc5 -linear -outdir build art/normals/*.png
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...   # and linux
```

Headless tests: `newHeadless(t, w, h)` in `gfx` and `newContext(t)` in
`ui` give a `Graphics` on an offscreen surface; `renderMaterial` in
`gfx/material_test.go` renders one frame and returns the image.
`newHeadless` turns the validation layers on and fails the test if they
report an error, so a pipeline or descriptor mistake is a red test rather
than a line in the log. UI tests
must feed a mouse move and run one frame before a press, because hover
is one frame behind. A glyph first drawn in a frame appears in that
frame, because the atlas upload is recorded into the frame before the
render pass, so text tests draw one frame.

## Rules

- `CGO_ENABLED=0` always. No cgo, no SDL, no GLFW. Native APIs through
  purego.
- Never name the other Go 2D engine the API review compared against, in
  code, docs or commit messages.
- Do not edit `internal/vk`; regenerate it from `vk.xml`.
- Keep the Windows and Linux layers compiling and vetted
  (`GOOS=windows` and `GOOS=linux` builds) even where they are untested
  on hardware; a feature that cannot be implemented there gets a no-op
  with a comment saying so, not a stub that appears to work.
- Every example must run headless to a screenshot; `examples_test.go`
  checks it. It also compares the frame against a stored image in
  `examples/testdata/<name>.png`, downscaled to 320 wide: a mean
  absolute difference over the whole frame, which is loose enough to
  absorb a measured millisecond figure changing digits, and a much
  tighter comparison of both frames blurred to 40 wide, which is what
  catches a widget moving. A failure writes `got.png`, `want.png` and
  `diff.png` to a temporary directory it names. The runs are made
  reproducible by `BUNYIP_FIXED_CLOCK=1` (`Config.FixedClock`), which
  makes the clock count frames instead of reading the wall clock, so an
  example must seed its own random numbers from a flag rather than the
  clock. After a deliberate change to an example, rerecord with
  `CGO_ENABLED=0 go test ./examples -run TestExamplesRun -update`, and
  add `-docs` to rewrite the 640-wide walkthrough screenshots from the
  same run. An example whose picture cannot be the same twice goes in
  `noGolden` in the test with the reason.
- **Changing an example means updating its walkthrough.** Every directory
  under `examples/` with a `main.go` has `docs/examples/<name>.md`, and
  the test in `cmd/bunyip-docs` checks that every fenced `go` block is a
  verbatim run of contiguous lines in the file it quotes (`main.go`
  unless an HTML comment `<!-- file: other.go -->` sits on the line
  before the fence) and that every top-level declaration of the example
  appears in some excerpt. Copy the changed lines back out of the source
  rather than retyping them, and add a section when a declaration is new.
  A new example needs a walkthrough and a screenshot; generate the
  screenshot by running the example headless and scaling the frame to at
  most 640 pixels wide.
- **Documentation moves with the API.** A change to exported API,
  behaviour, a tool's flags or a build step is not done until the same
  change updates: the package comment and the doc comments of what
  changed; the guide that covers the area in `docs/guides/`; the
  walkthrough of any example it touches in `docs/examples/`;
  `README.md` if a feature list or the examples table is affected;
  `docs/design/gaps.md` if a listed gap closed or a new one opened; and
  this file if a concept, path or rule changed. A new guide needs
  `group:` and `order:` in its front matter beside `title:` and
  `summary:`; the groups are Start, Engine, Graphics, Simulation and
  Audio, and a guide with no group or an unknown one is listed last
  under Other. Doc comments follow the house style: lead with the
  instruction ("To do X, call Y"), then the behaviour, then the caveat;
  short declarative sentences in the present tense; no metaphors,
  similes or analogies; no em-dashes; say what a thing is for; state the
  zero-value default.
- Commit messages describe what changed and why in prose; every commit
  is verified by the test suite and the cross-compiles first.

## Gotchas worth knowing

- `lin.Ortho` maps its bottom argument to the top of the screen; a y-up
  world passes top as bottom (see `Camera.Projection`).
- `LineWrapper` in go-text mutates the runs it is given and reuses its
  output storage; copy in and out (see `gfx/text.go`).
- The render-target colour images need `TRANSFER_DST` because
  `ClearColorForSampling` clears them before their first pass.
- `BeginSwapchainPass` stores its depth attachment rather than
  discarding it, though nothing reads it. A depth attachment that is
  cleared and discarded in a pass that records no draw takes a driver
  fast path that keeps the sample count of the pass before it, and after
  a multisampled scene pass that faults the GPU with a depth-target size
  violation. A frame that only updates a render texture is exactly that
  shape, and `internal/render/multisample_test.go` pins it. Storing a
  depth buffer that was only cleared costs almost nothing.
- Uploads inside a frame (`NewMesh`, `Mesh.Update`, `UpdateSkinned`,
  `NewTexture`, `Texture.Write`, `NewEnvironment`) copy through the
  per-slot staging arena (`render.Staging`, taken with
  `Graphics.stage`) into the frame's command buffer before any pass,
  with the barriers that let a draw recorded later in the frame read
  the data. Outside a frame they keep the `OneShot` path. Everything
  destroyed or replaced inside a frame goes on that slot's retire list
  through `Graphics.deferDestroy` and is freed at the slot's next
  `begin`, when its fence has been waited on; outside a frame
  `deferDestroy` waits the device and frees at once. A `Texture` or
  `Mesh` destroyed inside a frame keeps its image or buffers (and sets
  `destroyed`) until the retire runs, so draws already queued still
  draw. A model's morph descriptor set and buffer retire together;
  destruction captures their values before clearing the model's store.
  `FrameStats.Waits` counts the stalls a frame caused; a running
  game reports zero.
- `internal/render` has a retire ring of its own for objects `gfx`
  cannot reach: `Device.Retire` records a closure against the device's
  frame counter and `BeginFrame` runs the closures `FramesInFlight`
  frames old, where the slot's fence has just been waited on.
  `DynamicUniforms.grow` and `StorageSets.resize` use it: growing
  allocates a fresh descriptor pool, sets and buffers, swaps them in and
  retires the old pool, so a frame in flight keeps reading exactly what
  it bound and growth costs no `WaitIdle`. A descriptor set is only safe
  to rewrite in place when no submitted frame holds it; allocate a new
  one instead.
- Culling bounds a static mesh by its vertices, a skinned mesh by the
  union of its per-joint boxes under the pose (computed at load in
  `gfx/bounds.go`), and a mesh whose shader has a vertex hook by
  `Shader.VertexBounds`, whose zero means the draw is never culled.
  `Mesh.SetBounds` overrides the box and survives `Update`.
- Morph targets blend in the vertex shader (`gfx/morph.go`,
  `morph()` in `vert_common.glsl`, called before the game's `vertex`
  hook and before skinning). A model uploads every one of its meshes'
  deltas into one storage buffer at load, six floats a vertex a target,
  and binds it as set 5; a draw names up to `MaxGPUMorphTargets` open
  targets with their weights in the instance stream. Every mesh draw
  binds set 5, because the prelude names the buffer whether or not the
  draw reads it, so `meshPass.morphNone` is the empty one. A pose with
  more open targets than the cap falls back to the blend on the
  processor, which uploads, and switching back puts the rest pose up
  again first.
- Occlusion culling (`gfx/occlude.go`) runs after the frustum test in
  `prepareDraws`. `AddOccluder3D` meshes are rasterised into a CPU depth
  buffer (256 by 144 by default) holding the nearest occluder depth per
  pixel; a draw is culled when its bounding sphere's box is behind that
  everywhere it covers. Triangles are clipped in clip space against the
  near plane and the four sides first, so screen coordinates stay in the
  buffer and the fixed-point edge functions cannot overflow. An occluded
  draw is `culled` like any other, so it still casts shadows.
- A render texture is rendered in `end` with whatever `SetPost` left in
  force at that moment, not with the settings when `DrawTo` queued it, so
  two `DrawTo` calls in one frame cannot have different post settings.
  `BakeImpostor` sets its neutral settings once for both of a view's
  passes for that reason.
- `DrawBatch` does not queue anything: it puts the `StaticBatch` on the
  queue, and `prepareDraws` walks its hierarchy (`gfx/batch.go`) into
  `q.draws` once the frustum and the occlusion buffer are known. That is
  why `renderQueue`'s `has3D` counts queued batches as well as draws.
- A mesh shader has two fragment programs: the usual one and the one the
  order-independent transparency pass needs, which writes a second
  attachment (the accumulated colour and the revealage). `shaders.Compose`
  builds both from one source by substituting the `OUTPUT` line of
  `meshPostlude`, `bunyip-shader` always bundles both for `-kind mesh`,
  and a bundle without the second one (compiled before it existed) keeps
  its draws on the sorted path. The pass needs the device's
  `independentBlend`, because its two attachments blend differently; a
  device without it also keeps sorting.
- The atmospheric sky is written three times: `Sky.scatter` and
  `Sky.radiance` in `gfx/sky.go`, and the block between `// ATMOSPHERE.`
  and `// END ATMOSPHERE.` in `gfx/shaders/prelude_mesh.glsl` and in
  `gfx/shaders/skyparam.frag`, which are the same text. The ambient
  harmonics are projected from the Go side and the pixels come from the
  shaders, so a change to one is a change to all three.
  `TestAtmosphereBlocksMatch` compares the two shaders and
  `TestAtmosphereMatchesGo` renders the sky and checks it against
  `Sky.radiance`.
- The instance stream is fifteen `vec4`s at locations 5 to 16 and 19 to
  21, so a skinned mesh's joints and weights sit at 17 and 18
  (`vert_skin.glsl` and `skinVertexLayout`). The twelfth carries the
  draw's reflection probe index plus one and whether the draw is opaque.
  Three more `vec4`s follow them in the stride, the previous frame's
  model matrix, which only the velocity programs declare, and the morph
  block picks up after those at 22 to 24 plus a pair of packed words at
  25 (`morphInstanceOffset`).
- An opaque draw writes its screen-space reflection weight into the HDR
  alpha channel, which nothing else reads, and the reflection pass reads
  it back from the scene copy. A blended draw keeps its real alpha, which
  is what the opaque flag in the instance stream is for. The pass runs
  between the opaque and the blended draws, in its own pass without the
  depth attachment so it can sample the depth image.
- `BakeProbe` and `BakeLightProbes` render the scene through
  `renderScene` on their own one-shot command buffers, so they refuse to
  run inside `Draw`. They build a `baker`, which queues the game's scene
  once and re-renders it per face, and read the HDR image back with
  `Device.ReadImageRaw`; the prefilter and the harmonics are the same CPU
  code an image environment uses, over a cube sampler instead of an
  equirectangular one.
- The 3D draw order is a packed 64-bit key per draw (`gfx/sortkey.go`):
  class, then depth for blended draws or dense shader, uniform, material
  set and mesh ids for opaque ones, and the draw's index in the low
  twenty bits so ties keep submission order. A frame with more of any of
  those than a field holds falls back to `sortRecords`, which compares
  the draw records.
- `prepareDraws` runs before `writeUniforms`: the cascades need the
  caster bounds it resolves, and the shadow pass culls each draw against
  each cascade and spot light before recording it.
- `renderScene` calls `q.jitterFrame` before anything else, which fills
  `q.projJ`, `q.viewProjJ` and `q.invViewProjJ` with the sub-pixel
  offset temporal anti-aliasing needs (zero when it is off). It has to
  be there rather than in `renderQueue`, because a probe bake calls
  `renderScene` on its own command buffer; a bake takes no jitter and
  writes no motion vectors, which `g.frame != nil` tells it. Everything
  that rasterises the HDR pass or reads the depth buffer afterwards uses
  those and not `Camera.Projection`: the Frame block, the sky, the debug
  lines, SSAO, the velocity pass and the reprojection matrices. The
  cascades, the culling frustum and `gfx/pick.go` stay unjittered.
  `q.prevViewProj` is the previous frame's unjittered view-projection,
  set at the end of `renderQueue` and used by everything that reprojects.
  The clustered light lookup is deliberately left alone: `clusterAt` in
  `prelude_mesh.glsl` reads `gl_FragCoord` and `vViewDepth`, and
  `vViewDepth` comes from the unjittered view matrix, so the only effect
  of the jitter there is that a fragment within half a pixel of a tile
  edge can land in the neighbouring tile. Tiles are tens of pixels
  across, so it is ignored.
- The velocity image holds the object's own motion only, in texture
  coordinates: `ndc(prevViewProj * currentWorld) - ndc(prevViewProj *
  previousWorld)`. A draw the game did not mark as moved writes nothing,
  and the resolve passes reconstruct the camera's part from depth and
  add it. The velocity programs are standalone (`velocity.vert`,
  `velocity_skin.vert`, `velocity.frag`), take their matrices in a
  128-byte push block rather than the Frame block, and declare their own
  vertex layout (`velocityVertexLayout`), so the Frame block and the
  mesh preludes are untouched. The instance stream carries `prevModel`
  in the three vec4s past `gi`, which no other program declares.
- A 2D frame that goes through the post pass (`PostSettings.Post2D`)
  draws its stream into `t.ldr`, the swapchain-format image, and the
  composite reads it from there through `final2DSet`. That is why no 2D
  pipeline needs an HDR-format variant. The composite's `pc.d.y` flag
  skips exposure and tone mapping in that mode.
- Immediate-mode identity comes from the label plus the enclosing
  containers plus call order; overlays (menus, modals, drag ghosts) are
  drawn deferred at `end`, and overlays may add overlays, so that list
  is iterated by index.
- A placed convex in `phys` (`convex` in `gjk.go`) holds its core
  vertices in a fixed array, so placing a shape allocates nothing. Take
  its address only where the pointer cannot escape, or the whole value
  lands on the heap; `convexPair` and `meshContacts` keep theirs in the
  scratch for that reason. `phys/cache3.go` keeps each collider's placed
  parts between queries, keyed by the collider's placement and bounds, so
  a shape edited in place without moving goes stale.
- Each physics substep ends with a relax pass over the contacts that
  solves them again with the position-correction bias dropped. The bias
  leaves the bodies separating at about the sleep threshold, so without
  the pass a stack never rests. Restitution is held in its own field
  (`solverContact.restBias`) and stays in the relax pass, so bounces
  survive it.
- Input edges are fed into two sets at once: the per-update set that
  `endUpdate` clears and the frame set that `endFrame` clears, which
  Draw reads. Nothing copies one into the other, so a frame that runs
  no update still reports each edge once.
- A compressed texture holds blocks, so nothing writes texels into one:
  `Texture.Write` and `Replace` refuse, and `ReplaceCompressed` takes
  another KTX2 file. `NewCompressedTexture` uploads the file's levels as
  they are through `render.RecordLevelsUpload`, packing them into one
  staging allocation with each level's offset a multiple of sixteen,
  which every BC block size divides. Where `Device.SupportsFormat` says
  no, level 0 is decoded on the processor and the texture becomes an
  ordinary RGBA one, so the fallback is invisible except in the memory it
  costs. `gfx/ktx2` writes BC7 in modes 6 and 1 only, and its decoder
  reads back the same two; `TestCompressedMatchesCPUDecode` in `gfx`
  compares the hardware decode against that decoder, which is what proves
  the partition tables in `bc7tables.go`.
- `Texture.Replace` keeps the `*Texture` a game holds and swaps what is
  behind it, which is how `asset.Reloader` reloads a material's
  textures. The same size goes through `Write`; a different size takes a
  fresh image, so the cached material and image descriptor sets that
  name the old view are dropped through `forgetTexture` and the old
  image is retired. Anything that caches a descriptor set built from
  `Texture.img.View` has to be dropped there too.
- `NewTexture` and `Texture.Write` premultiply translucent texels in
  linear light before uploading to an sRGB image (`linearPremultiply`);
  Go's `image.RGBA` premultiplies in sRGB space, which an sRGB sampler
  would decode too dark. `Data` textures upload as given.
- The glyph atlas is one texture written in place. `Font.flush` uploads
  the glyphs rasterised so far through `Texture.Write`, whose in-frame
  path records the copy into the frame's command buffer before any
  render pass, so a glyph first drawn in a frame appears in it. Text
  drawing flushes before it queues its sprites. The atlas never grows,
  so a font whose atlas is full drops later glyphs rather than replacing
  the texture the frame's draws already point at.
- A colour glyph (COLR layers, an SVG document, a bitmap strike) is
  composited on the CPU in `gfx/colr.go` and `gfx/svgglyph.go` and
  stored premultiplied in linear light, because the atlas is a `Data`
  texture that samples without gamma decoding. `DrawGlyphs` draws such a
  glyph with a white tint, so a game's text colour does not reach it.
- Descriptor sets come from a chain of pools that grows on
  `VK_ERROR_OUT_OF_POOL_MEMORY`; the capacities in `newGraphics` and
  `post.go` are starting sizes, not limits.
- A headless renderer has no surface and no swapchain: `NewHeadlessSurface`
  returns zero, `Swapchain.Handle` stays zero and `BeginFrame`/`EndFrame`
  skip acquire and present. Do not add code that assumes a swapchain
  image is presentable.
- Reading the clipboard on X11 waits for another client to answer, for
  up to a second, inside the game's `Update`. Events that arrive during
  the wait are handled as usual but pushed onto `App.queued` rather than
  the slice `Poll` already returned, and the next `Poll` puts them back;
  anything that pushes an event on Linux has to go through `App.push`
  for that to hold.
- An ECS query walk snapshots every matched table's row count when it
  begins (`matcher.begin`), so entities the callback moves into another
  matched table are not visited twice. A walk must not start another
  walk of the same query inside its callback.
- The console is built in `runOnce` before `Init`, because it tees the
  log and the game logs during `Init`, but the game draws it: `Draw` is
  the game's, and the console has to be last. It cannot import the root
  package, so it declares `Frame` and the one-method `Host` that
  `Context` implements. Every console method is safe on a nil receiver,
  so `ctx.Console.Draw(ctx)` compiles and does nothing when
  `Config.Console` is off.
- The console's panels are laid out inside `ui.ScrollArea`, which has to
  be told how tall its contents are, so each tab counts its rows
  (`Console.rowsH`). A row added to a tab without adding to its count
  scrolls short.
- GPU pass times come from one timestamp query pool with a range per
  frame slot (`render.Timestamps`). A frame resets its slot's range at
  the top of its command buffer, and that reset first reads the results
  the same slot left `FramesInFlight` frames ago, whose fence
  `BeginFrame` has already waited on, so nothing waits and the figures
  lag the frame on screen. `Timestamps.Begin` and `End` bracket a pass;
  spans with the same name are summed, so a pass run for a render
  texture and for the screen reads as one. A device with a zero
  timestamp period or no valid timestamp bits gets a nil `*Timestamps`
  on which every method does nothing. `Begin` and `End` also do nothing
  on a command buffer that did not call `Reset`, so `renderScene` on a
  one-shot buffer (a `BakeProbe` face) writes into no query it never
  reset and spends none of the open frame's.

# Part two: using Bunyip in a game

## Setup

Go 1.26 or later and a Vulkan driver (`brew install vulkan-loader
molten-vk` on macOS). `go get github.com/matjam/bunyip`. Build with
`CGO_ENABLED=0`; there is nothing to link against.

## The shape of a game

```go
type game struct{ tex *gfx.Texture; font *gfx.Font }

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	g.tex, err = ctx.Gfx.NewTexture(img, gfx.TextureOptions{})
	return err
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) { ctx.Quit() }
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	ctx.Gfx.DrawTexture(g.tex, 100, 100)
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) { g.tex.Destroy() }

func main() {
	bunyip.Run(bunyip.Config{Title: "Game", Width: 1280, Height: 720}, &game{})
}
```

`Init` and `Shutdown` are optional. `Init` may run twice if the GPU
device is lost. Create GPU resources from the context in `Init`, `Update`
or `Draw` and destroy them from the same goroutine; never from another
goroutine.

## Where things are

| Need | Look at |
|---|---|
| The loop, the view, the window, screenshots, headless runs | `bunyip.Config`, `bunyip.Context` |
| Keys, mouse, gamepads, rebinding | `input.State`, `input.Actions` |
| Sprites, text, paths, tilemaps, cameras, particles | `gfx` (2D half), `particle`, `tiled` |
| Compressed textures and their offline mip chains | `cmd/bunyip-tex`, `gfx/ktx2`, `gfx.NewCompressedTexture` |
| Autotiling: terrain that picks its own tiles | `grid/autotile`, `tiled.WangSet` for sets from the Tiled editor |
| Meshes, materials, lights, shadows, sky, fog, post-processing | `gfx` (3D half), `gltf` for loading models |
| Reflection probes, baked light probe grids, screen-space reflections | `gfx.ReflectionProbe`, `gfx.LightProbeGrid`, `gfx.PostSettings.Reflections` |
| Game-written shaders | `cmd/bunyip-shader`, the shaders guide |
| Widgets, menus, text fields, themes | `ui` |
| A debug console and panels over a running game | `console`, `bunyip.Config.Console` |
| Entities, queries, systems, saves, prefabs, scene files | `ecs`, `asset.Scene` |
| Rigid bodies, joints, ragdolls, character controllers, raycasts | `phys` |
| Cloth, jelly and soft bodies, 2D fluids | `phys/soft` |
| Skeletal and keyframe animation, IK, blend spaces | `anim`, `gfx.AnimPlayer` |
| Orbits, planets, ships | `orbit`, `orbit/sol` |
| Sounds, music, positional audio, tracker modules | `audio`, `audio/tracker` |
| Assets and pack files, saves, translation, random numbers, timers, tweens, grids | `asset`, `save`, `locale`, `rng`, `timer`, `tween`, `grid` |
| Multiplayer | `network` |

## Conventions to remember

- 2D coordinates are view units with the origin at the top-left and +Y
  down; 3D is right-handed with +Y up. Angles are radians. Rectangles are
  `lin.Rect`; vectors are `lin.Vec2` and `lin.Vec3`.
- Colours are `gfx.Color` in linear space; `gfx.RGB` and `gfx.Hex`
  convert from sRGB bytes. A zero colour where a tint is expected means
  white.
- Zero values are defaults throughout; an empty `Material`, `Camera`,
  `PostSettings` or `Emitter` is valid.
- Everything drawn in `Draw` is queued; order within a layer is call
  order. Use `SetLayer` to order across calls.
- GPU resources (`Texture`, `Font`, `Mesh`, `Model`, `Shader`,
  `RenderTexture`, `Environment`, `ReflectionProbe`) have `Destroy`; call
  it in `Shutdown`.
- The interface is rebuilt every frame inside `ui.Begin`; values are
  passed by pointer, and widgets return whether something happened.
- Physics, animation and orbits are ECS systems: give entities the
  components, register the system, call `world.Update(ctx.Delta)` from
  `Update`.
- Examples are the fastest reference: `examples/<name>/main.go` is a
  complete program for each area, and every one runs with `-seconds 3
  -shot out.png`.

## Reading the documentation

Start with `llms.txt` on the site for the index. The guides explain each
area; the `pkg/<package>.md` pages are the full API with doc comments,
declarations and examples. The `README.md` lists the packages and the
examples.
