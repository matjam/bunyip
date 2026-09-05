package gltf

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/matjam/bunyip/lin"
)

var componentSizes = map[int]int{5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}

var typeCounts = map[string]int{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4, "MAT4": 16}

// maxAccessorFloats bounds an accessor with no buffer view, which the
// specification reads as all zeros and would otherwise be sized by the
// file alone.
const maxAccessorFloats = 64 << 20

// floats reads an accessor as float32 components, converting integer
// types (normalised or not) the way the specification says. A sparse
// accessor's replacements are applied over its base data, or over zeros
// when it has no buffer view.
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
	if a.Count < 0 || a.ByteOffset < 0 || a.Count > 1<<28 {
		return nil, 0, fmt.Errorf("accessor %d: bad count %d or offset %d", index, a.Count, a.ByteOffset)
	}
	var out []float32
	if a.BufferView == nil {
		// All zeros, per spec; the count is bounded so a file cannot ask
		// for gigabytes of nothing.
		if a.Count > maxAccessorFloats/n {
			return nil, 0, fmt.Errorf("accessor %d: too many values without a buffer view", index)
		}
		out = make([]float32, a.Count*n)
	} else {
		data, stride, err := l.bufferView(*a.BufferView)
		if err != nil {
			return nil, 0, fmt.Errorf("accessor %d: %w", index, err)
		}
		if stride == 0 {
			stride = n * size
		}
		// The overrun check runs before the output is allocated, so the
		// buffer bounds the memory a file can claim.
		if !accessorFits(len(data), a.ByteOffset, a.Count, stride, n*size) {
			return nil, 0, fmt.Errorf("accessor %d overruns its buffer view", index)
		}
		out = make([]float32, a.Count*n)
		for i := range a.Count {
			base := a.ByteOffset + i*stride
			for c := range n {
				out[i*n+c] = readComponent(data[base+c*size:], a.ComponentType, a.Normalized)
			}
		}
	}
	if err := l.applySparse(index, a, n, size, func(elem int, vals []float32) {
		copy(out[elem*n:(elem+1)*n], vals)
	}); err != nil {
		return nil, 0, err
	}
	return out, n, nil
}

// accessorFits checks the final element without adding or multiplying file
// offsets, counts or strides, which could overflow before the bounds check.
func accessorFits(length, offset, count, stride, elementSize int) bool {
	if offset < 0 || offset > length || count < 0 || stride < elementSize {
		return false
	}
	if count == 0 {
		return true
	}
	remaining := length - offset
	return elementSize <= remaining && count-1 <= (remaining-elementSize)/stride
}

// sparseIndexSizes is what an element index in a sparse accessor may be
// stored as: unsigned byte, short or int.
var sparseIndexSizes = map[int]int{5121: 1, 5123: 2, 5125: 4}

// applySparse reads an accessor's sparse block, which replaces some of
// its elements with values from another buffer view, and hands each
// replacement to set as an element index and that element's components.
// An accessor with no sparse block does nothing. Blender writes morph
// targets this way, since a blend shape usually moves few vertices.
func (l *loader) applySparse(index int, a jsonAccessor, n, size int, set func(elem int, vals []float32)) error {
	s := a.Sparse
	if s == nil || s.Count == 0 {
		return nil
	}
	if s.Count < 0 || s.Count > a.Count {
		return fmt.Errorf("accessor %d: sparse count %d is not within the accessor's %d", index, s.Count, a.Count)
	}
	isize := sparseIndexSizes[s.Indices.ComponentType]
	if isize == 0 {
		return fmt.Errorf("accessor %d: sparse indices of component type %d", index, s.Indices.ComponentType)
	}
	// Sparse index and value views are tightly packed: the specification
	// forbids a byte stride on them.
	idxData, _, err := l.bufferView(s.Indices.BufferView)
	if err != nil {
		return fmt.Errorf("accessor %d: sparse indices: %w", index, err)
	}
	valData, _, err := l.bufferView(s.Values.BufferView)
	if err != nil {
		return fmt.Errorf("accessor %d: sparse values: %w", index, err)
	}
	if s.Indices.ByteOffset < 0 || s.Values.ByteOffset < 0 {
		return fmt.Errorf("accessor %d: negative sparse offset", index)
	}
	if !accessorFits(len(idxData), s.Indices.ByteOffset, s.Count, isize, isize) {
		return fmt.Errorf("accessor %d: sparse indices overrun their buffer view", index)
	}
	if !accessorFits(len(valData), s.Values.ByteOffset, s.Count, n*size, n*size) {
		return fmt.Errorf("accessor %d: sparse values overrun their buffer view", index)
	}
	vals := make([]float32, n)
	for i := range s.Count {
		b := idxData[s.Indices.ByteOffset+i*isize:]
		var elem int
		switch isize {
		case 1:
			elem = int(b[0])
		case 2:
			elem = int(binary.LittleEndian.Uint16(b))
		default:
			elem = int(binary.LittleEndian.Uint32(b))
		}
		if elem < 0 || elem >= a.Count {
			return fmt.Errorf("accessor %d: sparse index %d is outside its %d elements", index, elem, a.Count)
		}
		base := s.Values.ByteOffset + i*n*size
		for c := range n {
			vals[c] = readComponent(valData[base+c*size:], a.ComponentType, a.Normalized)
		}
		set(elem, vals)
	}
	return nil
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

// indices reads an index accessor of any integer width, applying a
// sparse block over the base data when the accessor has one.
func (l *loader) indices(index int) ([]uint32, error) {
	if index < 0 || index >= len(l.j.Accessors) {
		return nil, fmt.Errorf("accessor %d out of range", index)
	}
	a := l.j.Accessors[index]
	size := componentSizes[a.ComponentType]
	if a.Type != "SCALAR" || size == 0 || a.ComponentType == 5126 {
		return nil, fmt.Errorf("index accessor %d: unsupported type %s/%d", index, a.Type, a.ComponentType)
	}
	if a.Count < 0 || a.ByteOffset < 0 || a.Count > 1<<28 {
		return nil, fmt.Errorf("index accessor %d: bad count %d or offset %d", index, a.Count, a.ByteOffset)
	}
	var out []uint32
	if a.BufferView == nil {
		if a.Sparse == nil {
			return nil, fmt.Errorf("index accessor %d has no data", index)
		}
		if a.Count > maxAccessorFloats {
			return nil, fmt.Errorf("index accessor %d: too many values without a buffer view", index)
		}
		out = make([]uint32, a.Count)
	} else {
		data, stride, err := l.bufferView(*a.BufferView)
		if err != nil {
			return nil, err
		}
		if stride == 0 {
			stride = size
		}
		if !accessorFits(len(data), a.ByteOffset, a.Count, stride, size) {
			return nil, fmt.Errorf("index accessor %d overruns its buffer view", index)
		}
		out = make([]uint32, a.Count)
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
	}
	if err := l.applySparse(index, a, 1, size, func(elem int, vals []float32) {
		out[elem] = uint32(vals[0])
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (l *loader) mesh(m jsonMesh) (Mesh, error) {
	mesh := Mesh{Name: m.Name, Weights: m.Weights, TargetNames: m.Extras.TargetNames}
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
	if uvIdx, ok := p.Attributes["TEXCOORD_1"]; ok {
		uv, n, err := l.floats(uvIdx)
		if err != nil || n != 2 || len(uv) != count*2 {
			return prim, fmt.Errorf("TEXCOORD_1: %v", err)
		}
		prim.UVs2 = make([]lin.Vec2, count)
		for i := range prim.UVs2 {
			prim.UVs2[i] = lin.V2(uv[i*2], uv[i*2+1])
		}
	}
	if cIdx, ok := p.Attributes["COLOR_0"]; ok {
		c, n, err := l.floats(cIdx)
		if err != nil || (n != 3 && n != 4) || len(c) != count*n {
			return prim, fmt.Errorf("COLOR_0: %v", err)
		}
		prim.Colors = make([]lin.Vec4, count)
		for i := range prim.Colors {
			a := float32(1)
			if n == 4 {
				a = c[i*4+3]
			}
			prim.Colors[i] = lin.V4(c[i*n], c[i*n+1], c[i*n+2], a)
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
	for ti, target := range p.Targets {
		mt, err := l.morphTarget(target, count)
		if err != nil {
			return prim, fmt.Errorf("target %d: %w", ti, err)
		}
		prim.Targets = append(prim.Targets, mt)
	}
	if p.Material != nil && *p.Material >= 0 && *p.Material < len(l.j.Materials) {
		prim.Material = *p.Material
	}
	return prim, nil
}

// morphTarget reads one primitive target's position and normal deltas.
func (l *loader) morphTarget(attrs map[string]int, count int) (MorphTarget, error) {
	var mt MorphTarget
	read := func(name string) ([]lin.Vec3, error) {
		idx, ok := attrs[name]
		if !ok {
			return nil, nil
		}
		v, n, err := l.floats(idx)
		if err != nil || n != 3 || len(v) != count*3 {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		out := make([]lin.Vec3, count)
		for i := range out {
			out[i] = lin.V3(v[i*3], v[i*3+1], v[i*3+2])
		}
		return out, nil
	}
	var err error
	if mt.Positions, err = read("POSITION"); err != nil {
		return mt, err
	}
	if mt.Positions == nil {
		mt.Positions = make([]lin.Vec3, count)
	}
	mt.Normals, err = read("NORMAL")
	return mt, err
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
