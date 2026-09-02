package tiled

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/matjam/bunyip/lin"
)

// parseXML decodes a .tmx map. It fills the same Map the JSON path
// does, so Build and the accessors need not know which form was read.
func parseXML(data []byte, resolve Resolver) (*Map, error) {
	var x xmlMap
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("parse XML: %w", err)
	}
	m := &Map{
		Width: x.Width, Height: x.Height, TileWidth: x.TileWidth, TileHeight: x.TileHeight,
		Orientation: x.Orientation, Infinite: x.Infinite, Properties: xmlProperties(x.Properties),
	}
	if x.BackgroundColor != "" {
		if c, ok := ParseColor(x.BackgroundColor); ok {
			m.BackgroundColor = c
		}
	}
	for i, t := range x.Tilesets {
		var ts Tileset
		var err error
		if t.Source != "" {
			ts, err = externalTileset(t.Source, t.FirstGID, resolve)
		} else {
			ts, err = convertXMLTileset(t, "")
		}
		if err != nil {
			return nil, fmt.Errorf("tileset %d: %w", i, err)
		}
		m.Tilesets = append(m.Tilesets, ts)
	}
	sortTilesets(m)
	var err error
	if m.Layers, err = xmlLayers(x.Layers); err != nil {
		return nil, err
	}
	return m, nil
}

// parseXMLTileset decodes a .tsx document, or the tileset element of a
// map, with its paths rebased from dir to the map's directory.
func parseXMLTileset(data []byte, dir string) (Tileset, error) {
	var x xmlTileset
	if err := xml.Unmarshal(data, &x); err != nil {
		return Tileset{}, fmt.Errorf("parse XML: %w", err)
	}
	return convertXMLTileset(x, dir)
}

func convertXMLTileset(x xmlTileset, dir string) (Tileset, error) {
	ts := Tileset{
		FirstGID: x.FirstGID, Name: x.Name,
		TileWidth: x.TileWidth, TileHeight: x.TileHeight, Columns: x.Columns, TileCount: x.TileCount,
		Margin: x.Margin, Spacing: x.Spacing, Properties: xmlProperties(x.Properties),
	}
	if x.Image != nil {
		ts.Image = rebase(dir, x.Image.Source)
		ts.ImageWidth, ts.ImageHeight = x.Image.Width, x.Image.Height
	}
	for _, t := range x.Tiles {
		tile := Tile{ID: t.ID, Properties: xmlProperties(t.Properties)}
		if t.Image != nil {
			tile.Image = rebase(dir, t.Image.Source)
			tile.ImageWidth, tile.ImageHeight = t.Image.Width, t.Image.Height
		}
		if t.Animation != nil {
			for _, f := range t.Animation.Frames {
				tile.Animation = append(tile.Animation, Frame{TileID: f.TileID, Duration: float32(f.Duration) / 1000})
			}
		}
		if t.ObjectGroup != nil {
			var err error
			if tile.Collision, err = xmlObjects(t.ObjectGroup.Objects); err != nil {
				return Tileset{}, fmt.Errorf("tile %d: %w", t.ID, err)
			}
		}
		if ts.Tiles == nil {
			ts.Tiles = map[int]Tile{}
		}
		ts.Tiles[t.ID] = tile
	}
	return ts, nil
}

// xmlLayers converts a map's or group's layer elements in document
// order. Elements that are not layers, such as editorsettings, are
// skipped.
func xmlLayers(xs []xmlLayer) ([]Layer, error) {
	var out []Layer
	for _, x := range xs {
		l := Layer{
			ID: x.ID, Name: x.Name, Width: x.Width, Height: x.Height, Visible: true, Opacity: 1,
			OffsetX: float32(x.OffsetX), OffsetY: float32(x.OffsetY), Properties: xmlProperties(x.Properties),
		}
		if x.Visible != nil {
			l.Visible = *x.Visible
		}
		if x.Opacity != nil {
			l.Opacity = float32(*x.Opacity)
		}
		var err error
		switch x.XMLName.Local {
		case "layer":
			l.Kind = TileLayer
			err = xmlTileData(&l, x.Data)
		case "objectgroup":
			l.Kind = ObjectLayer
			l.Objects, err = xmlObjects(x.Objects)
		case "imagelayer":
			l.Kind = ImageLayer
			if x.Image != nil {
				l.Image = x.Image.Source
			}
		case "group":
			l.Kind = GroupLayer
			l.Layers, err = xmlLayers(x.Layers)
		default:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("layer %q: %w", x.Name, err)
		}
		out = append(out, l)
	}
	return out, nil
}

// xmlTileData fills a tile layer from its data element: whole-layer
// cells, or chunks for infinite maps.
func xmlTileData(l *Layer, d *xmlData) error {
	if d == nil {
		return fmt.Errorf("layer has no data")
	}
	if len(d.Chunks) == 0 {
		cells, err := xmlCells(d.Encoding, d.Compression, d.Text, d.Tiles)
		if err != nil {
			return err
		}
		return setCells(l, cells)
	}
	chunks := make([]chunk, len(d.Chunks))
	for i, c := range d.Chunks {
		cells, err := xmlCells(d.Encoding, d.Compression, c.Text, c.Tiles)
		if err != nil {
			return fmt.Errorf("chunk (%d,%d): %w", c.X, c.Y, err)
		}
		chunks[i] = chunk{x: c.X, y: c.Y, width: c.Width, height: c.Height, cells: cells}
	}
	return flattenChunks(l, chunks)
}

// xmlCells reads a data element's cells: tile child elements when no
// encoding is named, comma-separated ids for csv, or base64 with the
// optional compression.
func xmlCells(encoding, compression, text string, tiles []xmlDataTile) ([]uint32, error) {
	switch encoding {
	case "":
		cells := make([]uint32, len(tiles))
		for i, t := range tiles {
			cells[i] = t.GID
		}
		return cells, nil
	case "csv":
		fields := strings.Split(strings.TrimSpace(text), ",")
		cells := make([]uint32, len(fields))
		for i, f := range fields {
			v, err := strconv.ParseUint(strings.TrimSpace(f), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("data: csv cell %d: %w", i, err)
			}
			cells[i] = uint32(v)
		}
		return cells, nil
	case "base64":
		return decodeBase64Cells(text, compression)
	default:
		return nil, fmt.Errorf("%w: encoding %q", ErrUnsupported, encoding)
	}
}

func xmlObjects(xs []xmlObject) ([]Object, error) {
	var out []Object
	for _, x := range xs {
		o := Object{
			ID: x.ID, Name: x.Name, Class: x.Class, X: float32(x.X), Y: float32(x.Y),
			Width: float32(x.Width), Height: float32(x.Height), Rotation: float32(x.Rotation),
			GID: x.GID, Visible: true, Point: x.Point != nil, Ellipse: x.Ellipse != nil,
			Properties: xmlProperties(x.Properties),
		}
		if o.Class == "" {
			o.Class = x.Type // the attribute's name outside Tiled 1.9
		}
		if x.Visible != nil {
			o.Visible = *x.Visible
		}
		var err error
		if x.Polygon != nil {
			if o.Polygon, err = xmlPoints(x.Polygon.Points); err != nil {
				return nil, fmt.Errorf("object %d: polygon: %w", x.ID, err)
			}
		}
		if x.Polyline != nil {
			if o.Polyline, err = xmlPoints(x.Polyline.Points); err != nil {
				return nil, fmt.Errorf("object %d: polyline: %w", x.ID, err)
			}
		}
		if x.Text != nil {
			o.Text = x.Text.Text
		}
		out = append(out, o)
	}
	return out, nil
}

// xmlPoints reads a polygon's "x,y x,y ..." attribute.
func xmlPoints(s string) ([]lin.Vec2, error) {
	fields := strings.Fields(s)
	out := make([]lin.Vec2, len(fields))
	for i, f := range fields {
		xs, ys, ok := strings.Cut(f, ",")
		if !ok {
			return nil, fmt.Errorf("point %q", f)
		}
		x, err := strconv.ParseFloat(xs, 32)
		if err != nil {
			return nil, fmt.Errorf("point %q: %w", f, err)
		}
		y, err := strconv.ParseFloat(ys, 32)
		if err != nil {
			return nil, fmt.Errorf("point %q: %w", f, err)
		}
		out[i] = lin.V2(float32(x), float32(y))
	}
	return out, nil
}

// xmlProperties converts a properties element. Members of a class value
// carry their own types here, so a float member stays a float64 even
// when whole; the JSON form has no member types and gives int for a
// whole number. The accessors on Properties convert either way. A
// malformed value is dropped rather than failing the whole map, as in
// the JSON path.
func xmlProperties(p *xmlPropertyList) Properties {
	if p == nil || len(p.List) == 0 {
		return nil
	}
	out := Properties{}
	for _, x := range p.List {
		if v := xmlPropertyValue(x); v != nil {
			out[x.Name] = v
		}
	}
	return out
}

func xmlPropertyValue(x xmlProperty) any {
	if x.Type == "class" {
		return xmlProperties(x.Properties)
	}
	// A multi-line string is written as the element's text instead of
	// the value attribute.
	text := x.Text
	if x.Value != nil {
		text = *x.Value
	}
	switch x.Type {
	case "int", "object":
		v, err := strconv.Atoi(text)
		if err != nil {
			return nil
		}
		return v
	case "float":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil
		}
		return v
	case "bool":
		v, err := strconv.ParseBool(text)
		if err != nil {
			return nil
		}
		return v
	default: // string, color, file
		return text
	}
}

// The XML shapes Tiled writes. Attribute and element names match the
// file's. A map's layers of every kind are collected in document order
// through the catch-all field so their draw order holds.

type xmlMap struct {
	XMLName         xml.Name         `xml:"map"`
	Width           int              `xml:"width,attr"`
	Height          int              `xml:"height,attr"`
	TileWidth       int              `xml:"tilewidth,attr"`
	TileHeight      int              `xml:"tileheight,attr"`
	Orientation     string           `xml:"orientation,attr"`
	BackgroundColor string           `xml:"backgroundcolor,attr"`
	Infinite        bool             `xml:"infinite,attr"`
	Properties      *xmlPropertyList `xml:"properties"`
	Tilesets        []xmlTileset     `xml:"tileset"`
	Layers          []xmlLayer       `xml:",any"`
}

// xmlLayer is a layer, objectgroup, imagelayer or group element; the
// kind is in XMLName.
type xmlLayer struct {
	XMLName    xml.Name
	ID         int              `xml:"id,attr"`
	Name       string           `xml:"name,attr"`
	Width      int              `xml:"width,attr"`
	Height     int              `xml:"height,attr"`
	Visible    *bool            `xml:"visible,attr"`
	Opacity    *float64         `xml:"opacity,attr"`
	OffsetX    float64          `xml:"offsetx,attr"`
	OffsetY    float64          `xml:"offsety,attr"`
	Properties *xmlPropertyList `xml:"properties"`
	Data       *xmlData         `xml:"data"`
	Objects    []xmlObject      `xml:"object"`
	Image      *xmlImage        `xml:"image"`
	Layers     []xmlLayer       `xml:",any"`
}

type xmlData struct {
	Encoding    string        `xml:"encoding,attr"`
	Compression string        `xml:"compression,attr"`
	Tiles       []xmlDataTile `xml:"tile"`
	Chunks      []xmlChunk    `xml:"chunk"`
	Text        string        `xml:",chardata"`
}

type xmlChunk struct {
	X      int           `xml:"x,attr"`
	Y      int           `xml:"y,attr"`
	Width  int           `xml:"width,attr"`
	Height int           `xml:"height,attr"`
	Tiles  []xmlDataTile `xml:"tile"`
	Text   string        `xml:",chardata"`
}

type xmlDataTile struct {
	GID uint32 `xml:"gid,attr"`
}

type xmlObject struct {
	ID         int              `xml:"id,attr"`
	Name       string           `xml:"name,attr"`
	Type       string           `xml:"type,attr"`
	Class      string           `xml:"class,attr"`
	X          float64          `xml:"x,attr"`
	Y          float64          `xml:"y,attr"`
	Width      float64          `xml:"width,attr"`
	Height     float64          `xml:"height,attr"`
	Rotation   float64          `xml:"rotation,attr"`
	GID        uint32           `xml:"gid,attr"`
	Visible    *bool            `xml:"visible,attr"`
	Properties *xmlPropertyList `xml:"properties"`
	Ellipse    *xmlEmpty        `xml:"ellipse"`
	Point      *xmlEmpty        `xml:"point"`
	Polygon    *xmlPointList    `xml:"polygon"`
	Polyline   *xmlPointList    `xml:"polyline"`
	Text       *xmlText         `xml:"text"`
}

type xmlEmpty struct{}

type xmlPointList struct {
	Points string `xml:"points,attr"`
}

type xmlText struct {
	Text string `xml:",chardata"`
}

type xmlTileset struct {
	XMLName    xml.Name         `xml:"tileset"`
	FirstGID   uint32           `xml:"firstgid,attr"`
	Source     string           `xml:"source,attr"`
	Name       string           `xml:"name,attr"`
	TileWidth  int              `xml:"tilewidth,attr"`
	TileHeight int              `xml:"tileheight,attr"`
	Columns    int              `xml:"columns,attr"`
	TileCount  int              `xml:"tilecount,attr"`
	Margin     int              `xml:"margin,attr"`
	Spacing    int              `xml:"spacing,attr"`
	Image      *xmlImage        `xml:"image"`
	Properties *xmlPropertyList `xml:"properties"`
	Tiles      []xmlTile        `xml:"tile"`
}

type xmlImage struct {
	Source string `xml:"source,attr"`
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
}

type xmlTile struct {
	ID          int              `xml:"id,attr"`
	Properties  *xmlPropertyList `xml:"properties"`
	Image       *xmlImage        `xml:"image"`
	ObjectGroup *xmlObjectGroup  `xml:"objectgroup"`
	Animation   *xmlAnimation    `xml:"animation"`
}

type xmlObjectGroup struct {
	Objects []xmlObject `xml:"object"`
}

type xmlAnimation struct {
	Frames []xmlFrame `xml:"frame"`
}

type xmlFrame struct {
	TileID   int `xml:"tileid,attr"`
	Duration int `xml:"duration,attr"` // milliseconds
}

type xmlPropertyList struct {
	List []xmlProperty `xml:"property"`
}

type xmlProperty struct {
	Name       string           `xml:"name,attr"`
	Type       string           `xml:"type,attr"`
	Value      *string          `xml:"value,attr"`
	Text       string           `xml:",chardata"`
	Properties *xmlPropertyList `xml:"properties"`
}
