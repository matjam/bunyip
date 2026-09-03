package tiled

import (
	"image/color"

	"github.com/matjam/bunyip/grid/autotile"
)

// WangSet is one terrain set of a tileset, as drawn in the Tiled
// editor's terrain tool: named colours and, for each participating
// tile, the colour at each of its eight positions.
type WangSet struct {
	Name string
	// Type is "corner", "edge" or "mixed", saying which positions the
	// set paints.
	Type   string
	Colors []WangColor
	Tiles  []WangSetTile
}

// WangColor is one terrain colour of a set. Colours are numbered from 1
// in WangSetTile.WangID, in the order they appear here.
type WangColor struct {
	Name        string
	Color       color.RGBA
	Tile        int     // a representative tile id, or -1
	Probability float64 // relative chance among variants; zero counts as 1
}

// WangSetTile gives one tile's colours, clockwise from north: N, NE, E,
// SE, S, SW, W, NW. Zero means the position has no colour.
type WangSetTile struct {
	TileID int
	WangID [8]int
}

// Rules converts the set to autotile rules, with tile ids as frames: a
// tileset cut with gfx.NewSheet uses them directly. A "corner" set
// matches corners, an "edge" set edges, anything else all eight
// positions. A tile's weight is the product of its colours'
// probabilities, so variants keep the balance set in the editor.
func (ws *WangSet) Rules() *autotile.Rules {
	t := autotile.WangFull
	switch ws.Type {
	case "corner":
		t = autotile.WangCorners
	case "edge":
		t = autotile.WangEdges
	}
	tiles := make([]autotile.WangTile, 0, len(ws.Tiles))
	for _, wt := range ws.Tiles {
		tile := autotile.WangTile{Frame: wt.TileID, Colors: wt.WangID, Weight: 1}
		seen := map[int]bool{}
		for _, c := range wt.WangID {
			if c > 0 && c <= len(ws.Colors) && !seen[c] {
				seen[c] = true
				if p := ws.Colors[c-1].Probability; p > 0 {
					tile.Weight *= p
				}
			}
		}
		tiles = append(tiles, tile)
	}
	return autotile.Wang(t, tiles)
}

// WangSet finds a terrain set by name across the map's tilesets, or
// nil. The frames its Rules produce are local tile ids within the
// returned tileset.
func (m *Map) WangSet(name string) (*Tileset, *WangSet) {
	for i := range m.Tilesets {
		ts := &m.Tilesets[i]
		for j := range ts.WangSets {
			if ts.WangSets[j].Name == name {
				return ts, &ts.WangSets[j]
			}
		}
	}
	return nil, nil
}
