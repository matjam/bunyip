package gltf

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"testing"
)

// onePNG is a one-pixel PNG as a data URI, so a test document can hold a
// texture the loader will resolve.
func onePNG(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// materialDoc parses a document whose one material carries the given
// JSON, with one texture available and no geometry to get in the way.
func materialDoc(t *testing.T, material string) Material {
	t.Helper()
	bin := triangleBin()
	uri := base64.StdEncoding.EncodeToString(bin)
	src := fmt.Sprintf(`{"asset":{"version":"2.0"},
"buffers":[{"byteLength":%d,"uri":"data:application/octet-stream;base64,%s"}],
"bufferViews":[{"buffer":0,"byteOffset":0,"byteLength":36},{"buffer":0,"byteOffset":36,"byteLength":6}],
"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"},{"bufferView":1,"componentType":5123,"count":3,"type":"SCALAR"}],
"images":[{"uri":"%s"}],"textures":[{"source":0}],
"materials":[%s],
"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1,"material":0}]}],
"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}]}`, len(bin), uri, onePNG(t), material)
	doc, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Materials) != 1 {
		t.Fatalf("materials: %+v", doc.Materials)
	}
	return doc.Materials[0]
}

func near(t *testing.T, name string, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 0.02 {
		t.Errorf("%s is %v, want %v", name, got, want)
	}
}

func TestMaterialExtensionDefaults(t *testing.T) {
	m := materialDoc(t, `{"pbrMetallicRoughness":{}}`)
	near(t, "specular factor", m.SpecularFactor, 1)
	if m.SpecularColor != [3]float32{1, 1, 1} {
		t.Errorf("specular colour is %v, want white", m.SpecularColor)
	}
	near(t, "iridescence", m.IridescenceFactor, 0)
	near(t, "iridescence ior", m.IridescenceIOR, 1.3)
	near(t, "anisotropy", m.AnisotropyStrength, 0)
	if m.SpecGloss {
		t.Error("a plain material claims the specular-glossiness workflow")
	}
	for name, image := range map[string]int{
		"specular": m.SpecularImage, "specular colour": m.SpecularColorImage,
		"iridescence": m.IridescenceImage, "iridescence thickness": m.IridescenceThicknessImage,
		"anisotropy": m.AnisotropyImage, "specular-glossiness": m.SpecGlossImage,
	} {
		if image != -1 {
			t.Errorf("the %s image is %d, want -1 for none", name, image)
		}
	}
}

func TestIridescenceExtension(t *testing.T) {
	m := materialDoc(t, `{"extensions":{"KHR_materials_iridescence":{
"iridescenceFactor":0.8,"iridescenceIor":1.8,
"iridescenceThicknessMinimum":200,"iridescenceThicknessMaximum":900}}}`)
	near(t, "iridescence", m.IridescenceFactor, 0.8)
	near(t, "film ior", m.IridescenceIOR, 1.8)
	near(t, "thickness maximum", m.IridescenceThicknessMax, 900)
	// With no thickness map the film is at its maximum everywhere, so the
	// minimum is pulled up to meet it.
	near(t, "thickness minimum", m.IridescenceThicknessMin, 900)
	m = materialDoc(t, `{"extensions":{"KHR_materials_iridescence":{
"iridescenceFactor":1,"iridescenceThicknessTexture":{"index":0}}},
"pbrMetallicRoughness":{}}`)
	near(t, "thickness minimum with a map", m.IridescenceThicknessMin, 100)
	near(t, "thickness maximum with a map", m.IridescenceThicknessMax, 400)
}

func TestAnisotropyExtension(t *testing.T) {
	m := materialDoc(t, `{"extensions":{"KHR_materials_anisotropy":{"anisotropyStrength":0.6,"anisotropyRotation":1.57}}}`)
	near(t, "anisotropy", m.AnisotropyStrength, 0.6)
	near(t, "rotation", m.AnisotropyRotation, 1.57)
}

func TestSpecularExtension(t *testing.T) {
	m := materialDoc(t, `{"extensions":{"KHR_materials_specular":{"specularFactor":0.5,"specularColorFactor":[1,0.5,0.25]}}}`)
	near(t, "specular factor", m.SpecularFactor, 0.5)
	if m.SpecularColor != [3]float32{1, 0.5, 0.25} {
		t.Errorf("specular colour is %v", m.SpecularColor)
	}
}

// TestSpecularGlossiness checks the conversion to metallic-roughness: a
// bright specular colour with a dark diffuse is metal, and a dark
// specular with a bright diffuse is a dielectric.
func TestSpecularGlossiness(t *testing.T) {
	gold := materialDoc(t, `{"extensions":{"KHR_materials_pbrSpecularGlossiness":{
"diffuseFactor":[0,0,0,1],"specularFactor":[1,0.766,0.336],"glossinessFactor":0.9}}}`)
	if !gold.SpecGloss {
		t.Fatal("the converted material does not say it came from specular-glossiness")
	}
	near(t, "metallic", gold.Metallic, 1)
	near(t, "roughness", gold.Roughness, 0.1)
	if gold.BaseColor[0] < 0.9 || gold.BaseColor[2] > 0.5 {
		t.Errorf("a gold specular colour became base colour %v, want the specular colour itself", gold.BaseColor)
	}
	plastic := materialDoc(t, `{"extensions":{"KHR_materials_pbrSpecularGlossiness":{
"diffuseFactor":[0.2,0.4,0.8,1],"specularFactor":[0.04,0.04,0.04],"glossinessFactor":0.25}}}`)
	near(t, "metallic", plastic.Metallic, 0)
	near(t, "roughness", plastic.Roughness, 0.75)
	if plastic.BaseColor[2] < plastic.BaseColor[0] {
		t.Errorf("a blue diffuse became base colour %v", plastic.BaseColor)
	}
	near(t, "base colour blue", plastic.BaseColor[2], 0.8)
}
