package ui

// panel is a container laying widgets out top to bottom; a row inside it
// lays them left to right.
type panel struct {
	id     widgetID
	rect   Rect
	cursor float32 // next widget's Y
	row    *row
}

type row struct {
	x, y, h float32
	count   int
	width   float32   // per-item width for equal rows
	weights []float32 // per-item weights for Columns; nil for equal rows
	total   float32
	inner   float32
}

func (c *Context) currentPanel() *panel {
	if len(c.panels) == 0 {
		return nil
	}
	return c.panels[len(c.panels)-1]
}

// Panel draws a titled box at r and lays out the widgets body creates
// inside it, top to bottom.
func (c *Context) Panel(title string, r Rect, body func()) {
	p := &panel{id: c.id("panel:" + title), rect: r, cursor: r.Y + c.Theme.Padding}
	c.panels = append(c.panels, p)
	c.frameRects = append(c.frameRects, r)
	c.box(c.skin().Panel, r, c.Theme.Panel, c.Theme.PanelBorder)
	if title != "" {
		_, h := c.Theme.Font.Measure(title)
		c.text(title, r.X+c.Theme.Padding, p.cursor, c.Theme.Title)
		p.cursor += h + c.Theme.Spacing
	}
	body()
	c.panels = c.panels[:len(c.panels)-1]
}

// Row lays the widgets body creates side by side, n of equal width.
func (c *Context) Row(n int, body func()) {
	p := c.currentPanel()
	if p == nil || n <= 0 {
		body()
		return
	}
	inner := p.rect.W - 2*c.Theme.Padding
	p.row = &row{x: p.rect.X + c.Theme.Padding, y: p.cursor, count: n, width: (inner - float32(n-1)*c.Theme.Spacing) / float32(n)}
	body()
	c.endRow(p)
}

// endRow closes a row that body left partly filled.
func (c *Context) endRow(p *panel) {
	if r := p.row; r != nil {
		p.cursor = r.y + max(r.h, c.Theme.RowHeight) + c.Theme.Spacing
		p.row = nil
	}
}

// nextWidth is the width the next widget will be given, so text can be
// wrapped to it before the rectangle is reserved.
func (c *Context) nextWidth() float32 {
	p := c.currentPanel()
	if p == nil {
		return 0
	}
	if r := p.row; r != nil {
		if r.weights != nil {
			i := len(r.weights) - r.count
			return r.inner * r.weights[i] / r.total
		}
		return r.width
	}
	return p.rect.W - 2*c.Theme.Padding
}

// next reserves the next widget rectangle of height h.
func (c *Context) next(h float32) Rect {
	p := c.currentPanel()
	if p == nil {
		return Rect{}
	}
	h = max(h, c.Theme.RowHeight)
	if r := p.row; r != nil {
		w := r.width
		if r.weights != nil {
			i := len(r.weights) - r.count
			w = r.inner * r.weights[i] / r.total
		}
		rect := Rect{X: r.x, Y: r.y, W: w, H: h}
		r.x += w + c.Theme.Spacing
		r.h = max(r.h, h)
		r.count--
		if r.count == 0 {
			p.cursor = r.y + r.h + c.Theme.Spacing
			p.row = nil
		}
		return rect
	}
	rect := Rect{X: p.rect.X + c.Theme.Padding, Y: p.cursor, W: p.rect.W - 2*c.Theme.Padding, H: h}
	p.cursor += h + c.Theme.Spacing
	return rect
}

// Space leaves a vertical gap.
func (c *Context) Space(h float32) {
	if p := c.currentPanel(); p != nil {
		p.cursor += h
	}
}
