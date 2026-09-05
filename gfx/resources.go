package gfx

import "sort"

// ResourceKind names one kind of GPU resource in a Resources snapshot.
type ResourceKind uint8

const (
	ResourceTexture ResourceKind = iota
	ResourceMesh
	ResourceModel
	ResourceFont
	ResourceRenderTexture
	ResourceEnvironment
)

// String names the kind in lower case, for a debug listing.
func (k ResourceKind) String() string {
	switch k {
	case ResourceTexture:
		return "texture"
	case ResourceMesh:
		return "mesh"
	case ResourceModel:
		return "model"
	case ResourceFont:
		return "font"
	case ResourceRenderTexture:
		return "render texture"
	case ResourceEnvironment:
		return "environment"
	}
	return "resource"
}

// Resource describes one live GPU resource: what it is, how big it is
// and roughly how much GPU memory it holds. Only the fields that suit
// the kind are filled in.
type Resource struct {
	Kind ResourceKind
	// Width and Height are texels, for textures, render textures, font
	// atlases and an environment's cube face.
	Width, Height int
	// Vertices and Indices are a mesh's counts.
	Vertices, Indices int
	// Parts is a model's primitive count.
	Parts int
	// Bytes is an estimate of the GPU memory the resource holds. It
	// counts the images and buffers the resource owns, so a model reads
	// as zero: its meshes and textures are listed on their own.
	Bytes int
}

// resEntry is one tracked resource and the order it was created in, so a
// snapshot lists resources in creation order.
type resEntry struct {
	res Resource
	seq int
}

// resources tracks the resources a Graphics has created and not
// destroyed, for the debug console's graphics panel. A resource is
// keyed by its pointer, so replacing an entry (a mesh whose geometry was
// updated) keeps its place in the list.
type resources struct {
	live map[any]*resEntry
	next int
}

// track records a resource, or updates the one already recorded.
func (g *Graphics) track(key gpuResource, r Resource) {
	g.owned.add(key)
	if g.res.live == nil {
		g.res.live = map[any]*resEntry{}
	}
	if e, ok := g.res.live[key]; ok {
		e.res = r
		return
	}
	g.res.live[key] = &resEntry{res: r, seq: g.res.next}
	g.res.next++
}

// forget drops a destroyed resource; forgetting one twice is harmless.
func (g *Graphics) forget(key any) {
	if g.res.live != nil {
		delete(g.res.live, key)
	}
}

// Resources lists the GPU resources this context has created and not
// destroyed, oldest first: what a debug view shows to find a leak or a
// texture nobody meant to load. A font's atlas, a render texture's
// image and a model's meshes are counted once each, under their own
// kind. The byte figures are estimates of the images and buffers alone,
// with no allowance for alignment or driver overhead.
func (g *Graphics) Resources() []Resource {
	out := make([]Resource, 0, len(g.res.live))
	entries := make([]*resEntry, 0, len(g.res.live))
	for _, e := range g.res.live {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].seq < entries[j].seq })
	for _, e := range entries {
		out = append(out, e.res)
	}
	return out
}

// textureBytes estimates a texture's GPU memory: four bytes a texel,
// with a third again for a full mip chain.
func textureBytes(w, h int, mips bool) int {
	n := w * h * 4
	if mips {
		n += n / 3
	}
	return n
}
