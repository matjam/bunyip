package locale_test

import (
	"testing"

	"github.com/matjam/bunyip/locale"
)

func benchTranslator(b *testing.B) *locale.Translator {
	b.Helper()
	t := locale.NewTable("en")
	t.Set("menu.play", "Play")
	t.Set("hud.gold", "{n} gold")
	t.Set("hud.greet", "{who} has {n} arrows")
	t.SetPlural("inv.arrows", map[locale.Category]string{
		locale.One:   "{n} arrow",
		locale.Other: "{n} arrows",
	})
	bundle := locale.NewBundle("en")
	bundle.Add(t)
	return bundle.For("en")
}

// BenchmarkTNoArgs is the common case: a menu label with no
// placeholders, which should be a lookup and nothing else.
func BenchmarkTNoArgs(b *testing.B) {
	tr := benchTranslator(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if s := tr.T("menu.play"); s == "" {
			b.Fatal("empty")
		}
	}
}

// BenchmarkTTwoArgs fills two placeholders.
func BenchmarkTTwoArgs(b *testing.B) {
	tr := benchTranslator(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if s := tr.T("hud.greet", "who", "Ada", "n", 3); s == "" {
			b.Fatal("empty")
		}
	}
}

// BenchmarkTPlural picks a plural form and fills its count.
func BenchmarkTPlural(b *testing.B) {
	tr := benchTranslator(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if s := tr.N("inv.arrows", 3); s == "" {
			b.Fatal("empty")
		}
	}
}

// BenchmarkTMissing is the path a key nobody translated takes.
func BenchmarkTMissing(b *testing.B) {
	tr := benchTranslator(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		tr.T("menu.nothing")
	}
}
