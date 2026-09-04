package gfx

import (
	"bytes"
	"encoding/xml"
	"image"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/go-text/typesetting/font"
	"golang.org/x/image/vector"

	"github.com/matjam/bunyip/lin"
)

// An OpenType SVG glyph is a small SVG document per glyph. Enough of SVG
// is drawn here for the glyph fonts that use it: paths and the basic
// shapes, groups, uses, transforms, opacity, solid fills and linear and
// radial gradients. Strokes, clipping paths, masks, filters, patterns,
// text, images, style sheets and animation are not drawn, and the
// even-odd fill rule is drawn as non-zero.

// svgMaxDepth guards against a document that uses itself.
const svgMaxDepth = 32

// svgElem is one element of a parsed document.
type svgElem struct {
	tag  string
	attr map[string]string
	kids []*svgElem
}

// addSVG draws a glyph the font describes as an SVG document and puts it
// in the atlas. It reports false when the document cannot be drawn, so
// the caller can fall back to the outline the font must also carry.
func (f *Font) addSVG(face uint8, gid font.GID, data font.GlyphSVG) (glyph, bool) {
	root, err := parseSVGDocument(data.Source)
	if err != nil {
		return glyph{}, false
	}
	ff := f.faces[face]
	r := &svgRender{
		f: f, root: root, ids: map[string]*svgElem{},
		k: f.pxPerEm / ff.upem,
	}
	indexSVG(root, r.ids)
	// A document may hold several glyphs, each in an element named for
	// its glyph index; the rest of the document is then the others.
	target := root
	if el, ok := r.ids["glyph"+strconv.Itoa(int(gid))]; ok {
		target = el
	}
	// The user space is font units with y down, so a viewBox maps onto
	// the em square sitting on the baseline.
	view := svgViewBox(root, ff.upem)
	state := svgState{xform: view, fill: svgPaint{color: rgba{0, 0, 0, 1}, has: true}, opacity: 1}
	b := r.bounds(target, state, 0)
	if !b.valid() {
		return glyph{}, false
	}
	ox := int(math.Floor(float64(b.minX * r.k)))
	oy := int(math.Floor(float64(b.minY * r.k)))
	r.w = int(math.Ceil(float64(b.maxX*r.k))) - ox + 1
	r.h = int(math.Ceil(float64(b.maxY*r.k))) - oy + 1
	if r.w <= 0 || r.h <= 0 || r.w > f.packer.width || r.h > f.packer.height {
		return glyph{}, false
	}
	// The glyph's own space is font units with y down, so the pixel
	// transform scales and shifts without flipping.
	r.toPix = affine{a: r.k, d: r.k, tx: float32(-ox), ty: float32(-oy)}
	r.rast = vector.NewRasterizer(r.w, r.h)
	r.alpha = image.NewAlpha(image.Rect(0, 0, r.w, r.h))
	canvas := newColorCanvas(r.w, r.h)
	r.draw(canvas, target, state, 0)
	return f.addCanvas(canvas, ox, oy)
}

// svgViewBox returns the transform from the document's user space to
// font units with y down. Without a viewBox one user unit is one font
// unit; with one, the box is fitted into the em square that sits on the
// baseline, keeping its aspect ratio and centred, which is what
// preserveAspectRatio defaults to.
func svgViewBox(root *svgElem, upem float32) affine {
	// Attribute names are held in lower case, as the parser reads them.
	nums := svgNumbers(root.attr["viewbox"])
	if len(nums) != 4 || nums[2] <= 0 || nums[3] <= 0 {
		return identityAffine
	}
	s := min(upem/nums[2], upem/nums[3])
	return affine{
		a: s, d: s,
		tx: -nums[0]*s + (upem-nums[2]*s)/2,
		ty: -nums[1]*s + (upem-nums[3]*s)/2 - upem,
	}
}

// parseSVGDocument reads an SVG document into elements, ignoring the
// text between them.
func parseSVGDocument(src []byte) (*svgElem, error) {
	dec := xml.NewDecoder(bytes.NewReader(src))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	var stack []*svgElem
	var root *svgElem
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el := &svgElem{tag: strings.ToLower(t.Name.Local), attr: map[string]string{}}
			for _, a := range t.Attr {
				el.attr[strings.ToLower(a.Name.Local)] = a.Value
			}
			if n := len(stack); n > 0 {
				stack[n-1].kids = append(stack[n-1].kids, el)
			} else if root == nil {
				root = el
			}
			stack = append(stack, el)
		case xml.EndElement:
			if n := len(stack); n > 0 {
				stack = stack[:n-1]
			}
		}
	}
	if root == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return root, nil
}

// indexSVG collects the elements that carry an id, for use and paint
// references.
func indexSVG(el *svgElem, into map[string]*svgElem) {
	if id := el.attr["id"]; id != "" {
		if _, seen := into[id]; !seen {
			into[id] = el
		}
	}
	for _, k := range el.kids {
		indexSVG(k, into)
	}
}

// svgPaint is a resolved fill: a colour, a gradient, or nothing.
type svgPaint struct {
	color rgba
	grad  *svgElem // a gradient element, resolved per shape
	has   bool     // false means the fill is none
}

// svgState is what an element inherits from its parents.
type svgState struct {
	xform   affine
	fill    svgPaint
	opacity float32 // multiplied into every fill below
}

// svgRender draws one glyph's document.
type svgRender struct {
	f     *Font
	root  *svgElem
	ids   map[string]*svgElem
	k     float32
	toPix affine
	w, h  int
	rast  *vector.Rasterizer
	alpha *image.Alpha
}

// bounds returns the box the element's shapes cover, in font units with
// y down.
func (r *svgRender) bounds(el *svgElem, st svgState, depth int) box {
	b := emptyBox
	r.walk(el, st, depth, func(el *svgElem, st svgState, path []svgSeg) {
		for _, s := range path {
			for i := range s.n {
				x, y := st.xform.apply(s.pts[i].X, s.pts[i].Y)
				b.add(x, y)
			}
		}
	})
	return b
}

// draw paints the element and its children onto the canvas.
func (r *svgRender) draw(dst *colorCanvas, el *svgElem, st svgState, depth int) {
	r.walk(el, st, depth, func(el *svgElem, st svgState, path []svgSeg) {
		fill := svgFillOf(el, st)
		if !fill.has || len(path) == 0 {
			return
		}
		mask := r.mask(path, st.xform)
		if mask == nil {
			return
		}
		alpha := st.opacity * svgNumber(svgProperty(el, "fill-opacity"), 1)
		if fill.grad != nil {
			if at, ok := r.gradient(fill.grad, st.xform, alpha, path); ok {
				dst.fill(mask, at)
			}
			return
		}
		c := fill.color
		c.a *= alpha
		dst.fill(mask, solid(c))
	})
}

// walk visits every shape under an element with the state it inherits,
// resolving groups, uses and transforms.
func (r *svgRender) walk(el *svgElem, st svgState, depth int, shape func(*svgElem, svgState, []svgSeg)) {
	if depth > svgMaxDepth {
		return
	}
	if t := el.attr["transform"]; t != "" {
		st.xform = st.xform.mul(parseSVGTransform(t))
	}
	if o := svgProperty(el, "opacity"); o != "" {
		st.opacity *= lin.Clamp(svgNumber(o, 1), 0, 1)
	}
	st.fill = svgFillOf(el, st)
	switch el.tag {
	case "defs", "clippath", "mask", "pattern", "filter", "style", "text", "symbol", "marker":
		return // not drawn, and not drawn through a use either
	case "use":
		href := el.attr["href"]
		if href == "" {
			href = el.attr["xlink:href"]
		}
		target, ok := r.ids[strings.TrimPrefix(href, "#")]
		if !ok || target == el {
			return
		}
		x, y := svgNumber(el.attr["x"], 0), svgNumber(el.attr["y"], 0)
		if x != 0 || y != 0 {
			st.xform = st.xform.mul(translateAffine(x, y))
		}
		r.walk(target, st, depth+1, shape)
		return
	}
	if path := svgShapePath(el); len(path) > 0 {
		shape(el, st, path)
	}
	for _, k := range el.kids {
		r.walk(k, st, depth+1, shape)
	}
}

// svgProperty reads a presentation attribute, which an inline style
// overrides.
func svgProperty(el *svgElem, name string) string {
	if style := el.attr["style"]; style != "" {
		for decl := range strings.SplitSeq(style, ";") {
			key, value, ok := strings.Cut(decl, ":")
			if ok && strings.EqualFold(strings.TrimSpace(key), name) {
				return strings.TrimSpace(value)
			}
		}
	}
	return el.attr[name]
}

// svgFillOf resolves an element's fill against the one it inherits.
func svgFillOf(el *svgElem, st svgState) svgPaint {
	value := strings.TrimSpace(svgProperty(el, "fill"))
	switch {
	case value == "":
		return st.fill
	case value == "none" || value == "transparent":
		return svgPaint{}
	case strings.HasPrefix(value, "url("):
		id := strings.TrimSuffix(strings.TrimPrefix(value, "url("), ")")
		id = strings.Trim(strings.TrimSpace(id), `"'`)
		return svgPaint{grad: &svgElem{attr: map[string]string{"id": strings.TrimPrefix(id, "#")}}, has: true}
	}
	if c, ok := parseSVGColor(value); ok {
		return svgPaint{color: c, has: true}
	}
	return st.fill
}

// gradient resolves a gradient reference into a colour function in pixel
// space. The path's box is needed for the object bounding box units a
// gradient uses by default.
func (r *svgRender) gradient(ref *svgElem, xform affine, alpha float32, path []svgSeg) (func(x, y float32) rgba, bool) {
	el, ok := r.ids[ref.attr["id"]]
	if !ok {
		return nil, false
	}
	line := colorLine{extend: svgSpread(el.attr["spreadmethod"])}
	for _, k := range el.kids {
		if k.tag != "stop" {
			continue
		}
		c, ok := parseSVGColor(strings.TrimSpace(svgProperty(k, "stop-color")))
		if !ok {
			c = rgba{0, 0, 0, 1}
		}
		c.a *= svgNumber(svgProperty(k, "stop-opacity"), 1) * alpha
		line.stops = append(line.stops, colorStop{offset: svgOffset(svgProperty(k, "offset")), color: c})
	}
	if len(line.stops) == 0 {
		return nil, false
	}
	// Gradient coordinates are either in the user space of the shape or
	// in its bounding box, and carry a transform of their own.
	m := xform
	if !strings.EqualFold(el.attr["gradientunits"], "userSpaceOnUse") {
		b := emptyBox
		for _, s := range path {
			for i := range s.n {
				b.add(s.pts[i].X, s.pts[i].Y)
			}
		}
		if !b.valid() {
			return nil, false
		}
		m = m.mul(affine{a: max(b.maxX-b.minX, 1e-6), d: max(b.maxY-b.minY, 1e-6), tx: b.minX, ty: b.minY})
	}
	if t := el.attr["gradienttransform"]; t != "" {
		m = m.mul(parseSVGTransform(t))
	}
	inv, ok := r.toPix.mul(m).invert()
	if !ok {
		return nil, false
	}
	unit := func(name string, def float32) float32 {
		return svgOffsetOr(el.attr[name], def)
	}
	switch el.tag {
	case "lineargradient":
		return linearGradient(line, unit("x1", 0), unit("y1", 0), unit("x2", 1), unit("y2", 0), inv), true
	case "radialgradient":
		cx, cy, rr := unit("cx", 0.5), unit("cy", 0.5), unit("r", 0.5)
		fx, fy := svgOffsetOr(el.attr["fx"], cx), svgOffsetOr(el.attr["fy"], cy)
		return radialGradient(line, fx, fy, 0, cx, cy, rr, inv), true
	}
	return nil, false
}

// svgSpread reads a gradient's spread method.
func svgSpread(s string) extendMode {
	switch strings.ToLower(s) {
	case "repeat":
		return extendRepeat
	case "reflect":
		return extendReflect
	}
	return extendPad
}

// mask rasterises a path as coverage over the glyph's pixels.
func (r *svgRender) mask(path []svgSeg, m affine) []float32 {
	full := r.toPix.mul(m)
	r.rast.Reset(r.w, r.h)
	drawn := false
	for _, s := range path {
		x0, y0 := full.apply(s.pts[0].X, s.pts[0].Y)
		switch s.op {
		case svgMoveTo:
			r.rast.MoveTo(x0, y0)
		case svgLineTo:
			r.rast.LineTo(x0, y0)
			drawn = true
		case svgCubeTo:
			x1, y1 := full.apply(s.pts[1].X, s.pts[1].Y)
			x2, y2 := full.apply(s.pts[2].X, s.pts[2].Y)
			r.rast.CubeTo(x0, y0, x1, y1, x2, y2)
			drawn = true
		case svgClose:
			r.rast.ClosePath()
		}
	}
	if !drawn {
		return nil
	}
	r.rast.ClosePath()
	clear(r.alpha.Pix)
	r.rast.Draw(r.alpha, r.alpha.Bounds(), image.Opaque, image.Point{})
	out := make([]float32, r.w*r.h)
	any := false
	for y := range r.h {
		row := r.alpha.Pix[y*r.alpha.Stride:]
		for x := range r.w {
			v := float32(row[x]) / 255
			out[y*r.w+x] = v
			any = any || v > 0
		}
	}
	if !any {
		return nil
	}
	return out
}

// parseSVGColor reads a colour keyword, a hex colour or an rgb() colour
// into linear light.
func parseSVGColor(s string) (rgba, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return rgba{}, false
	}
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		var r, g, b uint8
		switch len(hex) {
		case 3, 4:
			v, err := strconv.ParseUint(hex[:3], 16, 32)
			if err != nil {
				return rgba{}, false
			}
			r = uint8(v>>8&0xf) * 0x11
			g = uint8(v>>4&0xf) * 0x11
			b = uint8(v&0xf) * 0x11
		case 6, 8:
			v, err := strconv.ParseUint(hex[:6], 16, 32)
			if err != nil {
				return rgba{}, false
			}
			r, g, b = uint8(v>>16), uint8(v>>8), uint8(v)
		default:
			return rgba{}, false
		}
		return rgba{srgbToLinear(r), srgbToLinear(g), srgbToLinear(b), 1}, true
	}
	if strings.HasPrefix(strings.ToLower(s), "rgb") {
		open := strings.IndexByte(s, '(')
		close := strings.IndexByte(s, ')')
		if open < 0 || close < open {
			return rgba{}, false
		}
		parts := svgNumbers(strings.ReplaceAll(s[open+1:close], ",", " "))
		if len(parts) < 3 {
			return rgba{}, false
		}
		channel := func(v float32) float32 {
			return srgbToLinear(uint8(lin.Clamp(v, 0, 255) + 0.5))
		}
		a := float32(1)
		if len(parts) > 3 {
			a = lin.Clamp(parts[3], 0, 1)
		}
		return rgba{channel(parts[0]), channel(parts[1]), channel(parts[2]), a}, true
	}
	// The colour keywords glyph documents use in practice, plus the two
	// that mean the text colour, which a colour glyph draws as white.
	switch strings.ToLower(s) {
	case "currentcolor":
		return rgba{1, 1, 1, 1}, true
	case "black":
		return rgba{0, 0, 0, 1}, true
	case "white":
		return rgba{1, 1, 1, 1}, true
	case "red":
		return rgba{srgbToLinear(255), 0, 0, 1}, true
	case "lime", "green":
		return rgba{0, srgbToLinear(255), 0, 1}, true
	case "blue":
		return rgba{0, 0, srgbToLinear(255), 1}, true
	case "yellow":
		return rgba{srgbToLinear(255), srgbToLinear(255), 0, 1}, true
	case "cyan", "aqua":
		return rgba{0, srgbToLinear(255), srgbToLinear(255), 1}, true
	case "magenta", "fuchsia":
		return rgba{srgbToLinear(255), 0, srgbToLinear(255), 1}, true
	case "gray", "grey":
		return rgba{srgbToLinear(128), srgbToLinear(128), srgbToLinear(128), 1}, true
	case "silver":
		return rgba{srgbToLinear(192), srgbToLinear(192), srgbToLinear(192), 1}, true
	case "orange":
		return rgba{srgbToLinear(255), srgbToLinear(165), 0, 1}, true
	}
	return rgba{}, false
}

// svgNumber reads a length as a number, dropping a unit suffix.
func svgNumber(s string, def float32) float32 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if nums := svgNumbers(s); len(nums) > 0 {
		return nums[0]
	}
	return def
}

// svgOffset reads a gradient stop offset, which may be a percentage.
func svgOffset(s string) float32 { return svgOffsetOr(s, 0) }

// svgOffsetOr reads a number that may be a percentage of the unit
// interval, falling back to a default.
func svgOffsetOr(s string, def float32) float32 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if strings.HasSuffix(s, "%") {
		return svgNumber(strings.TrimSuffix(s, "%"), def*100) / 100
	}
	return svgNumber(s, def)
}

// svgNumbers reads the numbers of an attribute, which may be separated
// by spaces, commas or nothing at all.
func svgNumbers(s string) []float32 {
	var out []float32
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		j := i
		if c == '+' || c == '-' {
			j++
		}
		seenDot, seenExp := false, false
		for j < len(s) {
			d := s[j]
			switch {
			case d >= '0' && d <= '9':
			case d == '.' && !seenDot && !seenExp:
				seenDot = true
			case (d == 'e' || d == 'E') && !seenExp && j > i:
				seenExp = true
				if j+1 < len(s) && (s[j+1] == '+' || s[j+1] == '-') {
					j++
				}
			default:
				goto done
			}
			j++
		}
	done:
		if j == i {
			i++ // a character that starts no number
			continue
		}
		v, err := strconv.ParseFloat(s[i:j], 32)
		if err == nil {
			out = append(out, float32(v))
		}
		i = j
	}
	return out
}

// parseSVGTransform reads a transform list.
func parseSVGTransform(s string) affine {
	m := identityAffine
	for len(s) > 0 {
		open := strings.IndexByte(s, '(')
		if open < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(strings.Trim(s[:open], " ,\t\n\r")))
		closing := strings.IndexByte(s[open:], ')')
		if closing < 0 {
			break
		}
		args := svgNumbers(s[open+1 : open+closing])
		s = s[open+closing+1:]
		arg := func(i int, def float32) float32 {
			if i < len(args) {
				return args[i]
			}
			return def
		}
		switch name {
		case "matrix":
			if len(args) >= 6 {
				m = m.mul(affine{a: args[0], b: args[1], c: args[2], d: args[3], tx: args[4], ty: args[5]})
			}
		case "translate":
			m = m.mul(translateAffine(arg(0, 0), arg(1, 0)))
		case "scale":
			m = m.mul(scaleAffine(arg(0, 1), arg(1, arg(0, 1))))
		case "rotate":
			rot := rotateAffine(arg(0, 0) * math.Pi / 180)
			if len(args) >= 3 {
				rot = around(rot, args[1], args[2])
			}
			m = m.mul(rot)
		case "skewx":
			m = m.mul(skewAffine(arg(0, 0)*math.Pi/180, 0))
		case "skewy":
			m = m.mul(skewAffine(0, arg(0, 0)*math.Pi/180))
		}
	}
	return m
}
