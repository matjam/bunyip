package soft

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// parallelMin is the fewest fluid or soft-body particles a pass splits
// across goroutines, and linkParallelMin the fewest cloth links or
// particles, whose work each is a few dozen instructions. Below them,
// starting and joining the goroutines costs more than the pass saves:
// on an eight-core Apple M2 a fluid of 2000 particles already steps
// faster split, and a cloth only from about 16000 particles, whose link
// batches hold some 8000 links each.
const (
	parallelMin     = 2048
	linkParallelMin = 8192
)

// chunkSize is how many items each goroutine takes at a time. The chunks
// are cut the same way whatever the machine, and every pass that is split
// writes only its own items, so the result never depends on GOMAXPROCS
// or on which goroutine ran which chunk.
const chunkSize = 1024

// parallel calls fn over [0, n) chunk by chunk, on up to GOMAXPROCS
// goroutines. The chunks are the same however many goroutines run them,
// so fn may keep state per chunk. fn must write only the items of its
// own range. Callers take the pass in one call below their threshold, so
// a small scene creates no goroutine and no closure.
func parallel(n int, fn func(lo, hi int)) {
	chunks := (n + chunkSize - 1) / chunkSize
	workers := min(runtime.GOMAXPROCS(0), chunks)
	if workers <= 1 {
		for c := range chunks {
			fn(c*chunkSize, min(n, (c+1)*chunkSize))
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				c := int(next.Add(1) - 1)
				if c >= chunks {
					return
				}
				fn(c*chunkSize, min(n, (c+1)*chunkSize))
			}
		}()
	}
	wg.Wait()
}

// parallelEach calls fn for each of n independent items, one item at a
// time, on up to GOMAXPROCS goroutines when there are at least two and
// work, their total size, reaches parallelMin.
func parallelEach(n, work int, fn func(i int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	if n < 2 || work < parallelMin || workers <= 1 {
		for i := range n {
			fn(i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}
