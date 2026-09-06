package gfx

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// TestHyphenatorForLanguages checks a known hyphenation for each language
// the engine ships patterns for, including the words British and American
// English break differently.
func TestHyphenatorForLanguages(t *testing.T) {
	cases := []struct {
		lang string
		word string
		want string // the word with a hyphen at every break point
	}{
		{"en-us", "criticism", "crit-i-cism"},
		{"en-us", "democracy", "democ-racy"},
		{"en-gb", "criticism", "cri-ti-cism"},
		{"en-gb", "democracy", "demo-cracy"},
		{"de", "Silbentrennung", "Sil-ben-tren-nung"},
		{"fr", "typographie", "ty-po-gra-phie"},
		{"es", "ortografía", "or-to-gra-fía"},
		{"it", "sillabazione", "sil-la-ba-zio-ne"},
		{"nl", "lettergrepen", "let-ter-gre-pen"},
		{"pt", "computador", "com-pu-ta-dor"},
		{"sv", "avstavning", "av-stav-ning"},
		{"da", "orddeling", "ord-de-ling"},
		{"nb", "orddeling", "ord-de-ling"},
		{"no", "orddeling", "ord-de-ling"},
		{"fi", "tietokone", "tie-to-ko-ne"},
		{"pl", "przenoszenie", "prze-no-sze-nie"},
		{"ru", "переносы", "пе-ре-но-сы"},
		// A regional tag with no patterns of its own falls back to its
		// primary language.
		{"de-AT", "Silbentrennung", "Sil-ben-tren-nung"},
		{"en_AU", "criticism", "crit-i-cism"},
		// Norwegian Bokmål adds its own exceptions to the shared Norwegian
		// patterns it inputs, which break this word differently.
		{"nb", "attende", "at-ten-de"},
		{"no", "attende", "atten-de"},
	}
	for _, c := range cases {
		t.Run(c.lang+"/"+c.word, func(t *testing.T) {
			h, err := HyphenatorFor(c.lang)
			if err != nil {
				t.Fatalf("HyphenatorFor(%q): %v", c.lang, err)
			}
			if got := hyphenated(h, c.word); got != c.want {
				t.Errorf("Hyphenate(%q) = %q, want %q", c.word, got, c.want)
			}
		})
	}
	if _, err := HyphenatorFor("kl"); err == nil {
		t.Error("HyphenatorFor(kl) found patterns the engine does not ship")
	}
	// The hyphenator is shared, so two lookups of the same language are the
	// same value.
	a, _ := HyphenatorFor("fr")
	b, _ := HyphenatorFor("FR")
	if a != b || a != mustHyphenator(t, "fr-CA") {
		t.Error("HyphenatorFor built the French patterns more than once")
	}
	// The hyphenmins in a pattern file's header set the limits.
	if h := EnglishHyphenator(); h.MinLeft != 2 || h.MinRight != 3 {
		t.Errorf("American English mins = %d/%d, want 2/3", h.MinLeft, h.MinRight)
	}
	if h := mustHyphenator(t, "de"); h.MinLeft != 2 || h.MinRight != 2 {
		t.Errorf("German mins = %d/%d, want 2/2", h.MinLeft, h.MinRight)
	}
}

// TestAutoHyphenateFollowsLanguage checks that text options hyphenate in
// the language they are drawn in, and that an explicit hyphenator wins.
func TestAutoHyphenateFollowsLanguage(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 12, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	const word = "Bundesregierung"
	width, _ := f.Measure(word, TextOptions{})
	de := TextOptions{Width: width * 0.7, Language: "de", AutoHyphenate: true}
	lines := layoutStrings(t, f, word, de)
	if len(lines) != 2 || lines[0] != "Bundesre" {
		t.Errorf("German auto-hyphenation gave %q, want a break at Bundesre-", lines)
	}
	// A language with no patterns is not hyphenated, so the word breaks
	// where it overflows instead.
	none := de
	none.Language = "kl"
	if lines := layoutStrings(t, f, word, none); slices.Equal(lines, layoutStrings(t, f, word, de)) {
		t.Errorf("a language without patterns hyphenated %q into %q", word, lines)
	}
	// The explicit hyphenator wins over the language.
	both := de
	both.Hyphenate = EnglishHyphenator()
	english := de
	english.Language, english.AutoHyphenate = "", false
	english.Hyphenate = EnglishHyphenator()
	if !slices.Equal(layoutStrings(t, f, word, both), layoutStrings(t, f, word, english)) {
		t.Errorf("Hyphenate did not win over Language: %q vs %q", layoutStrings(t, f, word, both), layoutStrings(t, f, word, english))
	}
	if slices.Equal(layoutStrings(t, f, word, english), lines) {
		t.Errorf("the English and German hyphenators break %q the same way, so the case proves nothing", word)
	}
	// The two are cached apart, so the German lines are still German.
	if lines2 := layoutStrings(t, f, word, de); !slices.Equal(lines, lines2) {
		t.Errorf("the language cache mixed hyphenators: %q then %q", lines, lines2)
	}
}

// hyphenated writes a word with a hyphen at each of its break points.
func hyphenated(h *Hyphenator, word string) string {
	r := []rune(word)
	var b strings.Builder
	prev := 0
	for _, p := range h.Hyphenate(word) {
		b.WriteString(string(r[prev:p]))
		b.WriteByte('-')
		prev = p
	}
	b.WriteString(string(r[prev:]))
	return b.String()
}

func mustHyphenator(t *testing.T, lang string) *Hyphenator {
	t.Helper()
	h, err := HyphenatorFor(lang)
	if err != nil {
		t.Fatalf("HyphenatorFor(%q): %v", lang, err)
	}
	return h
}
