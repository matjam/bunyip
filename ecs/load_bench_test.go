package ecs_test

// Load benchmarks: a scene document of 10000 named entities, half of them
// parented, built, encoded, parsed and instantiated.

import (
	"fmt"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type loadVel struct{ X, Y, Z float32 }

type loadHealth struct{ HP, Max int32 }

func init() {
	ecs.Register[loadVel]("load.vel")
	ecs.Register[loadHealth]("load.health")
}

func loadScene(b *testing.B, n int) *ecs.Scene {
	s := ecs.NewScene("level")
	for i := range n {
		id, err := s.AddEntity(fmt.Sprintf("e%d", i), gfx.Transform{Position: lin.V3(float32(i), 0, 0), Scale: lin.V3(1, 1, 1)}, loadVel{}, loadHealth{HP: 3})
		if err != nil {
			b.Fatal(err)
		}
		if i%2 == 1 {
			s.SetParent(id, id-1)
		}
	}
	return s
}

func BenchmarkLoadScene10k(b *testing.B) {
	const n = 10000
	b.Run("build_named", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			loadScene(b, n)
		}
	})
	s := loadScene(b, n)
	data, err := s.Encode()
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.Encode(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("parse", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			if _, err := ecs.ParseScene(data); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("instantiate", func(b *testing.B) {
		b.ReportAllocs()
		w := ecs.NewWorld()
		for b.Loop() {
			si, err := w.Instantiate(s)
			if err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
			si.Despawn(w)
			b.StartTimer()
		}
	})
}
