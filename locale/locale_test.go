package locale

import "testing"

func TestPlural(t *testing.T) {
	cases := []struct {
		lang string
		n    float64
		want Category
	}{
		{"en", 1, One}, {"en", 0, Other}, {"en", 2, Other}, {"en", 1.5, Other},
		{"fr", 0, One}, {"fr", 1.5, One}, {"fr", 2, Other},
		{"pt-BR", 0, One}, {"pt-PT", 0, Other},
		{"ru", 1, One}, {"ru", 21, One}, {"ru", 11, Many}, {"ru", 3, Few}, {"ru", 5, Many}, {"ru", 2.5, Other},
		{"pl", 1, One}, {"pl", 22, Few}, {"pl", 12, Many}, {"pl", 5, Many},
		{"cs", 3, Few}, {"cs", 5, Other},
		{"ar", 0, Zero}, {"ar", 2, Two}, {"ar", 7, Few}, {"ar", 15, Many}, {"ar", 100, Other},
		{"ja", 1, Other}, {"zh-Hans", 1, Other},
		{"he", 2, Two}, {"lv", 10, Zero}, {"ga", 4, Few}, {"xx", 1, One},
	}
	for _, c := range cases {
		if got := Plural(c.lang, c.n); got != c.want {
			t.Errorf("Plural(%s, %v) = %s, want %s", c.lang, c.n, got, c.want)
		}
	}
}

func TestBundle(t *testing.T) {
	b := NewBundle("en")
	if err := b.Load("en", []byte(`{
		"menu": {"play": "Play", "quit": "Quit"},
		"hud.gold": "{n} gold",
		"inv.arrows": {"one": "{n} arrow", "other": "{n} arrows"},
		"greet": "Hello, {who}! {{braces}} {unknown}"
	}`)); err != nil {
		t.Fatal(err)
	}
	if err := b.Load("ru", []byte(`{
		"menu.play": "Играть",
		"inv.arrows": {"one": "{n} стрела", "few": "{n} стрелы", "many": "{n} стрел"}
	}`)); err != nil {
		t.Fatal(err)
	}
	en := b.For("en")
	if got := en.T("menu.play"); got != "Play" {
		t.Errorf("menu.play = %q", got)
	}
	if got := en.N("inv.arrows", 1); got != "1 arrow" {
		t.Errorf("one arrow = %q", got)
	}
	if got := en.N("inv.arrows", 5); got != "5 arrows" {
		t.Errorf("five arrows = %q", got)
	}
	if got := en.T("greet", "who", "Ada"); got != "Hello, Ada! {braces} {unknown}" {
		t.Errorf("greet = %q", got)
	}
	if got := en.T("nope"); got != "[nope]" {
		t.Errorf("missing key = %q", got)
	}
	ru := b.For("ru-RU")
	if got := ru.T("menu.play"); got != "Играть" {
		t.Errorf("ru menu.play = %q", got)
	}
	if got := ru.N("inv.arrows", 3); got != "3 стрелы" {
		t.Errorf("ru three arrows = %q", got)
	}
	if got := ru.N("inv.arrows", 21); got != "21 стрела" {
		t.Errorf("ru 21 arrows = %q", got)
	}
	if got := ru.T("menu.quit"); got != "Quit" {
		t.Errorf("ru falls back to %q", got)
	}
	if missing := b.Missing("ru", "en"); len(missing) != 3 || missing[0] != "greet" {
		t.Errorf("missing in ru: %v", missing)
	}
	if langs := b.Languages(); len(langs) != 2 || langs[0] != "en" {
		t.Errorf("languages %v", langs)
	}
	if _, err := ParseTable("en", []byte(`{"a": 5}`)); err == nil {
		t.Error("a number parsed as a message")
	}
}
