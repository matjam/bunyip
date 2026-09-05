package gfx

import "testing"

// FuzzDecodeHDR feeds the Radiance decoder corrupt files: an error, never
// a panic. It needs no GPU.
func FuzzDecodeHDR(f *testing.F) {
	f.Add([]byte("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n\x80\x80\x80\x80"))
	f.Add([]byte("#?RADIANCE\n\n-Y 2 +X 2\n\x02\x02\x00\x02\x01\x80\x01\x80\x01\x80\x01\x80\x02\x02\x00\x02\x01\x80\x01\x80\x01\x80\x01\x80"))
	f.Add([]byte("#?RGBE\n"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeHDR(data)
	})
}

// FuzzDecodeEXR feeds the OpenEXR decoder corrupt files: an error, never
// a panic. It needs no GPU.
func FuzzDecodeEXR(f *testing.F) {
	pix := exrGradient(3, 2)
	f.Add(encodeEXR(3, 2, pix, true, exrNone))
	f.Add(encodeEXR(3, 2, pix, false, exrZIP))
	f.Add(encodeEXR(3, 2, pix, true, exrRLE))
	f.Add([]byte("v/\x01\x00\x02\x00\x00\x00"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeEXR(data)
	})
}

// FuzzParseAtlas feeds the atlas parsers corrupt JSON.
func FuzzParseAtlas(f *testing.F) {
	f.Add([]byte(`{"frames":{"a.png":{"frame":{"x":0,"y":0,"w":8,"h":8}}},"meta":{"size":{"w":16,"h":16}}}`))
	f.Add([]byte(`{"frames":[{"filename":"a","frame":{"x":0,"y":0,"w":8,"h":8},"duration":100}],"meta":{"size":{"w":16,"h":16},"frameTags":[{"name":"run","from":0,"to":0}]}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseAtlas(data)
	})
}

// FuzzParseAseprite feeds the Aseprite reader corrupt files: an error,
// never a panic and never an unbounded allocation.
func FuzzParseAseprite(f *testing.F) {
	f.Add(twoLayerFile(f))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := ParseAseprite(data, AsepriteOptions{Layers: true})
		if err == nil && a.Image == nil {
			t.Fatal("parsed with no image")
		}
	})
}

// FuzzSVGGlyph feeds the SVG glyph reader corrupt documents: no panic
// and no loop, whatever a font holds. It needs no GPU.
func FuzzSVGGlyph(f *testing.F) {
	f.Add(`<svg viewBox="0 0 10 10"><path d="M0 0 L10 10 A 5 5 0 1 1 0 0 Z" fill="#f00"/></svg>`)
	f.Add(`<svg><g transform="rotate(45 1 2) scale(2)"><rect width="4" height="4" fill="url(#g)"/></g>` +
		`<defs><linearGradient id="g"><stop offset="0" stop-color="red"/></linearGradient></defs></svg>`)
	f.Add(`<svg><use href="#a"/><g id="a"><use href="#a"/></g></svg>`)
	f.Add(`<svg><path d="M0 0 z 5 5 5"/></svg>`)
	f.Add(`<svg`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, s string) {
		root, err := parseSVGDocument([]byte(s))
		if err != nil {
			return
		}
		r := &svgRender{root: root, ids: map[string]*svgElem{}, k: 0.1}
		indexSVG(root, r.ids)
		_ = r.bounds(root, svgState{xform: svgViewBox(root, 1000), opacity: 1}, 0)
	})
}

// FuzzParseRich feeds the rich text parser arbitrary markup.
func FuzzParseRich(f *testing.F) {
	f.Add("plain")
	f.Add("<b>bold</b> <i>it</i> <color=#ff0000>red</color> <link=x>go</link>")
	f.Add("<b><i>unclosed")
	f.Add("</b>>>><<")
	f.Fuzz(func(t *testing.T, s string) {
		_ = ParseRich(s)
	})
}
