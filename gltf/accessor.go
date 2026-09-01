package gltf

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/matjam/bunyip/lin"
)

var componentSizes = map[int]int{5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}

var typeCounts = map[string]int{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4, "MAT4": 16}

// floats reads an accessor as float32 components, converting integer
// types (normalised or not) the way the specification says.
func (l *loader) floats(index int) ([]float32, int, error) {
	if index < 0 || index >= len(l.j.Accessors) {
		return nil, 0, fmt.Errorf("accessor %d out of range", index)
	}
	a := l.j.Accessors[index]
	n := typeCounts[a.Type]
	size := componentSizes[a.ComponentType]
	if n == 0 || size == 0 {
		return nil, 0, fmt.Errorf("accessor %d: unsupported type %s/%d", index, a.Type, a.ComponentType)
	}
	out := make([]float32, a.Count*n)
	if a.BufferView == nil {
		return out, n, nil // all zeros, per spec
	}
	data, stride, err := l.bufferView(*a.BufferView)
	if err != nil {
		return nil, 0, fmt.Errorf("accessor %d: %w", index, err)
	}
	if stride == 0 {
		stride = n * size
	}
	if a.ByteOffset+(a.Count-1)*stride+n*size > len(data) && a.Count > 0 {
		return nil, 0, fmt.Errorf("accessor %d overruns its buffer view", index)
	}
	for i := range a.Count {
		base := a.ByteOffset + i*stride
		for c := range n {
			out[i*n+c] = readComponent(data[base+c*size:], a.ComponentType, a.Normalized)
		}
	}
	return out, n, nil
}

func readComponent(b []byte, ctype int, normalized bool) float32 {
	switch ctype {
	case 5126:
		return math.Float32frombits(binary.LittleEndian.Uint32(b))
	case 5120:
		v := float32(int8(b[0]))
		if normalized {
			return max(v/127, -1)
		}
		return v
	case 5121:
		v := float32(b[0])
		if normalized {
			return v / 255
		}
		return v
	case 5122:
		v := float32(int16(binary.LittleEndian.Uint16(b)))
		if normalized {
			return max(v/32767, -1)
		}
		return v
	case 5123:
		v := float32(binary.LittleEndian.Uint16(b))
		if normalized {
			return v / 65535
		}
		return v
	case 5125:
		return float32(binary.LittleEndian.Uint32(b))
	}
	return 0
}

// indices reads an index accessor of any integer width.
func (l *loader) indices(index int) ([]uint32, error) {
	if index < 0 || index >= len(l.j.Accessors) {
		return nil, fmt.Errorf("accessor %d out of range", index)
	}
	a := l.j.Accessors[index]
	size := componentSizes[a.ComponentType]
	if a.Type != "SCALAR" || size == 0 || a.ComponentType == 5126 {
		return nil, fmt.Errorf("index accessor %d: unsupported type %s/%d", index, a.Type, a.ComponentType)
	}
	if a.BufferView == nil {
		return nil, fmt.Errorf("index accessor %d has no data", index)
	}
	data, stride, err := l.bufferView(*a.BufferView)
	if err != nil {
		return nil, err
	}
	if stride == 0 {
		stride = size
	}
	out := make([]uint32, a.Count)
	for i := range a.Count {
		b := data[a.ByteOffset+i*stride:]
		switch size {
		case 1:
			out[i] = uint32(b[0])
		case 2:
			out[i] = uint32(binary.LittleEndian.Uint16(b))
		case 4:
			out[i] = binary.LittleEndian.Uint32(b)
		}
	}
	return out, nil
}

func (l *loader) mesh(m jsonMesh) (Mesh, error) {
	mesh := Mesh{Name: m.Name}
	for pi, p := range m.Primitives {
		if p.Mode != nil && *p.Mode != 4 {
			continue // only triangle lists; points, lines and strips are skipped
		}
		prim, err := l.primitive(p)
		if err != nil {
			return mesh, fmt.Errorf("mesh %q primitive %d: %w", m.Name, pi, err)
		}
		mesh.Primitives = append(mesh.Primitives, prim)
	}
	return mesh, nil
}

func (l *loader) primitive(p jsonPrimitive) (Primitive, error) {
	prim := Primitive{Material: -1}
	posIdx, ok := p.Attributes["POSITION"]
	if !ok {
		return prim, fmt.Errorf("no POSITION attribute")
	}
	pos, n, err := l.floats(posIdx)
	if err != nil || n != 3 {
		return prim, fmt.Errorf("POSITION: %v (components %d)", err, n)
	}
	prim.Positions = make([]lin.Vec3, len(pos)/3)
	for i := range prim.Positions {
		prim.Positions[i] = lin.V3(pos[i*3], pos[i*3+1], pos[i*3+2])
	}
	count := len(prim.Positions)
	if p.Indices != nil {
		if prim.Indices, err = l.indices(*p.Indices); err != nil {
			return prim, err
		}
		for _, i := range prim.Indices {
			if int(i) >= count {
				return prim, fmt.Errorf("index %d out of range for %d vertices", i, count)
			}
		}
	} else {
		prim.Indices = make([]uint32, count)
		for i := range prim.Indices {
			prim.Indices[i] = uint32(i)
		}
	}
	prim.UVs = make([]lin.Vec2, count)
	if uvIdx, ok := p.Attributes["TEXCOORD_0"]; ok {
		uv, n, err := l.floats(uvIdx)
		if err != nil || n != 2 || len(uv) != count*2 {
			return prim, fmt.Errorf("TEXCOORD_0: %v", err)
		}
		for i := range prim.UVs {
			prim.UVs[i] = lin.V2(uv[i*2], uv[i*2+1])
		}
	}
	if nIdx, ok := p.Attributes["NORMAL"]; ok {
		nm, n, err := l.floats(nIdx)
		if err != nil || n != 3 || len(nm) != count*3 {
			return prim, fmt.Errorf("NORMAL: %v", err)
		}
		prim.Normals = make([]lin.Vec3, count)
		for i := range prim.Normals {
			prim.Normals[i] = lin.V3(nm[i*3], nm[i*3+1], nm[i*3+2])
		}
	} else {
		prim.Normals = smoothNormals(prim.Positions, prim.Indices)
	}
	if jIdx, ok := p.Attributes["JOINTS_0"]; ok {
		if wIdx, ok := p.Attributes["WEIGHTS_0"]; ok {
			joints, jn, err := l.floats(jIdx)
			weights, wn, err2 := l.floats(wIdx)
			if err == nil && err2 == nil && jn == 4 && wn == 4 && len(joints) == count*4 && len(weights) == count*4 {
				prim.Joints = make([][4]uint8, count)
				prim.Weights = make([][4]float32, count)
				for i := range count {
					for k := range 4 {
						prim.Joints[i][k] = uint8(joints[i*4+k])
						prim.Weights[i][k] = weights[i*4+k]
					}
				}
			}
		}
	}
	if p.Material != nil && *p.Material >= 0 && *p.Material < len(l.j.Materials) {
		prim.Material = *p.Material
	}
	return prim, nil
}

// smoothNormals averages face normals at shared vertices.
func smoothNormals(pos []lin.Vec3, idx []uint32) []lin.Vec3 {
	normals := make([]lin.Vec3, len(pos))
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := pos[idx[i]], pos[idx[i+1]], pos[idx[i+2]]
		n := b.Sub(a).Cross(c.Sub(a))
		normals[idx[i]] = normals[idx[i]].Add(n)
		normals[idx[i+1]] = normals[idx[i+1]].Add(n)
		normals[idx[i+2]] = normals[idx[i+2]].Add(n)
	}
	for i := range normals {
		normals[i] = normals[i].Norm()
	}
	return normals
}
