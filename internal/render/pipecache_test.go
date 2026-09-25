package render

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/bunyip/internal/render/shaders"
	"github.com/matjam/bunyip/internal/vk"
)

// cacheRenderer is a headless renderer whose pipeline cache file lives in
// dir.
func cacheRenderer(t *testing.T, dir string) *Renderer {
	t.Helper()
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "pipecache_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil)), PipelineCacheDir: dir}
	r, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 16, Height: 16}, true)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return r
}

// cacheFiles lists the pipeline cache files in dir.
func cacheFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "pipelines-*.vkcache"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestPipelineCacheFile builds a pipeline, checks the cache reaches its
// file when the device goes, that the next device loads it, that a
// damaged file is ignored rather than trusted, and that another key gets
// a file of its own and removes the old one.
func TestPipelineCacheFile(t *testing.T) {
	dir := t.TempDir()
	desc := PipelineDesc{Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag, ColorFormat: vk.VK_FORMAT_R8G8B8A8_UNORM}
	build := func(key string) *Renderer {
		r := cacheRenderer(t, dir)
		if err := r.OpenPipelineCache([]byte(key)); err != nil {
			t.Fatalf("OpenPipelineCache: %v", err)
		}
		p, err := r.Device.NewPipeline(desc)
		if err != nil {
			t.Fatalf("NewPipeline: %v", err)
		}
		p.Destroy()
		return r
	}
	r := build("one")
	if r.Device.pipes.loaded != 0 {
		t.Fatal("a first run loaded a cache it cannot have")
	}
	r.Destroy()
	files := cacheFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("cache files after the first run: %v", files)
	}
	r = build("one")
	if r.Device.pipes.loaded == 0 {
		t.Fatal("the second run did not load the first run's cache")
	}
	r.Destroy()

	// Flip a byte in the driver's data: the checksum refuses it.
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if err := os.WriteFile(files[0], raw, 0o644); err != nil {
		t.Fatal(err)
	}
	r = build("one")
	if r.Device.pipes.loaded != 0 {
		t.Fatal("a damaged cache file was loaded")
	}
	r.Destroy()
	// Cut the file short: refused as well, and rewritten whole.
	if err := os.WriteFile(files[0], raw[:10], 0o644); err != nil {
		t.Fatal(err)
	}
	r = build("one")
	if r.Device.pipes.loaded != 0 {
		t.Fatal("a truncated cache file was loaded")
	}
	r.Destroy()
	if data, err := os.ReadFile(files[0]); err != nil {
		t.Fatal(err)
	} else if _, err := unwrapPipelineCache(data); err != nil {
		t.Fatalf("the rewritten cache file is not whole: %v", err)
	}

	r = build("two")
	r.Destroy()
	if got := cacheFiles(t, dir); len(got) != 1 || got[0] == files[0] {
		t.Fatalf("cache files after a new key: %v, want one other than %v", got, files)
	}
}

// TestPipelineCacheHeader checks the device check on the driver's header:
// data from another device or driver is refused before the driver sees it.
func TestPipelineCacheHeader(t *testing.T) {
	r := cacheRenderer(t, "")
	defer r.Destroy()
	d := r.Device
	if err := d.OpenPipelineCache(PipelineCacheOptions{}); err != nil {
		t.Fatal(err)
	}
	p, err := d.NewPipeline(PipelineDesc{Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag, ColorFormat: vk.VK_FORMAT_R8G8B8A8_UNORM})
	if err != nil {
		t.Fatal(err)
	}
	p.Destroy()
	d.pipes.path = "unused" // snapshot only copies the data out
	d.pipes.built = 1
	data, _ := d.snapshotPipelineCache()
	if err := d.checkPipelineCacheHeader(data); err != nil {
		t.Fatalf("the device's own cache fails the check: %v", err)
	}
	other := append([]byte(nil), data...)
	other[16] ^= 1 // the driver's cache identifier
	if err := d.checkPipelineCacheHeader(other); err == nil {
		t.Fatal("a cache from another driver passed the check")
	}
	other = append([]byte(nil), data...)
	other[8] ^= 1 // the vendor
	if err := d.checkPipelineCacheHeader(other); err == nil {
		t.Fatal("a cache from another device passed the check")
	}
	if err := d.checkPipelineCacheHeader(data[:20]); err == nil {
		t.Fatal("a short cache passed the check")
	}
}

// TestPipelineBatch builds pipelines on workers and checks that each is
// built by Wait, that a program that cannot build reports its error
// through Wait, and that modules are shared between pipelines built from
// one program.
func TestPipelineBatch(t *testing.T) {
	r := cacheRenderer(t, "")
	defer r.Destroy()
	d := r.Device
	b := d.BeginPipelineBatch()
	var pipes []*Pipeline
	for _, f := range []vk.VkFormat{vk.VK_FORMAT_R8G8B8A8_UNORM, vk.VK_FORMAT_B8G8R8A8_UNORM, vk.VK_FORMAT_R16G16B16A16_SFLOAT, vk.VK_FORMAT_R8G8B8A8_SRGB} {
		p, err := d.NewPipeline(PipelineDesc{Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag, ColorFormat: f})
		if err != nil {
			t.Fatal(err)
		}
		if p.Layout == 0 {
			t.Fatal("a batched pipeline has no layout")
		}
		pipes = append(pipes, p)
	}
	d.EndPipelineBatch()
	if err := b.Wait(); err != nil {
		t.Fatalf("batch: %v", err)
	}
	for _, p := range pipes {
		if !p.Ready() || p.Handle == 0 {
			t.Fatal("a batched pipeline was not built by Wait")
		}
	}
	if n := len(d.pipes.modules); n != 2 {
		t.Fatalf("%d shader modules for one vertex and one fragment program", n)
	}
	for _, p := range pipes {
		p.Destroy()
	}
	if n := len(d.pipes.modules); n != 0 {
		t.Fatalf("%d shader modules left after every pipeline was destroyed", n)
	}
	bad, err := d.StartPipeline(PipelineDesc{Vert: shaders.TriangleVert, Frag: []byte{1, 2, 3}, ColorFormat: vk.VK_FORMAT_R8G8B8A8_UNORM})
	if err != nil {
		t.Fatal(err)
	}
	if err := bad.Wait(); err == nil {
		t.Fatal("a pipeline with a broken program built")
	}
	bad.Destroy()
}
