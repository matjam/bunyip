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

// Rules converts the set to autotile rules for a square grid, with tile
// ids as frames: a tileset cut with gfx.NewSheet uses them directly. A
// "corner" set matches corners, an "edge" set edges, anything else all
// eight positions. A tile's weight is the product of its colours'
// probabilities, so variants keep the balance set in the editor. It is
// RulesFor(autotile.Square); a hexagonal map wants RulesFor(m.Layout()).
func (ws *WangSet) Rules() *autotile.Rules { return ws.RulesFor(autotile.Square) }

// RulesFor converts the set to autotile rules for a layout, which the
// map supplies through Map.Layout. On a hexagonal layout it moves each
// colour into the direction slot the layout uses: Tiled stores a
// hexagon's six sides in the eight-slot wangid one place back, so the
// slot the editor calls the top holds the north-east side of a rows
// hexagon. Give the same layout to the Mapper that applies the rules.
func (ws *WangSet) RulesFor(layout autotile.Layout) *autotile.Rules {
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
		if layout.Hex() {
			tile.Colors = hexColors(wt.WangID, layout)
		}
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

// hexColors moves a hexagon's six colours from Tiled's wangid slots into
// the direction slots the layout uses. Tiled numbers a hexagon's sides
// clockwise through the same eight slots, starting one slot before the
// side's own direction, and leaves the two slots for the directions the
// hexagon has no side in at zero.
func hexColors(id [8]int, layout autotile.Layout) [8]int {
	var out [8]int
	for _, d := range layout.Dirs() {
		out[d] = id[(d+7)%8]
	}
	return out
}

// Layout returns the autotile layout that matches the map's shape, for
// the Mapper that walks its cells and for WangSet.RulesFor. A
// hexagonal map gives the rows or columns layout its stagger axis and
// stagger index name. Every other orientation gives autotile.Square,
// isometric maps included: Tiled's terrain tool matches an isometric
// map on the plain grid neighbours, so a set painted there lines up
// with Square. A game whose isometric tiles are drawn by their
// direction on screen sets autotile.IsoDiamond on the Mapper itself.
func (m *Map) Layout() autotile.Layout {
	if m.Orientation != "hexagonal" {
		return autotile.Square
	}
	even := m.StaggerIndex == "even"
	if m.StaggerAxis == "x" {
		if even {
			return autotile.HexColsEven
		}
		return autotile.HexColsOdd
	}
	if even {
		return autotile.HexRowsEven
	}
	return autotile.HexRowsOdd
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
