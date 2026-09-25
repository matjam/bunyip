package gltf

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// pngOf encodes a w by h image whose first pixel is shade.
func pngOf(t *testing.T, w, h int, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.SetRGBA(0, 0, color.RGBA{shade, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// gltfWithImages is a .gltf document whose images are the given URIs.
func gltfWithImages(t *testing.T, uris []string) []byte {
	t.Helper()
	var images []map[string]string
	for _, u := range uris {
		images = append(images, map[string]string{"uri": u})
	}
	js, err := json.Marshal(map[string]any{"asset": map[string]string{"version": "2.0"}, "images": images})
	if err != nil {
		t.Fatal(err)
	}
	return js
}

// TestImagesDecodeInOrder checks that images decoded side by side come
// back in the document's order.
func TestImagesDecodeInOrder(t *testing.T) {
	var uris []string
	for i := range 12 {
		uris = append(uris, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(pngOf(t, i+1, 2*i+1, uint8(i*20))))
	}
	doc, err := Parse(gltfWithImages(t, uris), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Images) != len(uris) {
		t.Fatalf("%d images, want %d", len(doc.Images), len(uris))
	}
	for i, img := range doc.Images {
		if b := img.Bounds(); b.Dx() != i+1 || b.Dy() != 2*i+1 {
			t.Errorf("image %d is %v, want %dx%d", i, b, i+1, 2*i+1)
		}
		if r, _, _, _ := img.At(0, 0).RGBA(); uint8(r>>8) != uint8(i*20) {
			t.Errorf("image %d has shade %d, want %d", i, r>>8, i*20)
		}
	}
}

// TestImagesFirstErrorByIndex checks that of several bad images the
// error names the first one in the document, whether it fails to decode
// or to fetch, as a decode in order would.
func TestImagesFirstErrorByIndex(t *testing.T) {
	good := pngOf(t, 4, 4, 1)
	files := map[string][]byte{"a.png": good, "b.png": []byte("not a png"), "c.png": good, "d.png": []byte("nor this")}
	resolve := func(uri string) ([]byte, error) {
		if data, ok := files[uri]; ok {
			return data, nil
		}
		return nil, &json.SyntaxError{}
	}
	for _, c := range []struct {
		uris []string
		want string
	}{
		{[]string{"a.png", "b.png", "c.png", "d.png"}, "image 1:"},
		{[]string{"a.png", "c.png", "d.png", "b.png"}, "image 2:"},
		{[]string{"a.png", "b.png", "missing.png", "d.png"}, "image 1:"},
		{[]string{"a.png", "c.png", "missing.png", "b.png"}, "image 2:"},
		{[]string{"missing.png", "b.png"}, "image 0:"},
	} {
		_, err := Parse(gltfWithImages(t, c.uris), resolve)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("images %v: error %v, want one naming %q", c.uris, err, c.want)
		}
	}
}
