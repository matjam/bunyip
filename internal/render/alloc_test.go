package render

import (
	"log/slog"
	"os"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
)

// TestAllocatorStress creates and frees thousands of buffers and images and
// checks that they share a few blocks and that freed memory is reused.
func TestAllocatorStress(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "alloc_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 16, Height: 16}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	d := r.Device
	base := d.Stats()
	var bufs []*Buffer
	var imgs []*Image
	for i := range 3000 {
		b, err := d.NewBuffer(vk.VkDeviceSize(256+i%2048), vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			t.Fatalf("buffer %d: %v", i, err)
		}
		if err := b.Write(0, []byte{1, 2, 3, 4}); err != nil {
			t.Fatal(err)
		}
		bufs = append(bufs, b)
		if i%10 == 0 {
			img, err := d.NewImage(vk.VkExtent2D{Width: 64, Height: 64}, vk.VK_FORMAT_R8G8B8A8_UNORM,
				vk.VK_IMAGE_USAGE_SAMPLED_BIT|vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT, vk.VK_IMAGE_ASPECT_COLOR_BIT)
			if err != nil {
				t.Fatalf("image %d: %v", i, err)
			}
			imgs = append(imgs, img)
		}
	}
	st := d.Stats()
	t.Logf("after 3000 buffers and 300 images: %+v", st)
	if st.Blocks-base.Blocks > 6 {
		t.Errorf("used %d blocks; sub-allocation is not sharing memory", st.Blocks-base.Blocks)
	}
	for _, b := range bufs {
		b.Destroy()
	}
	for _, img := range imgs {
		imgs = imgs[1:]
		img.Destroy()
	}
	after := d.Stats()
	if after.Live != base.Live || after.Used != base.Used {
		t.Errorf("leak: live %d used %d (base live %d used %d)", after.Live, after.Used, base.Live, base.Used)
	}
	// A mip-mapped texture builds its chain without complaint.
	pix := make([]byte, 256*256*4)
	tex, err := d.NewTextureImage(vk.VkExtent2D{Width: 256, Height: 256}, vk.VK_FORMAT_R8G8B8A8_SRGB, pix, true)
	if err != nil {
		t.Fatalf("mipmapped texture: %v", err)
	}
	if tex.Mips != 9 {
		t.Errorf("mips = %d, want 9", tex.Mips)
	}
	tex.Destroy()
}
