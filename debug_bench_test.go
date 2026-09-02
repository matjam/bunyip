package bunyip

import (
	"testing"
	"time"
)

// profileClosure is the shape Profile had before it returned a scope: a
// closure per timed section per frame. It is kept so the benchmarks can
// compare the two.
//
// At the default optimisation both shapes allocate nothing, because
// Profile is small enough to inline and the closure then stays on the
// stack. Run these with -gcflags=-l, which is what a call site where
// Profile cannot be inlined looks like, and the closure costs 64 bytes
// and an allocation every time while the scope still costs neither:
//
//	go test -run XXX -bench Profile -benchmem -gcflags=-l .
func profileClosure(c *Context, name string) func() {
	start := time.Now()
	return func() {
		c.scopes = append(c.scopes, Scope{Name: name, MS: float64(time.Since(start).Microseconds()) / 1000})
	}
}

func BenchmarkProfileClosure(b *testing.B) {
	ctx := &Context{}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		done := profileClosure(ctx, "section")
		done()
		ctx.scopes = ctx.scopes[:0]
	}
}

// deferredClosure and deferredScope profile a whole function, the form
// the documentation used to recommend and the form it recommends now.
// This is where the closure was paid for: a deferred call cannot keep it
// on the stack when the function it guards has more than the one defer.
func deferredClosure(ctx *Context) {
	defer profileClosure(ctx, "section")()
	sink++
}

func deferredScope(ctx *Context) {
	defer ctx.Profile("section").End()
	sink++
}

var sink int

func BenchmarkProfileClosureDeferred(b *testing.B) {
	ctx := &Context{}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		deferredClosure(ctx)
		ctx.scopes = ctx.scopes[:0]
	}
}

func BenchmarkProfileDeferred(b *testing.B) {
	ctx := &Context{}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		deferredScope(ctx)
		ctx.scopes = ctx.scopes[:0]
	}
}

// BenchmarkProfile times one profiled section, which a game opens and
// closes several times a frame whether or not the overlay is shown.
func BenchmarkProfile(b *testing.B) {
	ctx := &Context{}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		section := ctx.Profile("section")
		section.End()
		ctx.scopes = ctx.scopes[:0]
	}
}
