package gfx

import (
	"image"
	"slices"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

type ownedTestResource struct {
	name  string
	owner *resourceOwner
	order *[]string
	child *ownedTestResource
}

func (r *ownedTestResource) Destroy() {
	r.owner.remove(r)
	*r.order = append(*r.order, r.name)
	if r.child != nil {
		r.child.Destroy()
	}
}

func TestResourceOwnerReverseOrder(t *testing.T) {
	var owner resourceOwner
	var order []string
	a := &ownedTestResource{name: "first", owner: &owner, order: &order}
	b := &ownedTestResource{name: "child", owner: &owner, order: &order}
	c := &ownedTestResource{name: "parent", owner: &owner, order: &order, child: b}
	for _, r := range []*ownedTestResource{a, b, c, a} {
		owner.add(r)
	}
	owner.destroy()
	owner.destroy()
	if !slices.Equal(order, []string{"parent", "child", "first"}) {
		t.Fatalf("destruction order %v", order)
	}
	if len(owner.live) != 0 || owner.order.Len() != 0 {
		t.Fatal("destroyed resources remain retained")
	}
}

// Exercise shutdown with resources the game never explicitly released,
// including a setup failure and a Draw that never reaches frame submission.
func TestGraphicsOwnsResources(t *testing.T) {
	for _, phase := range []string{"setup_failure", "submitted", "draw_failure"} {
		t.Run(phase, func(t *testing.T) {
			previous := newHeadless(t, 64, 64)
			beginMorphRetireFrame(t, previous)
			endMorphRetireFrame(t, previous)
			previous.destroy()
			baseline := previous.r.Device.Stats()
			g, err := newGraphics(previous.r)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(g.destroy)
			tex, err := g.NewBlankTexture(4, 4, TextureOptions{})
			if err != nil {
				t.Fatal(err)
			}
			font, err := g.NewFont(goregular.TTF, 14, FontOptions{})
			if err != nil {
				t.Fatal(err)
			}
			atlas := font.Texture()
			if _, listed := g.res.live[atlas]; listed {
				t.Fatal("font atlas is double-counted in Resources")
			}
			model, err := g.LoadModel(morphGridDoc(1))
			if err != nil {
				t.Fatal(err)
			}
			morph := model.morphBuf.buf
			shader, err := g.NewShader(shaders.SpriteFrag)
			if err != nil {
				t.Fatal(err)
			}
			if err := shader.Reload(shaders.SpriteFrag); err != nil {
				t.Fatal(err)
			}
			rt, err := g.NewRenderTexture(16, 16)
			if err != nil {
				t.Fatal(err)
			}
			gradient, err := g.NewGradient(GradientStop{Color: White})
			if err != nil {
				t.Fatal(err)
			}
			if phase == "setup_failure" {
				if _, err := g.NewFont([]byte("invalid font"), 14, FontOptions{}); err == nil {
					t.Fatal("expected setup failure")
				}
			} else {
				beginMorphRetireFrame(t, g)
				if err := tex.Replace(image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
					t.Fatal(err)
				}
				g.DrawTo(rt, Black, func() { g.DrawTexture(tex, 0, 0) })
				g.Shaded(shader, func() { g.DrawTexture(rt.Texture(), 0, 0) })
				g.DrawText(font, "owned", 0, 20, White)
				g.DrawModel(model, lin.Identity())
				// Explicit composite destruction and replacement both add
				// work to the retire ring before automatic cleanup starts.
				model.Destroy()
				model.Destroy()
				if phase == "submitted" {
					endMorphRetireFrame(t, g)
				}
			}
			g.destroy()
			if got := g.r.Device.Stats(); got.Live != baseline.Live || got.Used != baseline.Used {
				t.Errorf("GPU allocations after cleanup %+v; renderer baseline %+v", got, baseline)
			}
			if len(g.owned.live) != 0 || g.owned.order.Len() != 0 || len(g.Resources()) != 0 {
				t.Fatal("cleanup retained resources")
			}
			if tex.img != nil || atlas.img != nil || gradient.tex.img != nil || font.atlas != nil || rt.target != nil || shader.pipes != nil || morph.Handle != 0 {
				t.Fatal("a resource retained its GPU storage")
			}
			// Early release and automatic release have the same idempotence.
			tex.Destroy()
			font.Destroy()
			model.Destroy()
			shader.Destroy()
			rt.Destroy()
			gradient.Destroy()
		})
	}
}

func TestFailedShaderReleasesBuiltPipelines(t *testing.T) {
	g := newHeadless(t, 32, 32)
	create, destroy := vk.VkCreateGraphicsPipelines, vk.VkDestroyPipeline
	t.Cleanup(func() { vk.VkCreateGraphicsPipelines, vk.VkDestroyPipeline = create, destroy })
	var calls, freed int
	vk.VkCreateGraphicsPipelines = func(device vk.VkDevice, cache vk.VkPipelineCache, count uint32, info *vk.VkGraphicsPipelineCreateInfo, alloc *vk.VkAllocationCallbacks, out *vk.VkPipeline) vk.VkResult {
		calls++
		if calls == 2 {
			return vk.VK_ERROR_OUT_OF_DEVICE_MEMORY
		}
		return create(device, cache, count, info, alloc, out)
	}
	vk.VkDestroyPipeline = func(device vk.VkDevice, pipeline vk.VkPipeline, alloc *vk.VkAllocationCallbacks) {
		freed++
		destroy(device, pipeline, alloc)
	}
	if _, err := g.NewMeshShader(shaders.PBRFrag); err == nil {
		t.Fatal("expected second pipeline creation to fail")
	}
	if calls != 2 || freed != 1 {
		t.Fatalf("created %d pipeline variants, freed %d; want 2 attempts and 1 release", calls, freed)
	}
}

func TestFailedGraphicsSetupReleasesResources(t *testing.T) {
	for _, phase := range []string{"sampler", "pipeline"} {
		t.Run(phase, func(t *testing.T) {
			previous := newHeadless(t, 32, 32)
			previous.destroy()
			r := previous.r
			baseline := r.Device.Stats()
			createSampler, createPipeline := vk.VkCreateSampler, vk.VkCreateGraphicsPipelines
			t.Cleanup(func() {
				vk.VkCreateSampler, vk.VkCreateGraphicsPipelines = createSampler, createPipeline
			})
			calls := 0
			if phase == "sampler" {
				vk.VkCreateSampler = func(device vk.VkDevice, info *vk.VkSamplerCreateInfo, alloc *vk.VkAllocationCallbacks, out *vk.VkSampler) vk.VkResult {
					calls++
					if calls == 2 {
						return vk.VK_ERROR_OUT_OF_DEVICE_MEMORY
					}
					return createSampler(device, info, alloc, out)
				}
			} else {
				vk.VkCreateGraphicsPipelines = func(device vk.VkDevice, cache vk.VkPipelineCache, count uint32, info *vk.VkGraphicsPipelineCreateInfo, alloc *vk.VkAllocationCallbacks, out *vk.VkPipeline) vk.VkResult {
					calls++
					if calls == 2 {
						return vk.VK_ERROR_OUT_OF_DEVICE_MEMORY
					}
					return createPipeline(device, cache, count, info, alloc, out)
				}
			}
			if g, err := newGraphics(r); err == nil {
				g.destroy()
				t.Fatal("expected renderer setup failure")
			}
			if calls != 2 {
				t.Fatalf("failure was reached after %d calls, want 2", calls)
			}
			if got := r.Device.Stats(); got.Live != baseline.Live || got.Used != baseline.Used {
				t.Errorf("failed setup left GPU allocations %+v; baseline %+v", got, baseline)
			}
		})
	}
}
