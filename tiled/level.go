package tiled

import (
	"fmt"
	"image"
	"path"

	"github.com/matjam/bunyip/asset"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Images fetches a tileset or image layer picture by the path the map
// names, relative to the map's directory.
type Images func(path string) (image.Image, error)

// ImagesFrom reads images through an asset FS, with paths joined to the
// map's directory.
func ImagesFrom(fs *asset.FS, dir string) Images {
	return func(p string) (image.Image, error) {
		return asset.Image(fs, path.Join(dir, p))
	}
}

// LoadFS reads a map through an asset FS, resolving external tilesets
// relative to the map's directory.
func LoadFS(fs *asset.FS, name string) (*Map, error) {
	data, err := fs.Read(name)
	if err != nil {
		return nil, fmt.Errorf("tiled: %w", err)
	}
	dir := path.Dir(name)
	m, err := Parse(data, func(p string) ([]byte, error) {
		return fs.Read(path.Join(dir, p))
	})
	if err != nil {
		return nil, fmt.Errorf("tiled: %s: %w", name, err)
	}
	return m, nil
}

// Level is a map uploaded for drawing: one tilemap per tileset a layer
// uses, in the map's layer order.
type Level struct {
	Map    *Map
	Layers []LevelLayer
	// Sheets index Map.Tilesets; an image-collection tileset has none.
	Sheets   []*gfx.Sheet
	textures []*gfx.Texture
}

// LevelLayer is one tile or object layer with the group state above it
// applied: a layer inside a hidden group is hidden, offsets add, and
// opacities multiply.
type LevelLayer struct {
	Name string
	Kind LayerKind
	// Maps hold the layer's cells split by tileset, in Map.Tilesets
	// order for the tilesets the layer uses; a cell belonging to another
	// tileset is -1 in each.
	Maps    []*gfx.Tilemap
	Objects []Object
	Visible bool
	Opacity float32
	Offset  lin.Vec2
}

// Build uploads a map's tileset images (nearest filtered, no mipmaps),
// fills tilemaps from its tile layers, and registers tile animations.
// Image layers and image-collection tilesets are not drawn. The Level
// owns the textures; Destroy frees them.
func Build(g *gfx.Graphics, m *Map, images Images) (*Level, error) {
	lv := &Level{Map: m, Sheets: make([]*gfx.Sheet, len(m.Tilesets))}
	for i := range m.Tilesets {
		ts := &m.Tilesets[i]
		if ts.Image == "" {
			continue
		}
		img, err := images(ts.Image)
		if err != nil {
			lv.Destroy()
			return nil, fmt.Errorf("tiled: tileset %q: %w", ts.Name, err)
		}
		tex, err := g.NewTexture(img, gfx.TextureOptions{NoMipmaps: true})
		if err != nil {
			lv.Destroy()
			return nil, fmt.Errorf("tiled: tileset %q: %w", ts.Name, err)
		}
		lv.textures = append(lv.textures, tex)
		lv.Sheets[i] = sheet(tex, ts)
	}
	lv.walk(m.Layers, lin.Vec2{}, 1, true)
	return lv, nil
}

// sheet describes the tileset's grid over its texture. The tileset's
// own columns win over the texture size so margins and spacing hold.
func sheet(tex *gfx.Texture, ts *Tileset) *gfx.Sheet {
	s := gfx.NewSheet(tex, ts.TileWidth, ts.TileHeight)
	s.Margin, s.Spacing = ts.Margin, ts.Spacing
	if ts.Columns > 0 {
		s.Columns = ts.Columns
		s.Rows = max((ts.TileCount+ts.Columns-1)/ts.Columns, 1)
	}
	return s
}

func (lv *Level) walk(layers []Layer, offset lin.Vec2, opacity float32, visible bool) {
	for i := range layers {
		l := &layers[i]
		off := offset.Add(lin.V2(l.OffsetX, l.OffsetY))
		op, vis := opacity*l.Opacity, visible && l.Visible
		switch l.Kind {
		case GroupLayer:
			lv.walk(l.Layers, off, op, vis)
		case TileLayer:
			lv.Layers = append(lv.Layers, LevelLayer{Name: l.Name, Kind: TileLayer, Maps: lv.tilemaps(l),
				Visible: vis, Opacity: op, Offset: off})
		case ObjectLayer:
			lv.Layers = append(lv.Layers, LevelLayer{Name: l.Name, Kind: ObjectLayer, Objects: l.Objects,
				Visible: vis, Opacity: op, Offset: off})
		}
	}
}

// tilemaps splits a tile layer's cells into one tilemap per tileset it
// draws from.
func (lv *Level) tilemaps(l *Layer) []*gfx.Tilemap {
	m := lv.Map
	byTileset := map[int]*gfx.Tilemap{}
	var order []int
	for i, gid := range l.Data {
		ts, local := m.Tileset(gid)
		if ts == nil {
			continue
		}
		idx := tilesetIndex(m, ts)
		if lv.Sheets[idx] == nil {
			continue
		}
		tm := byTileset[idx]
		if tm == nil {
			tm = gfx.NewTilemap(lv.Sheets[idx], l.Width, l.Height)
			tm.TileW, tm.TileH = float32(m.TileWidth), float32(m.TileHeight)
			byTileset[idx] = tm
			order = append(order, idx)
		}
		_, fx, fy, diag := SplitGID(gid)
		tm.Tiles[i] = gfx.TileFlipped(local, fx, fy, diag)
	}
	var out []*gfx.Tilemap
	for _, idx := range order {
		tm := byTileset[idx]
		for id, tile := range m.Tilesets[idx].Tiles {
			if len(tile.Animation) == 0 {
				continue
			}
			var a gfx.TileAnimation
			for _, f := range tile.Animation {
				a.Frames = append(a.Frames, f.TileID)
				a.Durations = append(a.Durations, f.Duration)
			}
			tm.Animate(id, a)
		}
		out = append(out, tm)
	}
	return out
}

func tilesetIndex(m *Map, ts *Tileset) int {
	for i := range m.Tilesets {
		if &m.Tilesets[i] == ts {
			return i
		}
	}
	return -1
}

// Draw draws every visible tile layer in order with the map's origin at
// (x, y). Layer opacity scales the tint's alpha.
func (lv *Level) Draw(g *gfx.Graphics, x, y float32, tint gfx.Color) {
	if tint == (gfx.Color{}) {
		tint = gfx.White
	}
	for _, l := range lv.Layers {
		if !l.Visible || l.Kind != TileLayer {
			continue
		}
		c := tint.WithAlpha(tint.A * l.Opacity)
		for _, tm := range l.Maps {
			g.DrawTilemap(tm, x+l.Offset.X, y+l.Offset.Y, c)
		}
	}
}

// Advance moves every tile animation forward by dt seconds.
func (lv *Level) Advance(dt float64) {
	for _, l := range lv.Layers {
		for _, tm := range l.Maps {
			tm.Advance(dt)
		}
	}
}

// Layer returns the first level layer with the name, or nil.
func (lv *Level) Layer(name string) *LevelLayer {
	for i := range lv.Layers {
		if lv.Layers[i].Name == name {
			return &lv.Layers[i]
		}
	}
	return nil
}

// Size is the map's pixel size.
func (lv *Level) Size() lin.Vec2 {
	return lin.V2(float32(lv.Map.Width*lv.Map.TileWidth), float32(lv.Map.Height*lv.Map.TileHeight))
}

// Destroy frees the tileset textures.
func (lv *Level) Destroy() {
	for _, t := range lv.textures {
		t.Destroy()
	}
	lv.textures = nil
}
