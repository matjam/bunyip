package ecs

import "reflect"

// Remapper is implemented by components and resources that hold
// entities somewhere Load and CloneTree cannot see: unexported fields,
// or values behind an interface they should not rewrite. Remap replaces
// every entity the value holds with fn of it. A type without Remap has
// its exported fields walked with reflection instead, which covers
// Entity fields, slices, arrays, maps and pointers of them.
type Remapper interface {
	Remap(fn func(Entity) Entity)
}

var (
	entityType   = reflect.TypeOf(Entity{})
	parentType   = reflect.TypeOf(Parent{})
	childrenType = reflect.TypeOf(Children{})
	remapperType = reflect.TypeOf((*Remapper)(nil)).Elem()
)

// remapAny rewrites the entities inside a component or resource value
// and returns the result.
func remapAny(v any, fn func(Entity) Entity) any {
	p := reflect.New(reflect.TypeOf(v))
	p.Elem().Set(reflect.ValueOf(v))
	remapPtr(p, fn)
	return p.Elem().Interface()
}

// remapPtr rewrites the entities inside the value p points to.
func remapPtr(p reflect.Value, fn func(Entity) Entity) {
	if p.Type().Implements(remapperType) {
		p.Interface().(Remapper).Remap(fn)
		return
	}
	if holdsEntity(p.Type().Elem(), map[reflect.Type]bool{}) {
		remapValue(p.Elem(), fn, map[uintptr]bool{})
	}
}

// holdsEntity reports whether a value of type t can contain an Entity
// reachable through exported fields.
func holdsEntity(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == entityType {
		return true
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			if f.IsExported() && holdsEntity(f.Type, seen) {
				return true
			}
		}
	case reflect.Slice, reflect.Array, reflect.Pointer:
		return holdsEntity(t.Elem(), seen)
	case reflect.Map:
		return holdsEntity(t.Key(), seen) || holdsEntity(t.Elem(), seen)
	case reflect.Interface:
		return true
	}
	return false
}

// remapValue rewrites entities in a settable value in place.
func remapValue(v reflect.Value, fn func(Entity) Entity, seen map[uintptr]bool) {
	if v.Type() == entityType {
		v.Set(reflect.ValueOf(fn(v.Interface().(Entity))))
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				remapValue(f, fn, seen)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			remapValue(v.Index(i), fn, seen)
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		n := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			k := reflect.New(v.Type().Key()).Elem()
			k.Set(it.Key())
			remapValue(k, fn, seen)
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(it.Value())
			remapValue(e, fn, seen)
			n.SetMapIndex(k, e)
		}
		v.Set(n)
	case reflect.Pointer:
		if v.IsNil() || seen[v.Pointer()] {
			return
		}
		seen[v.Pointer()] = true
		remapValue(v.Elem(), fn, seen)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		inner := v.Elem()
		n := reflect.New(inner.Type()).Elem()
		n.Set(inner)
		remapValue(n, fn, seen)
		v.Set(n)
	}
}

// deepCopy copies a component value. Exported fields are copied all the
// way down (slices, arrays, maps, pointers and interface values get new
// storage; a pointer met twice is copied once). Unexported fields are
// copied as values, so a slice or pointer in one still shares storage
// with the original. Functions and channels are shared.
func deepCopy(v any) any {
	if v == nil {
		return nil
	}
	src := reflect.ValueOf(v)
	dst := reflect.New(src.Type()).Elem()
	copyInto(dst, src, map[copyKey]reflect.Value{})
	return dst.Interface()
}

type copyKey struct {
	ptr uintptr
	typ reflect.Type
}

func copyInto(dst, src reflect.Value, seen map[copyKey]reflect.Value) {
	switch src.Kind() {
	case reflect.Struct:
		dst.Set(src)
		for i := range src.NumField() {
			if f := dst.Field(i); f.CanSet() {
				copyInto(f, src.Field(i), seen)
			}
		}
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		n := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		for i := range src.Len() {
			copyInto(n.Index(i), src.Index(i), seen)
		}
		dst.Set(n)
	case reflect.Array:
		for i := range src.Len() {
			copyInto(dst.Index(i), src.Index(i), seen)
		}
	case reflect.Map:
		if src.IsNil() {
			return
		}
		n := reflect.MakeMapWithSize(src.Type(), src.Len())
		for it := src.MapRange(); it.Next(); {
			k := reflect.New(src.Type().Key()).Elem()
			copyInto(k, it.Key(), seen)
			e := reflect.New(src.Type().Elem()).Elem()
			copyInto(e, it.Value(), seen)
			n.SetMapIndex(k, e)
		}
		dst.Set(n)
	case reflect.Pointer:
		if src.IsNil() {
			return
		}
		key := copyKey{src.Pointer(), src.Type()}
		if c, ok := seen[key]; ok {
			dst.Set(c)
			return
		}
		n := reflect.New(src.Type().Elem())
		seen[key] = n
		copyInto(n.Elem(), src.Elem(), seen)
		dst.Set(n)
	case reflect.Interface:
		if src.IsNil() {
			return
		}
		inner := src.Elem()
		n := reflect.New(inner.Type()).Elem()
		copyInto(n, inner, seen)
		dst.Set(n)
	default:
		dst.Set(src)
	}
}
