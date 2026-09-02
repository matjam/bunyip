package ui

import "github.com/matjam/bunyip/gfx"

// Slice is a nine-slice texture: the corners keep their size, the edges
// stretch along one axis and the centre fills the rest, so one small
// image skins boxes of any size.
type Slice struct {
	Tex                      *gfx.Texture
	Left, Top, Right, Bottom float32   // border sizes in texture pixels
	Tint                     gfx.Color // zero means untinted
}

// Skin holds the art for each widget part. Any nil slice falls back to
// the theme's flat colours, so a skin can start with just a button.
type Skin struct {
	Panel        *Slice
	Button       *Slice
	ButtonHover  *Slice
	ButtonActive *Slice
	Field        *Slice // text fields and drop-down heads
	FieldFocus   *Slice
	Check        *Slice // checkbox box, unticked
	CheckOn      *Slice // checkbox box, ticked
	Track        *Slice // slider, progress and scrollbar tracks
	Fill         *Slice // the filled part of sliders and progress bars
	Knob         *Slice // slider handle
	Thumb        *Slice // scrollbar handle
}

var noSkin Skin

func (c *Context) skin() *Skin {
	if c.Theme.Skin != nil {
		return c.Theme.Skin
	}
	return &noSkin
}

// box draws r with the slice when there is one, otherwise a flat fill
// and, when border is not the zero colour, a border.
func (c *Context) box(s *Slice, r Rect, fill, border gfx.Color) {
	if s != nil && s.Tex != nil {
		tint := s.Tint
		if tint == (gfx.Color{}) {
			tint = gfx.White
		}
		c.g.DrawNineSlice(s.Tex, r.X, r.Y, r.W, r.H, s.Left, s.Top, s.Right, s.Bottom, tint)
		return
	}
	c.fill(r, fill)
	if border != (gfx.Color{}) {
		c.border(r, border)
	}
}

// or returns the first slice that is set.
func or(slices ...*Slice) *Slice {
	for _, s := range slices {
		if s != nil {
			return s
		}
	}
	return nil
}
