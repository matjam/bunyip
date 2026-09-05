package gfx

import "container/list"

// gpuResource is the lifetime boundary Graphics needs from its resources.
// Keeping ownership separate from Resources avoids losing children hidden
// from the diagnostic list, such as a font's atlas.
type gpuResource interface{ Destroy() }

type resourceOwner struct {
	order list.List
	live  map[gpuResource]*list.Element
}

func (o *resourceOwner) add(r gpuResource) {
	if o.live == nil {
		o.live = make(map[gpuResource]*list.Element)
	}
	if _, exists := o.live[r]; !exists {
		o.live[r] = o.order.PushBack(r)
	}
}

func (o *resourceOwner) remove(r gpuResource) {
	if e := o.live[r]; e != nil {
		o.order.Remove(e)
		delete(o.live, r)
	}
}

func (o *resourceOwner) destroy() {
	for e := o.order.Back(); e != nil; e = o.order.Back() {
		r := e.Value.(gpuResource)
		// Remove first: composite resources may destroy other entries.
		o.remove(r)
		r.Destroy()
	}
}
