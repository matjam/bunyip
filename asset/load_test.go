package asset

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/ecs"
)

// encodeWAV16 writes PCM as a 16-bit WAV file.
func encodeWAV16(p audio.PCM) []byte {
	var buf bytes.Buffer
	dataLen := len(p.Samples) * 2
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVEfmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(p.Channels))
	binary.Write(&buf, binary.LittleEndian, uint32(p.Rate))
	binary.Write(&buf, binary.LittleEndian, uint32(p.Rate*p.Channels*2))
	binary.Write(&buf, binary.LittleEndian, uint16(p.Channels*2))
	binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataLen))
	for _, s := range p.Samples {
		binary.Write(&buf, binary.LittleEndian, int16(max(-1, min(1, s))*32767))
	}
	return buf.Bytes()
}

// buildMOD makes a 4-channel MOD with one looping square sample and one
// pattern holding a single C-2 on channel 0.
func buildMOD() []byte {
	var b []byte
	b = append(b, make([]byte, 20)...) // title
	for i := range 31 {
		h := make([]byte, 30)
		if i == 0 {
			copy(h, "square")
			binary.BigEndian.PutUint16(h[22:], 16) // length in words
			h[25] = 64                             // volume
			binary.BigEndian.PutUint16(h[26:], 0)  // loop start
			binary.BigEndian.PutUint16(h[28:], 16) // loop length
		}
		b = append(b, h...)
	}
	b = append(b, 1, 0) // song length, restart
	b = append(b, make([]byte, 128)...)
	b = append(b, "M.K."...)
	pattern := make([]byte, 64*4*4)
	pattern[0] = byte(428 >> 8)
	pattern[1] = byte(428 & 0xFF)
	pattern[2] = 0x10 // instrument 1
	b = append(b, pattern...)
	sample := make([]byte, 32)
	for i := range sample {
		if i < 16 {
			sample[i] = 100
		} else {
			sample[i] = byte(256 - 100)
		}
	}
	return append(b, sample...)
}

func encodeImage(t *testing.T, encode func(*bytes.Buffer, image.Image) error) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := range 3 {
		for x := range 4 {
			img.Set(x, y, color.NRGBA{R: uint8(x * 60), G: uint8(y * 80), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImage(t *testing.T) {
	pngData := encodeImage(t, func(w *bytes.Buffer, img image.Image) error { return png.Encode(w, img) })
	jpgData := encodeImage(t, func(w *bytes.Buffer, img image.Image) error { return jpeg.Encode(w, img, nil) })
	fs, err := OpenFS(FSSource(fstest.MapFS{
		"sprites/hero.png": {Data: pngData},
		"photo.jpg":        {Data: jpgData},
		"bad.png":          {Data: []byte("not an image")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	for _, name := range []string{"sprites/hero.png", "photo.jpg"} {
		img, err := Image(fs, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 3 {
			t.Fatalf("%s: bounds %v", name, b)
		}
	}
	if _, err := Image(fs, "bad.png"); err == nil || !strings.Contains(err.Error(), "asset bad.png:") {
		t.Fatalf("bad image error %v", err)
	}
	_, err = Image(fs, "missing.png")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "asset missing.png:") {
		t.Fatalf("missing image error %v", err)
	}
}

func TestLoadersAcceptStandardFS(t *testing.T) {
	files := fstest.MapFS{
		"hero.png":        {Data: encodeImage(t, func(w *bytes.Buffer, img image.Image) error { return png.Encode(w, img) })},
		"beep.wav":        {Data: encodeWAV16(audio.Sine(440, 0.01, 22050))},
		"tune.mod":        {Data: buildMOD()},
		"models/tri.gltf": {Data: []byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"../tri.bin","byteLength":12}]}`)},
		"tri.bin":         {Data: make([]byte, 12)},
	}
	img, err := Image(files, "hero.png")
	if err != nil || img.Bounds().Dx() != 4 {
		t.Fatalf("image: %v %v", img, err)
	}
	m := audio.NewMixer(44100)
	if _, err := Sound(m, files, "beep.wav"); err != nil {
		t.Fatal(err)
	}
	music, err := Music(m, files, "beep.wav", false)
	if err != nil {
		t.Fatal(err)
	}
	music.Close()
	if _, err := Tracker(files, "tune.mod"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseModel(files, "models/tri.gltf"); err != nil {
		t.Fatal(err)
	}
}

func TestSoundAndMusic(t *testing.T) {
	wav := encodeWAV16(audio.Sine(440, 0.1, 22050))
	fs, err := OpenFS(FSSource(fstest.MapFS{
		"sfx/beep.wav":   {Data: wav},
		"sfx/broken.wav": {Data: []byte("RIFF")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	m := audio.NewMixer(44100)
	snd, err := Sound(m, fs, "sfx/beep.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got := snd.Frames(); got < 4400 || got > 4420 {
		t.Fatalf("frames %d, want about 4410", got)
	}
	if _, err := Sound(m, fs, "sfx/broken.wav"); err == nil || !strings.Contains(err.Error(), "asset sfx/broken.wav:") {
		t.Fatalf("broken sound error %v", err)
	}
	if _, err := Sound(m, fs, "sfx/none.wav"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing sound error %v", err)
	}

	mu, err := Music(m, fs, "sfx/beep.wav", false)
	if err != nil {
		t.Fatal(err)
	}
	mu.Close()
	if _, err := Music(m, fs, "sfx/broken.wav", true); err == nil || !strings.Contains(err.Error(), "asset sfx/broken.wav:") {
		t.Fatalf("broken music error %v", err)
	}
}

func TestTracker(t *testing.T) {
	fs, err := OpenFS(FSSource(fstest.MapFS{
		"music/tune.mod": {Data: buildMOD()},
		"music/bad.mod":  {Data: []byte("nope")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	mod, err := Tracker(fs, "music/tune.mod")
	if err != nil {
		t.Fatal(err)
	}
	if mod == nil {
		t.Fatal("nil module")
	}
	if _, err := Tracker(fs, "music/bad.mod"); err == nil || !strings.Contains(err.Error(), "asset music/bad.mod:") {
		t.Fatalf("bad module error %v", err)
	}
}

func TestParseModelResolvesRelativeURIs(t *testing.T) {
	// A minimal .gltf whose single buffer lives in a sibling file. The
	// resolver must read it through the FS relative to the model's
	// directory, and name the model in any error.
	buffer := make([]byte, 12)
	doc := `{"asset":{"version":"2.0"},"buffers":[{"uri":"tri.bin","byteLength":12}]}`
	fs, err := OpenFS(FSSource(fstest.MapFS{
		"models/tri.gltf":    {Data: []byte(doc)},
		"models/tri.bin":     {Data: buffer},
		"models/orphan.gltf": {Data: []byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"gone.bin","byteLength":12}]}`)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if _, err := parseModel(fs, "models/tri.gltf"); err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = parseModel(fs, "models/orphan.gltf")
	if err == nil || !strings.Contains(err.Error(), "asset models/orphan.gltf:") || !errors.Is(err, ErrNotFound) {
		t.Fatalf("orphan error %v", err)
	}
}

// sceneComponent is what the scene and prefab documents below carry;
// the registered name is what the files hold.
type sceneComponent struct {
	HP   int
	Team string
}

func init() { ecs.Register[sceneComponent]("asset.test") }

func TestSceneAndPrefab(t *testing.T) {
	scene := `{"version":1,"name":"arena","properties":{"wave":2},"entities":[
		{"name":"boss","components":{"asset.test":{"HP":40,"Team":"red"}}},
		{"name":"escort","parent":1,"prefab":"grunt","components":{"asset.test":{"HP":3,"Team":"red"}}}]}`
	prefab := `{"components":{"asset.test":{"HP":1,"Team":"blue"}}}`
	fs, err := OpenFS(FSSource(fstest.MapFS{
		"levels/arena.scene.json": {Data: []byte(scene)},
		"prefabs/grunt.json":      {Data: []byte(prefab)},
		"levels/broken.json":      {Data: []byte(`{"version":9}`)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	doc, err := Scene(fs, "levels/arena.scene.json")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "arena" || doc.Properties["wave"] != float64(2) || len(doc.Entities) != 2 {
		t.Fatalf("scene %+v", doc)
	}
	grunt, err := Prefab(fs, "prefabs/grunt.json")
	if err != nil {
		t.Fatal(err)
	}
	w := ecs.NewWorld()
	w.SetResource(ecs.PrefabLibrary{"grunt": grunt})
	inst, err := w.Instantiate(doc)
	if err != nil {
		t.Fatal(err)
	}
	boss, ok := inst.Entity("boss")
	if !ok {
		t.Fatal("boss not spawned")
	}
	if c, _ := w.Get[sceneComponent](boss); c == nil || c.HP != 40 {
		t.Fatalf("boss %+v", c)
	}
	escort, _ := inst.Entity("escort")
	if p, ok := ecs.ParentOf(w, escort); !ok || p != boss {
		t.Fatal("escort not parented to the boss")
	}
	if c, _ := w.Get[sceneComponent](escort); c == nil || c.HP != 3 || c.Team != "red" {
		t.Fatalf("prefab override %+v", c)
	}

	if _, err := Scene(fs, "levels/broken.json"); err == nil || !strings.Contains(err.Error(), "asset levels/broken.json:") {
		t.Fatalf("broken scene error %v", err)
	}
	_, err = Scene(fs, "levels/missing.json")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "asset levels/missing.json:") {
		t.Fatalf("missing scene error %v", err)
	}
	if _, err := Prefab(fs, "levels/missing.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing prefab error %v", err)
	}
}
