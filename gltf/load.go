package gltf

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoders glTF images use
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/matjam/bunyip/lin"
)

// Resolver fetches the bytes behind a relative URI in a .gltf file.
type Resolver func(uri string) ([]byte, error)

// Load reads a .gltf or .glb file, resolving relative URIs next to it.
func Load(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gltf: %w", err)
	}
	dir := filepath.Dir(path)
	doc, err := Parse(data, func(uri string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, filepath.FromSlash(uri)))
	})
	if err != nil {
		return nil, fmt.Errorf("gltf: %s: %w", path, err)
	}
	return doc, nil
}

// Parse decodes .gltf JSON or a .glb container from memory. resolve may be
// nil when every buffer and image is embedded.
func Parse(data []byte, resolve Resolver) (*Document, error) {
	l := &loader{resolve: resolve}
	jsonData := data
	if len(data) >= 12 && string(data[:4]) == "glTF" {
		var err error
		if jsonData, l.glbBin, err = splitGLB(data); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(jsonData, &l.j); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return l.build()
}

const (
	glbChunkJSON = 0x4E4F534A
	glbChunkBIN  = 0x004E4942
)

func splitGLB(data []byte) (jsonChunk, binChunk []byte, err error) {
	if binary.LittleEndian.Uint32(data[4:]) != 2 {
		return nil, nil, fmt.Errorf("glb version %d is not 2", binary.LittleEndian.Uint32(data[4:]))
	}
	off := 12
	for off+8 <= len(data) {
		length := int(binary.LittleEndian.Uint32(data[off:]))
		kind := binary.LittleEndian.Uint32(data[off+4:])
		off += 8
		if off+length > len(data) {
			return nil, nil, fmt.Errorf("glb chunk overruns file")
		}
		switch kind {
		case glbChunkJSON:
			jsonChunk = data[off : off+length]
		case glbChunkBIN:
			binChunk = data[off : off+length]
		}
		off += length
	}
	if jsonChunk == nil {
		return nil, nil, fmt.Errorf("glb has no JSON chunk")
	}
	return jsonChunk, binChunk, nil
}

type loader struct {
	j       jsonDoc
	resolve Resolver
	glbBin  []byte
	buffers [][]byte
}

func (l *loader) build() (*Document, error) {
	if err := l.loadBuffers(); err != nil {
		return nil, err
	}
	doc := &Document{}
	var err error
	if doc.Images, err = l.loadImages(); err != nil {
		return nil, err
	}
	doc.Materials = l.materials()
	for _, m := range l.j.Meshes {
		mesh, err := l.mesh(m)
		if err != nil {
			return nil, err
		}
		doc.Meshes = append(doc.Meshes, mesh)
	}
	doc.Instances = l.instances()
	return doc, nil
}

func (l *loader) loadBuffers() error {
	for i, b := range l.j.Buffers {
		var data []byte
		switch {
		case b.URI == "":
			if l.glbBin == nil {
				return fmt.Errorf("buffer %d has no uri and this is not a glb", i)
			}
			data = l.glbBin
		default:
			var err error
			if data, err = l.fetch(b.URI); err != nil {
				return fmt.Errorf("buffer %d: %w", i, err)
			}
		}
		if len(data) < b.ByteLength {
			return fmt.Errorf("buffer %d is %d bytes, header says %d", i, len(data), b.ByteLength)
		}
		l.buffers = append(l.buffers, data[:b.ByteLength])
	}
	return nil
}

// fetch decodes a data: URI or asks the resolver.
func (l *loader) fetch(uri string) ([]byte, error) {
	if strings.HasPrefix(uri, "data:") {
		comma := strings.IndexByte(uri, ',')
		if comma < 0 || !strings.HasSuffix(uri[:comma], ";base64") {
			return nil, fmt.Errorf("unsupported data uri")
		}
		return base64.StdEncoding.DecodeString(uri[comma+1:])
	}
	if l.resolve == nil {
		return nil, fmt.Errorf("uri %q needs a resolver", uri)
	}
	return l.resolve(uri)
}

func (l *loader) bufferView(i int) ([]byte, int, error) {
	if i < 0 || i >= len(l.j.BufferViews) {
		return nil, 0, fmt.Errorf("bufferView %d out of range", i)
	}
	v := l.j.BufferViews[i]
	if v.Buffer < 0 || v.Buffer >= len(l.buffers) {
		return nil, 0, fmt.Errorf("bufferView %d: buffer %d out of range", i, v.Buffer)
	}
	buf := l.buffers[v.Buffer]
	if v.ByteOffset+v.ByteLength > len(buf) {
		return nil, 0, fmt.Errorf("bufferView %d overruns buffer", i)
	}
	return buf[v.ByteOffset : v.ByteOffset+v.ByteLength], v.ByteStride, nil
}

func (l *loader) loadImages() ([]image.Image, error) {
	var images []image.Image
	for i, im := range l.j.Images {
		var data []byte
		var err error
		switch {
		case im.BufferView != nil:
			data, _, err = l.bufferView(*im.BufferView)
		case im.URI != "":
			data, err = l.fetch(im.URI)
		default:
			err = fmt.Errorf("no source")
		}
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", i, err)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", i, err)
		}
		images = append(images, img)
	}
	return images, nil
}

func (l *loader) materials() []Material {
	mats := make([]Material, 0, len(l.j.Materials))
	for _, m := range l.j.Materials {
		mat := Material{Name: m.Name, BaseColor: [4]float32{1, 1, 1, 1}, Image: -1}
		if m.PBR != nil {
			if len(m.PBR.BaseColorFactor) == 4 {
				copy(mat.BaseColor[:], m.PBR.BaseColorFactor)
			}
			if t := m.PBR.BaseColorTexture; t != nil && t.Index >= 0 && t.Index < len(l.j.Textures) {
				tex := l.j.Textures[t.Index]
				if tex.Source != nil && *tex.Source >= 0 && *tex.Source < len(l.j.Images) {
					mat.Image = *tex.Source
				}
				if tex.Sampler != nil && *tex.Sampler >= 0 && *tex.Sampler < len(l.j.Samplers) {
					mat.Linear = l.j.Samplers[*tex.Sampler].MagFilter != 9728 // NEAREST
				} else {
					mat.Linear = true
				}
			}
		}
		mats = append(mats, mat)
	}
	return mats
}

// instances walks the default scene and flattens node transforms.
func (l *loader) instances() []Instance {
	var out []Instance
	var walk func(n int, parent lin.Mat4, depth int)
	walk = func(n int, parent lin.Mat4, depth int) {
		if n < 0 || n >= len(l.j.Nodes) || depth > 64 {
			return
		}
		node := l.j.Nodes[n]
		world := parent.Mul(nodeLocal(node))
		if node.Mesh != nil && *node.Mesh >= 0 && *node.Mesh < len(l.j.Meshes) {
			out = append(out, Instance{Name: node.Name, Mesh: *node.Mesh, World: world})
		}
		for _, c := range node.Children {
			walk(c, world, depth+1)
		}
	}
	roots := l.sceneRoots()
	for _, r := range roots {
		walk(r, lin.Identity(), 0)
	}
	return out
}

func (l *loader) sceneRoots() []int {
	if len(l.j.Scenes) > 0 {
		s := 0
		if l.j.Scene != nil && *l.j.Scene >= 0 && *l.j.Scene < len(l.j.Scenes) {
			s = *l.j.Scene
		}
		return l.j.Scenes[s].Nodes
	}
	// No scenes: treat every node that is nobody's child as a root.
	child := map[int]bool{}
	for _, n := range l.j.Nodes {
		for _, c := range n.Children {
			child[c] = true
		}
	}
	var roots []int
	for i := range l.j.Nodes {
		if !child[i] {
			roots = append(roots, i)
		}
	}
	return roots
}

func nodeLocal(n jsonNode) lin.Mat4 {
	if len(n.Matrix) == 16 {
		var m lin.Mat4
		copy(m[:], n.Matrix) // glTF matrices are column-major, as lin's are
		return m
	}
	t, r, s := lin.Vec3{}, lin.QuatIdentity(), lin.V3(1, 1, 1)
	if len(n.Translation) == 3 {
		t = lin.V3(n.Translation[0], n.Translation[1], n.Translation[2])
	}
	if len(n.Rotation) == 4 {
		r = lin.Quat{X: n.Rotation[0], Y: n.Rotation[1], Z: n.Rotation[2], W: n.Rotation[3]}
	}
	if len(n.Scale) == 3 {
		s = lin.V3(n.Scale[0], n.Scale[1], n.Scale[2])
	}
	return lin.TRS(t, r, s)
}
