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
