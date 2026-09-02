package network

// Interest decides which entities a viewer should hear about: those
// within Radius of the viewer, with hysteresis so an entity at the edge
// does not flicker in and out. An entity enters the set inside Radius
// and leaves only beyond Radius+Margin. Distances are in two
// dimensions; ID is whatever names an entity. Each frame, call Begin
// with the viewer's position, Visit for every candidate, then End. The
// zero value is ready to use once Radius is set.
type Interest[ID comparable] struct {
	// Radius is the distance within which an entity enters the set.
	Radius float32
	// Margin is how much further an entity may go before it leaves;
	// zero means a tenth of Radius.
	Margin  float32
	vx, vy  float32
	in      map[ID]struct{} // the set after the last End
	seen    map[ID]struct{} // the set being built this frame
	entered []ID            // reused by End
	left    []ID            // reused by End
}

// Begin starts a frame at the viewer's position.
func (in *Interest[ID]) Begin(viewerX, viewerY float32) {
	in.vx, in.vy = viewerX, viewerY
	if in.in == nil {
		in.in = map[ID]struct{}{}
		in.seen = map[ID]struct{}{}
	}
	clear(in.seen)
}

// Visit considers an entity at a position and reports whether it is in
// the set this frame, so the caller can send it in the same pass.
func (in *Interest[ID]) Visit(id ID, x, y float32) bool {
	if in.seen == nil {
		in.Begin(in.vx, in.vy)
	}
	r := in.Radius
	if _, was := in.in[id]; was {
		margin := in.Margin
		if margin <= 0 {
			margin = in.Radius / 10
		}
		r += margin
	}
	dx, dy := x-in.vx, y-in.vy
	if dx*dx+dy*dy > r*r {
		return false
	}
	in.seen[id] = struct{}{}
	return true
}

// End finishes the frame and reports which entities entered the set
// and which left it since the last End, in no particular order. An
// entity not visited this frame leaves. Both slices belong to the
// Interest and are refilled by the next End, so send from them during
// the frame and copy anything that has to outlive it.
func (in *Interest[ID]) End() (entered, left []ID) {
	in.entered, in.left = in.entered[:0], in.left[:0]
	for id := range in.seen {
		if _, was := in.in[id]; !was {
			in.entered = append(in.entered, id)
		}
	}
	for id := range in.in {
		if _, still := in.seen[id]; !still {
			in.left = append(in.left, id)
		}
	}
	in.in, in.seen = in.seen, in.in
	clear(in.seen)
	return in.entered, in.left
}

// Contains reports whether an entity was in the set at the last End.
func (in *Interest[ID]) Contains(id ID) bool {
	_, ok := in.in[id]
	return ok
}

// Len is how many entities were in the set at the last End.
func (in *Interest[ID]) Len() int { return len(in.in) }
