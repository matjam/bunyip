package asset

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for Image
	_ "image/png"  // registers the PNG decoder for Image
	"path"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/audio/tracker"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/gltf"
)

// Errors from the loaders below name the asset: "asset sprites/hero.png:
// image: unknown format".

// Image reads and decodes a PNG or JPEG.
func Image(fs *FS, name string) (image.Image, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return img, nil
}

// Texture decodes an image and uploads it.
func Texture(g *gfx.Graphics, fs *FS, name string, opts gfx.TextureOptions) (*gfx.Texture, error) {
	img, err := Image(fs, name)
	if err != nil {
		return nil, err
	}
	tex, err := g.NewTexture(img, opts)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return tex, nil
}

// Atlas reads a TexturePacker or Aseprite JSON atlas, loads the image it
// names from the same directory, uploads it and binds the frames: one
// call where a game would otherwise parse, load and bind by hand.
func Atlas(g *gfx.Graphics, fs *FS, name string, opts gfx.TextureOptions) (*gfx.Atlas, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	d, err := gfx.ParseAtlas(data)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	if d.Image == "" {
		return nil, fmt.Errorf("asset %s: the atlas names no image", name)
	}
	tex, err := Texture(g, fs, path.Join(path.Dir(name), d.Image), opts)
	if err != nil {
		return nil, err
	}
	return d.Bind(tex), nil
}

// Aseprite reads an .aseprite or .ase file, composites each frame from
// its visible layers, uploads the packed image and binds an atlas: one
// call where a game would otherwise parse, pack, upload and bind by
// hand. The atlas is on the result's Atlas field, its frames are named
// by number and its tags play through Atlas.Animation; the result also
// carries the file's layers, tags, slices and palette.
func Aseprite(g *gfx.Graphics, fs *FS, name string, opts gfx.AsepriteOptions, texOpts gfx.TextureOptions) (*gfx.Aseprite, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	a, err := gfx.ParseAseprite(data, opts)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	if _, err := a.Upload(g, texOpts); err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return a, nil
}

// Font reads a TTF or OTF file and prepares a bitmap atlas at size.
func Font(g *gfx.Graphics, fs *FS, name string, size float32, opts gfx.FontOptions) (*gfx.Font, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	font, err := g.NewFont(data, size, opts)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return font, nil
}

// SDFFont reads a TTF or OTF file and prepares a scalable font.
func SDFFont(g *gfx.Graphics, fs *FS, name string, size float32, opts gfx.FontOptions) (*gfx.Font, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	font, err := g.NewSDFFont(data, size, opts)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return font, nil
}

// Sound reads and decodes a WAV, Ogg Vorbis or MP3 clip into the mixer's
// format.
func Sound(m *audio.Mixer, fs *FS, name string) (*audio.Sound, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	pcm, err := audio.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	snd, err := m.NewSound(pcm)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return snd, nil
}

// Music reads a WAV, Ogg Vorbis or MP3 file and opens it for streaming.
// The encoded bytes stay in memory; decoding happens as it plays.
func Music(m *audio.Mixer, fs *FS, name string, loop bool) (*audio.Music, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	mu, err := m.OpenMusic(bytes.NewReader(data), loop)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return mu, nil
}

// Model reads a .gltf or .glb file and uploads it. External buffers and
// images are read through the same FS relative to the model's directory.
func Model(g *gfx.Graphics, fs *FS, name string) (*gfx.Model, error) {
	doc, err := parseModel(fs, name)
	if err != nil {
		return nil, err
	}
	model, err := g.LoadModel(doc)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return model, nil
}

// parseModel reads and parses a glTF file without touching the GPU.
func parseModel(fs *FS, name string) (*gltf.Document, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	dir := path.Dir(name)
	resolve := func(uri string) ([]byte, error) {
		return fs.Read(path.Join(dir, uri))
	}
	doc, err := gltf.Parse(data, resolve)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return doc, nil
}

// Tracker reads and parses a MOD, S3M, XM or IT module.
func Tracker(fs *FS, name string) (*tracker.Module, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	mod, err := tracker.Load(data)
	if err != nil {
		return nil, fmt.Errorf("asset %s: %w", name, err)
	}
	return mod, nil
}
