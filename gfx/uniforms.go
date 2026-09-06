package gfx

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"sync"

	"github.com/matjam/bunyip/lin"
)

// A uniformPlan describes GLSL extended alignment (std140), independently
// of Go offsets. Arrays share one element plan, so large array types cannot
// cause proportional plan allocations before the block limit is checked.
type uniformPlan struct {
	size, align int
	vector      int
	matrix      int
	stride      int
	element     *uniformPlan
	fields      []uniformField
}

type uniformField struct {
	index, offset int
	plan          *uniformPlan
}

type uniformPlanResult struct {
	plan *uniformPlan
	err  error
}

var uniformPlans sync.Map // reflect.Type -> uniformPlanResult

func cachedUniformPlan(t reflect.Type) (*uniformPlan, error) {
	if p, ok := uniformPlans.Load(t); ok {
		r := p.(uniformPlanResult)
		return r.plan, r.err
	}
	p, err := buildUniformPlan(t)
	actual, _ := uniformPlans.LoadOrStore(t, uniformPlanResult{p, err})
	r := actual.(uniformPlanResult)
	return r.plan, r.err
}

func uniformAlign(size, alignment int) int {
	return (size + alignment - 1) / alignment * alignment
}

func buildUniformPlan(t reflect.Type) (*uniformPlan, error) {
	p := new(uniformPlan)
	switch t {
	case reflect.TypeFor[lin.Vec2]():
		p.size, p.align, p.vector = 8, 8, 2
	case reflect.TypeFor[lin.Vec3]():
		p.size, p.align, p.vector = 12, 16, 3
	case reflect.TypeFor[lin.Vec4](), reflect.TypeFor[Color]():
		p.size, p.align, p.vector = 16, 16, 4
	case reflect.TypeFor[lin.Mat3]():
		p.size, p.align, p.matrix = 48, 16, 3
	case reflect.TypeFor[lin.Mat4]():
		p.size, p.align, p.matrix = 64, 16, 4
	default:
		switch t.Kind() {
		case reflect.Float32, reflect.Int32, reflect.Uint32, reflect.Bool:
			p.size, p.align = 4, 4
		case reflect.Array:
			element, err := cachedUniformPlan(t.Elem())
			if err != nil {
				return nil, fmt.Errorf("array element: %w", err)
			}
			p.align = uniformAlign(element.align, 16)
			p.stride = uniformAlign(element.size, p.align)
			if t.Len() == 0 || t.Len() > maxUniformBlock/p.stride {
				return nil, fmt.Errorf("array length %d exceeds uniform block capacity or is zero", t.Len())
			}
			p.size, p.element = t.Len()*p.stride, element
		case reflect.Struct:
			p.align = 16
			for i := range t.NumField() {
				field := t.Field(i)
				if !field.IsExported() {
					return nil, fmt.Errorf("field %s must be exported", field.Name)
				}
				child, err := cachedUniformPlan(field.Type)
				if err != nil {
					return nil, fmt.Errorf("field %s: %w", field.Name, err)
				}
				offset := uniformAlign(p.size, child.align)
				if offset > maxUniformBlock-child.size {
					return nil, fmt.Errorf("field %s exceeds %d-byte uniform block capacity", field.Name, maxUniformBlock)
				}
				p.fields = append(p.fields, uniformField{i, offset, child})
				p.size = offset + child.size
				p.align = max(p.align, child.align)
			}
			p.size = uniformAlign(p.size, p.align)
			if p.size == 0 || p.size > maxUniformBlock {
				return nil, fmt.Errorf("packed uniform block has %d bytes; want 1..%d", p.size, maxUniformBlock)
			}
		default:
			return nil, fmt.Errorf("unsupported uniform type %s", t)
		}
	}
	return p, nil
}

func (p *uniformPlan) pack(dst []byte, v reflect.Value) {
	if p.vector != 0 {
		for i := range p.vector {
			binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(float32(v.Field(i).Float())))
		}
		return
	}
	if p.matrix != 0 {
		for column := range p.matrix {
			for row := range p.matrix {
				binary.LittleEndian.PutUint32(dst[column*16+row*4:], math.Float32bits(float32(v.Index(column*p.matrix+row).Float())))
			}
		}
		return
	}
	switch v.Kind() {
	case reflect.Array:
		for i := range v.Len() {
			p.element.pack(dst[i*p.stride:], v.Index(i))
		}
	case reflect.Struct:
		for _, f := range p.fields {
			f.plan.pack(dst[f.offset:], v.Field(f.index))
		}
	case reflect.Float32:
		binary.LittleEndian.PutUint32(dst, math.Float32bits(float32(v.Float())))
	case reflect.Int32:
		binary.LittleEndian.PutUint32(dst, uint32(v.Int()))
	case reflect.Uint32:
		binary.LittleEndian.PutUint32(dst, uint32(v.Uint()))
	case reflect.Bool:
		if v.Bool() {
			binary.LittleEndian.PutUint32(dst, 1)
		}
	}
}
