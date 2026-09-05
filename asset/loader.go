package asset

import (
	"io/fs"
	"runtime"
	"sync"
	"sync/atomic"
)

// Loader reads and decodes assets on worker goroutines so a loading
// screen can keep drawing. Decoding produces CPU-side data (an image,
// decoded audio, a parsed model); creating GPU resources from it happens
// on the main thread once a handle is ready.
// Close finishes accepted loads and joins the workers before returning;
// the filesystem can then be closed. Wait joins currently submitted work
// without closing the loader; stop submitting loads before calling Wait.
type Loader struct {
	fs      fs.FS
	jobs    chan func()
	wg      sync.WaitGroup
	total   atomic.Int64
	done    atomic.Int64
	mu      sync.Mutex
	closing bool
	workers sync.WaitGroup
}

// NewLoader starts workers reading from fs; workers of zero means one
// per CPU.
func NewLoader(fsys fs.FS, workers int) *Loader {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	l := &Loader{fs: fsys, jobs: make(chan func(), 256)}
	for range workers {
		l.workers.Go(func() {
			for job := range l.jobs {
				job()
			}
		})
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

// Load reads name and decodes it on a worker. Submission can block when
// the 256-entry queue is full. A load submitted after shutdown starts
// returns a ready handle with fs.ErrClosed. Decode must not create GPU
// resources or call Close on its own loader.
func (l *Loader) Load[T any](name string, decode func(data []byte) (T, error)) *Handle[T] {
	h := &Handle[T]{done: make(chan struct{})}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing {
		h.err = &fs.PathError{Op: "load", Path: name, Err: fs.ErrClosed}
		close(h.done)
		return h
	}
	l.total.Add(1)
	l.wg.Add(1)
	l.jobs <- func() {
		defer l.wg.Done()
		defer close(h.done)
		defer l.done.Add(1)
		data, err := fs.ReadFile(l.fs, name)
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

// Close stops submission and waits for accepted loads and all workers.
// Repeated and concurrent calls are safe. A blocked reader or decoder
// must return before Close can finish. Do not call Close from a decoder.
func (l *Loader) Close() {
	l.mu.Lock()
	if !l.closing {
		l.closing = true
		close(l.jobs)
	}
	l.mu.Unlock()
	l.workers.Wait()
}
