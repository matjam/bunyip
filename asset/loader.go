package asset

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// Loader reads and decodes assets on worker goroutines so a loading
// screen can keep drawing. Decoding produces CPU-side data (an image,
// decoded audio, a parsed model); creating GPU resources from it happens
// on the main thread once a handle is ready.
type Loader struct {
	fs      *FS
	jobs    chan func()
	wg      sync.WaitGroup
	total   atomic.Int64
	done    atomic.Int64
	closing atomic.Bool
}

// NewLoader starts workers reading from fs; workers of zero means one
// per CPU.
func NewLoader(fs *FS, workers int) *Loader {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	l := &Loader{fs: fs, jobs: make(chan func(), 256)}
	for range workers {
		go func() {
			for job := range l.jobs {
				job()
			}
		}()
	}
	return l
}

// Handle is a pending or finished load.
type Handle[T any] struct {
	done  chan struct{}
	value T
	err   error
}

// Ready reports whether the load has finished, successfully or not.
func (h *Handle[T]) Ready() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

// Get waits for the load and returns its result.
func (h *Handle[T]) Get() (T, error) {
	<-h.done
	return h.value, h.err
}

// Value returns the result without waiting; ok is false until ready.
func (h *Handle[T]) Value() (v T, err error, ok bool) {
	if !h.Ready() {
		return v, nil, false
	}
	return h.value, h.err, true
}

// Load reads name and decodes it on a worker.
func Load[T any](l *Loader, name string, decode func(data []byte) (T, error)) *Handle[T] {
	h := &Handle[T]{done: make(chan struct{})}
	l.total.Add(1)
	l.wg.Add(1)
	l.jobs <- func() {
		defer l.wg.Done()
		defer close(h.done)
		defer l.done.Add(1)
		data, err := l.fs.Read(name)
		if err != nil {
			h.err = err
			return
		}
		h.value, h.err = decode(data)
	}
	return h
}

// Progress reports loads finished and requested so far, for a bar.
func (l *Loader) Progress() (done, total int) {
	return int(l.done.Load()), int(l.total.Load())
}

// Wait blocks until every requested load has finished.
func (l *Loader) Wait() { l.wg.Wait() }

// Close stops the workers after queued loads finish.
func (l *Loader) Close() {
	if l.closing.CompareAndSwap(false, true) {
		close(l.jobs)
	}
}
