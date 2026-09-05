package console

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/ui"
)

// listHeight is the height of the lists inside the panels, and
// listLimit how many entities one of them shows before it stops.
const (
	listHeight = 120
	listLimit  = 500
)

// world returns the world the entity and physics panels are showing, and
// its name, or nil when the game has attached none.
func (c *Console) world() (*ecs.World, string) {
	if len(c.attach.worlds) == 0 {
		return nil, ""
	}
	c.panels.world = max(0, min(c.panels.world, len(c.attach.worlds)-1))
	w := c.attach.worlds[c.panels.world]
	return w.world, w.name
}

// focusedField is the label of the text field that had keyboard focus in
// the last frame. A field being typed into keeps what is typed; every
// other field shows the live value.
func (c *Console) focusedField() string {
	for _, n := range c.ui.Accessible() {
		if n.Focused && (n.Role == "textfield" || n.Role == "textarea") {
			return n.Label
		}
	}
	return ""
}

// drawEntities lists the world's entities, shows the selected one's
// components and lets them be edited, spawned and despawned.
func (c *Console) drawEntities(f Frame, area ui.Rect) {
	u := c.ui
	w, _ := c.world()
	if w == nil {
		u.Label("no world attached: call Attach(name, world) on the console")
		return
	}
	p := &c.panels
	if p.edits == nil {
		p.edits = map[string]*string{}
	}
	// The entities matching the filter, by component type name. The list
	// stops at listLimit, so a world of a hundred thousand entities does
	// not build a hundred thousand strings every frame.
	var items []string
	p.entities = p.entities[:0]
	filter := strings.ToLower(strings.TrimSpace(p.filter))
	matched := 0
	for _, e := range w.Entities() {
		names := componentNames(w, e)
		if filter != "" && !matchesAny(names, filter) {
			continue
		}
		matched++
		if len(p.entities) >= listLimit {
			continue
		}
		p.entities = append(p.entities, e)
		items = append(items, e.String()+"  "+strings.Join(names, ", "))
	}
	if p.entityAt >= len(items) {
		p.entityAt = len(items) - 1
	}
	fields := 0
	if w.Alive(p.selected) {
		fields = countFields(w, p.selected)
	}
	prefabs := prefabNames(w)
	rows := 9 + fields + len(prefabs) + len(w.Stats())
	u.ScrollArea("entities", area, c.rowsH(rows)+listHeight+80, func() {
		if names := c.Worlds(); len(names) > 1 {
			u.Dropdown("world", &p.world, names)
		}
		shown := fmt.Sprintf("%d shown", len(items))
		if matched > len(items) {
			shown = fmt.Sprintf("%d of %d matching shown", len(items), matched)
		}
		u.Label(fmt.Sprintf("%d entities, %s, %d systems, %d resources",
			w.Count(), shown, len(w.Stats()), len(w.Resources())))
		u.TextField("filter by component", &p.filter)
		if u.ListBox("entities", listHeight, items, &p.entityAt) {
			if p.entityAt >= 0 && p.entityAt < len(p.entities) {
				p.selected = p.entities[p.entityAt]
				clear(p.edits)
			}
		}
		u.Separator()
		c.drawSelected(w)
		if len(prefabs) > 0 {
			u.Separator()
			u.Label("prefabs")
			lib := *ecs.Resource[Prefabs](w)
			for _, name := range prefabs {
				if u.Button("spawn " + name) {
					p.selected = lib[name].Spawn(w)
					clear(p.edits)
					c.Printf("spawned %s as %s", name, p.selected)
				}
			}
		}
		u.Separator()
		c.drawSystems(w)
		c.drawResources(w)
	})
}

// matchesAny reports whether any component name holds the filter text.
func matchesAny(names []string, filter string) bool {
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), filter) {
			return true
		}
	}
	return false
}

// componentNames are the short type names of an entity's components.
func componentNames(w *ecs.World, e ecs.Entity) []string {
	types := w.Components(e)
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = t.Name()
	}
	return out
}

// countFields estimates how many rows the selected entity's components
// take, so the scrolling area knows how tall its contents are.
func countFields(w *ecs.World, e ecs.Entity) int {
	n := 1
	for _, v := range w.ComponentValues(e) {
		n += 1 + countValueFields(reflect.ValueOf(v), 0)
	}
	return n
}

func countValueFields(v reflect.Value, depth int) int {
	if v.Kind() != reflect.Struct || depth > 2 {
		return 1
	}
	n := 0
	for i := range v.NumField() {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		n += countValueFields(v.Field(i), depth+1)
	}
	return max(n, 1)
}

// prefabNames are the names in the world's Prefabs resource, sorted.
func prefabNames(w *ecs.World) []string {
	lib := ecs.Resource[Prefabs](w)
	if lib == nil {
		return nil
	}
	out := make([]string, 0, len(*lib))
	for name := range *lib {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// drawSelected shows the selected entity's components, each field
// editable, with a button to despawn it.
func (c *Console) drawSelected(w *ecs.World) {
	u := c.ui
	e := c.panels.selected
	if !w.Alive(e) {
		u.Label("no entity selected")
		return
	}
	u.Row(2, func() {
		u.Label("selected " + e.String())
		if u.Button("despawn") {
			w.Despawn(e)
			c.panels.selected = ecs.None
			clear(c.panels.edits)
			return
		}
	})
	if !w.Alive(e) {
		return
	}
	types := w.Components(e)
	for i, v := range w.ComponentValues(e) {
		name := "component"
		if i < len(types) {
			name = types[i].Name()
		}
		c.editComponent(w, e, name, v)
	}
}

// editComponent draws one component's fields and writes the whole
// component back when a field changes. Only numbers, bools and strings
// are editable; anything else shows its value.
func (c *Console) editComponent(w *ecs.World, e ecs.Entity, name string, v any) {
	u := c.ui
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		u.Label(fmt.Sprintf("%s = %v", name, v))
		return
	}
	edit := reflect.New(rv.Type()).Elem()
	edit.Set(rv)
	changed := false
	u.TreeOpen(name, func() {
		changed = c.editFields(e, name, "", edit, 0)
	})
	if changed {
		w.SetComponent(e, edit.Interface())
	}
}

// editFields walks a struct's exported fields, drawing an editor for
// each one it can edit and recursing into nested structs. path names the
// field for the widget's identity, which is the whole path from the
// component; shown is what the panel writes beside it, which is the path
// inside the component. It reports whether anything changed.
func (c *Console) editFields(e ecs.Entity, path, shown string, v reflect.Value, depth int) bool {
	u := c.ui
	changed := false
	for i := range v.NumField() {
		ft := v.Type().Field(i)
		if !ft.IsExported() {
			continue
		}
		fv := v.Field(i)
		label := path + "." + ft.Name
		name := ft.Name
		if shown != "" {
			name = shown + "." + ft.Name
		}
		// text edits one field as text: the buffer is parsed by parse,
		// which stores it and reports whether the value changed.
		text := func(live string, parse func(string) bool) {
			s := c.buffer(e, label, live)
			if c.field(name, func() bool { return u.TextField(label, s) }) && parse(strings.TrimSpace(*s)) {
				changed = true
			}
		}
		switch fv.Kind() {
		case reflect.Bool:
			b := fv.Bool()
			if u.Checkbox(label, &b) {
				fv.SetBool(b)
				changed = true
			}
		case reflect.String:
			text(fv.String(), func(s string) bool { fv.SetString(s); return true })
		case reflect.Float32, reflect.Float64:
			text(strconv.FormatFloat(fv.Float(), 'g', 6, 64), func(s string) bool {
				n, err := strconv.ParseFloat(s, 64)
				if err != nil || fv.OverflowFloat(n) {
					return false
				}
				fv.SetFloat(n)
				return true
			})
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			text(strconv.FormatInt(fv.Int(), 10), func(s string) bool {
				n, err := strconv.ParseInt(s, 10, 64)
				if err != nil || fv.OverflowInt(n) {
					return false
				}
				fv.SetInt(n)
				return true
			})
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			text(strconv.FormatUint(fv.Uint(), 10), func(s string) bool {
				n, err := strconv.ParseUint(s, 10, 64)
				if err != nil || fv.OverflowUint(n) {
					return false
				}
				fv.SetUint(n)
				return true
			})
		case reflect.Struct:
			if depth >= 2 {
				u.Label(fmt.Sprintf("%s = %v", name, fv.Interface()))
				continue
			}
			if c.editFields(e, label, name, fv, depth+1) {
				changed = true
			}
		default:
			u.Label(fmt.Sprintf("%s = %v", name, fv.Interface()))
		}
	}
	return changed
}

// field lays a field's name beside its editor and reports what the
// editor said.
func (c *Console) field(name string, editor func() bool) bool {
	changed := false
	c.ui.Columns([]float32{1, 1.6}, func() {
		c.ui.Label(name)
		changed = editor()
	})
	return changed
}

// buffer is the text of one field being edited. Every field but the one
// with keyboard focus is refreshed from the live value, so a body's
// moving position keeps up while what is being typed is left alone.
func (c *Console) buffer(e ecs.Entity, label, live string) *string {
	key := e.String() + label
	p := c.panels.edits[key]
	if p == nil {
		s := live
		p = &s
		c.panels.edits[key] = p
	}
	if label != c.focusedField() {
		*p = live
	}
	return p
}

// drawSystems lists the world's systems with their last timings and a
// switch to turn one off.
func (c *Console) drawSystems(w *ecs.World) {
	u := c.ui
	stats := w.Stats()
	if len(stats) == 0 {
		u.Label("no systems registered")
		return
	}
	u.Label("systems")
	for _, s := range stats {
		on := w.SystemEnabled(s.Name)
		label := fmt.Sprintf("%s   %.3f ms", s.Name, s.MS)
		if u.Checkbox(label, &on) {
			w.SetSystemEnabled(s.Name, on)
		}
	}
}

// drawResources lists the world's resource types.
func (c *Console) drawResources(w *ecs.World) {
	names := w.Resources()
	if len(names) == 0 {
		return
	}
	c.ui.Label("resources: " + strings.Join(names, ", "))
}
