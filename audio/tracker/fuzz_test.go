package tracker

import "testing"

// FuzzLoad feeds the loaders corrupt modules: whatever comes in, they
// must return an error rather than panic, and a module that loads must
// render without panicking.
func FuzzLoad(f *testing.F) {
	f.Add(buildMOD())
	f.Add(buildS3M())
	f.Add([]byte("Extended Module: "))
	f.Add([]byte("IMPM"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Load(data)
		if err != nil {
			return
		}
		p := NewPlayer(m, 8000)
		buf := make([]float32, 2*256)
		for range 4 {
			p.Read(buf)
		}
	})
}
