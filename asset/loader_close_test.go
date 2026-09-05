package asset

import (
	"errors"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
	"testing/synctest"
)

func TestLoaderCloseJoinsAcceptedWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := NewLoader(fstest.MapFS{"value": {Data: []byte("data")}}, 1)
		release := make(chan struct{})
		h := l.Load("value", func(data []byte) (string, error) { <-release; return string(data), nil })
		queued := l.Load("value", func(data []byte) (int, error) { return len(data), nil })
		finished := make(chan struct{})
		go func() { l.Close(); close(finished) }()
		synctest.Wait()
		select {
		case <-finished:
			t.Fatal("Close returned before decode completed")
		default:
		}
		late := l.Load("value", func([]byte) (bool, error) { t.Error("decoder ran after Close"); return true, nil })
		if !late.Ready() {
			t.Fatal("rejected handle is pending")
		}
		if _, err := late.Get(); !errors.Is(err, fs.ErrClosed) {
			t.Fatal(err)
		}
		close(release)
		<-finished
		if value, err := h.Get(); value != "data" || err != nil {
			t.Fatalf("first: %q %v", value, err)
		}
		if value, err := queued.Get(); value != 4 || err != nil {
			t.Fatalf("queued: %d %v", value, err)
		}
		l.Close()
		if done, total := l.Progress(); done != 2 || total != 2 {
			t.Fatalf("progress %d/%d", done, total)
		}
	})
}

func TestLoaderConcurrentCloseAndSubmission(t *testing.T) {
	l := NewLoader(fstest.MapFS{"value": {Data: []byte("data")}}, 2)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			h := l.Load("value", func(data []byte) (string, error) { return string(data), nil })
			value, err := h.Get()
			if err != nil && !errors.Is(err, fs.ErrClosed) {
				t.Error(err)
			}
			if err == nil && value != "data" {
				t.Error(value)
			}
		})
		wg.Go(l.Close)
	}
	wg.Wait()
}
