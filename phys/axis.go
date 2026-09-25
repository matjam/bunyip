package phys

import "slices"

// axisList is a set of collider rows in sweep order: by the low end of
// their bounds on the sweep axis, then by row, so exactly one order
// exists whatever order the sort starts from. keys and ends, passed to
// each method, hold every row's interval on the axis.
type axisList struct {
	order []int32
	// The longest interval, so a search that walks back from the last
	// interval starting inside its target knows when to stop. It is
	// measured on the first search after a sort, and not at all when a
	// step makes none.
	span    float32
	spanned bool
}

func axisLess(keys []float32, a, b int32) bool {
	ka, kb := keys[a], keys[b]
	return ka < kb || (ka == kb && a < b)
}

// sort insertion-sorts the kept order, which barely changes from one
// step to the next, so re-sorting costs about one pass. A set that
// changed a lot spends the move budget and finishes with a general sort.
func (l *axisList) sort(keys []float32) {
	a := l.order
	budget := 4*len(a) + 64
	for i := 1; i < len(a); i++ {
		v := a[i]
		j := i - 1
		for j >= 0 && axisLess(keys, v, a[j]) {
			a[j+1] = a[j]
			j--
			budget--
		}
		a[j+1] = v
		if budget < 0 {
			slices.SortFunc(a, func(x, y int32) int {
				switch {
				case axisLess(keys, x, y):
					return -1
				case axisLess(keys, y, x):
					return 1
				}
				return 0
			})
			return
		}
	}
}

// measure finds the longest interval in the list.
func (l *axisList) measure(keys, ends []float32) {
	l.span = 0
	for _, k := range l.order {
		l.span = max(l.span, ends[k]-keys[k])
	}
	l.spanned = true
}

// overlapping appends every row whose interval reaches into [lo, hi] to
// dst. The walk begins at the last interval starting at or before hi and
// stops one span before lo, which nothing can reach across.
func (l *axisList) overlapping(dst []int32, keys, ends []float32, lo, hi float32) []int32 {
	n := len(l.order)
	if n == 0 {
		return dst
	}
	if !l.spanned {
		l.measure(keys, ends)
	}
	first, last := 0, n
	for first < last {
		mid := int(uint(first+last) >> 1)
		if keys[l.order[mid]] > hi {
			last = mid
		} else {
			first = mid + 1
		}
	}
	for x := first - 1; x >= 0; x-- {
		i := l.order[x]
		if keys[i] < lo-l.span {
			break
		}
		if ends[i] >= lo {
			dst = append(dst, i)
		}
	}
	return dst
}

// sweepPairs calls fn for every pair of rows whose intervals on the
// sweep axis overlap and of which at least one is moving, first the one
// earlier in sweep order. The pairs come in the order one sort-and-sweep
// over the moving and the still rows together gives them: by the first
// row's place in the combined order, then the second's. Pairs of two
// still rows are never visited, so a level of still colliders costs one
// pass over their list, not a pass over every pair of them that
// overlaps on the axis.
func sweepPairs(keys, ends []float32, moving, still []int32, fn func(i, j int)) {
	mi, si := 0, 0
	for mi < len(moving) {
		if si < len(still) && axisLess(keys, still[si], moving[mi]) {
			// A still row: its partners are the moving rows after it.
			x := still[si]
			si++
			end := ends[x]
			for _, y := range moving[mi:] {
				if keys[y] > end {
					break
				}
				fn(int(x), int(y))
			}
			continue
		}
		// A moving row: its partners are every row after it, taken from
		// both lists in combined order.
		x := moving[mi]
		mi++
		end := ends[x]
		a, b := mi, si
		for {
			var y int32
			switch {
			case a < len(moving) && (b == len(still) || axisLess(keys, moving[a], still[b])):
				y = moving[a]
				a++
			case b < len(still):
				y = still[b]
				b++
			default:
				y = -1
			}
			if y < 0 || keys[y] > end {
				break
			}
			fn(int(x), int(y))
		}
	}
}
