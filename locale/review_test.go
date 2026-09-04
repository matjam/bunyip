package locale

import "testing"

// Turkish, Hungarian and Georgian distinguish one from other.
func TestOneCategoryLanguages(t *testing.T) {
	for _, lang := range []string{"tr", "hu", "ka"} {
		if c := Plural(lang, 1); c != One {
			t.Errorf("Plural(%q, 1) = %q, want one", lang, c)
		}
		if c := Plural(lang, 2); c != Other {
			t.Errorf("Plural(%q, 2) = %q, want other", lang, c)
		}
	}
}
