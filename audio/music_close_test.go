package audio

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
)

type ownedMusicReader struct {
	*bytes.Reader
	closed int
}

func (r *ownedMusicReader) Close() error { r.closed++; return nil }

func TestMusicReaderOwnership(t *testing.T) {
	m := NewMixer(48000)
	data := encodeWAV16(Sine(440, 0.01, 48000))
	borrowed := &ownedMusicReader{Reader: bytes.NewReader(data)}
	music, err := m.OpenMusic(borrowed, false)
	if err != nil {
		t.Fatal(err)
	}
	music.Close()
	if borrowed.closed != 0 {
		t.Fatal("borrowed reader was closed")
	}
	owned := &ownedMusicReader{Reader: bytes.NewReader(data)}
	music, err = m.openOwnedMusic(owned, false)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(music.Close)
	}
	wg.Wait()
	if owned.closed != 1 {
		t.Fatalf("owned reader close calls=%d", owned.closed)
	}
	for _, data := range [][]byte{nil, []byte("nope"), []byte("RIFFbroken")} {
		r := &ownedMusicReader{Reader: bytes.NewReader(data)}
		if music, err := m.openOwnedMusic(r, false); err == nil || music != nil {
			t.Fatalf("invalid music: %v %v", music, err)
		}
		if r.closed != 1 {
			t.Fatalf("failure reader close calls=%d", r.closed)
		}
	}
	path := filepath.Join(t.TempDir(), "music.wav")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	music, err = m.OpenMusicFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	f := music.owned.(*os.File)
	music.Close()
	if _, err := f.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("owned file remained open: %v", err)
	}
	if _, err := m.OpenMusicFile(path+"missing", false); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestMusicCloseJoinsDecoder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		dec := tinyMusicDecoder{decoder: &memoryDecoder{pcm: PCM{Rate: 48000, Channels: 2}}, read: func([]float32) (int, error) { close(entered); <-release; return 0, io.EOF }}
		music := &Music{dec: dec, rate: 48000, seek: -1, ring: make([]float32, 2)}
		owned := &ownedMusicReader{}
		music.owned = owned
		music.cond = sync.NewCond(&music.mu)
		music.worker.Go(music.fill)
		<-entered
		returned := make(chan struct{})
		go func() { music.Close(); close(returned) }()
		synctest.Wait()
		select {
		case <-returned:
			t.Error("Close returned while decoder was still reading")
		default:
		}
		if owned.closed != 0 {
			t.Error("owned reader closed during active decode")
		}
		close(release)
		<-returned
		if owned.closed != 1 {
			t.Errorf("owned reader close calls=%d", owned.closed)
		}
	})
}
