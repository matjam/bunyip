// Package locale translates a game's strings. It provides tables of
// messages by language with placeholders and plural forms, a fallback
// chain so a half-translated language falls back to a configured source rather than
// showing keys, and the plural rules of the common languages.
//
// A Bundle holds one Table per language, loaded from JSON files a
// translator edits:
//
//	{
//	  "menu.play": "Play",
//	  "hud.gold": "{n} gold",
//	  "inv.arrows": {"one": "{n} arrow", "other": "{n} arrows"}
//	}
//
// To translate a string, get a language's Translator from the bundle and
// call T with a key and the values for its placeholders. A plural entry
// picks its form from the value named "n", or from the first number
// given. Keys missing from a language come from the fallbacks in order,
// and a key missing everywhere returns itself in brackets so it is
// visible and can be fixed. Right-to-left layout of the interface is the
// game's responsibility; the text itself shapes correctly through gfx.
// Bundles and tables have no internal locks. Finish loading them before
// sharing translators, or synchronize mutations with all readers.
package locale

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Category is a plural form a language distinguishes.
type Category string

// Plural-category names used in translation JSON. Which counts belong
// to a category depends on the language; Other is the fallback form.
const (
	Zero  Category = "zero"
	One   Category = "one"
	Two   Category = "two"
	Few   Category = "few"
	Many  Category = "many"
	Other Category = "other"
)

// Plural returns the plural category of a count in a language, by the
// language's rules ("en", "fr", "ru", "ar", ...; a region suffix such as
// "pt-BR" is ignored except where it matters). Unknown languages use
// the one/other rule.
func Plural(lang string, n float64) Category {
	base := strings.ToLower(lang)
	region := ""
	if i := strings.IndexAny(base, "-_"); i >= 0 {
		base, region = base[:i], base[i+1:]
	}
	i := int64(n)
	whole := float64(i) == n
	mod := func(m int64) int64 { return ((i % m) + m) % m }
	switch base {
	case "ja", "zh", "ko", "vi", "th", "id", "ms":
		// Turkish, Hungarian and Georgian are not here: CLDR gives them
		// one and other like English.
		return Other
	case "fr", "hy", "kab":
		if n >= 0 && n < 2 {
			return One
		}
		return Other
	case "pt":
		if region == "" || region == "br" {
			if n >= 0 && n < 2 {
				return One
			}
			return Other
		}
	case "ru", "uk", "be":
		if !whole {
			return Other
		}
		switch {
		case mod(10) == 1 && mod(100) != 11:
			return One
		case mod(10) >= 2 && mod(10) <= 4 && (mod(100) < 12 || mod(100) > 14):
			return Few
		default:
			return Many
		}
	case "pl":
		if !whole {
			return Other
		}
		switch {
		case i == 1:
			return One
		case mod(10) >= 2 && mod(10) <= 4 && (mod(100) < 12 || mod(100) > 14):
			return Few
		default:
			return Many
		}
	case "cs", "sk":
		if !whole {
			return Many
		}
		switch {
		case i == 1:
			return One
		case i >= 2 && i <= 4:
			return Few
		default:
			return Other
		}
	case "ar":
		if !whole {
			return Other
		}
		switch {
		case i == 0:
			return Zero
		case i == 1:
			return One
		case i == 2:
			return Two
		case mod(100) >= 3 && mod(100) <= 10:
			return Few
		case mod(100) >= 11 && mod(100) <= 99:
			return Many
		default:
			return Other
		}
	case "he", "iw":
		if !whole {
			return Other
		}
		switch {
		case i == 1:
			return One
		case i == 2:
			return Two
		default:
			return Other
		}
	case "lv":
		if !whole {
			return Other
		}
		switch {
		case mod(10) == 0 || (mod(100) >= 11 && mod(100) <= 19):
			return Zero
		case mod(10) == 1 && mod(100) != 11:
			return One
		default:
			return Other
		}
	case "ga":
		if !whole {
			return Other
		}
		switch {
		case i == 1:
			return One
		case i == 2:
			return Two
		case i >= 3 && i <= 6:
			return Few
		case i >= 7 && i <= 10:
			return Many
		default:
			return Other
		}
	}
	if whole && i == 1 {
		return One
	}
	return Other
}

// Table is one language's messages.
type Table struct {
	Lang  string // language tag used for plural rules and Bundle lookup
	plain map[string]string
	forms map[string]map[Category]string
}

// NewTable makes an empty table for a language.
func NewTable(lang string) *Table {
	return &Table{Lang: lang, plain: map[string]string{}, forms: map[string]map[Category]string{}}
}

// ParseTable reads a language's messages from JSON: a string per key, or
// an object of plural forms ("one", "other", ...). Keys may be nested
// objects, which flatten with dots: {"menu": {"play": "Play"}} is
// "menu.play".
func ParseTable(lang string, data []byte) (*Table, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("locale: %s: %w", lang, err)
	}
	t := NewTable(lang)
	if err := t.addRaw("", raw); err != nil {
		return nil, err
	}
	return t, nil
}

var categories = map[string]Category{"zero": Zero, "one": One, "two": Two, "few": Few, "many": Many, "other": Other}

func (t *Table) addRaw(prefix string, raw map[string]json.RawMessage) error {
	for key, val := range raw {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		var s string
		if json.Unmarshal(val, &s) == nil {
			t.plain[full] = s
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(val, &obj); err != nil {
			return fmt.Errorf("locale: %s: %s is neither a string nor an object", t.Lang, full)
		}
		// An object of plural forms, or a nested group of keys.
		plural := len(obj) > 0
		for k := range obj {
			if _, ok := categories[k]; !ok {
				plural = false
				break
			}
		}
		if !plural {
			if err := t.addRaw(full, obj); err != nil {
				return err
			}
			continue
		}
		forms := map[Category]string{}
		for k, v := range obj {
			var f string
			if err := json.Unmarshal(v, &f); err != nil {
				return fmt.Errorf("locale: %s: %s.%s is not a string", t.Lang, full, k)
			}
			forms[categories[k]] = f
		}
		t.forms[full] = forms
	}
	return nil
}

// Set adds or replaces a plain message.
func (t *Table) Set(key, message string) { t.plain[key] = message }

// SetPlural adds or replaces a message with plural forms. Include Other
// as a fallback: it is not validated, and when both the selected form
// and Other are absent, lookup chooses an unspecified available form.
// The map is retained, not copied. A plain entry of the same key takes
// precedence until the table is replaced.
func (t *Table) SetPlural(key string, forms map[Category]string) { t.forms[key] = forms }

// Keys lists the table's keys, sorted, for checking a translation's
// coverage against the source language.
func (t *Table) Keys() []string {
	keys := make([]string, 0, len(t.plain)+len(t.forms))
	for k := range t.plain {
		keys = append(keys, k)
	}
	for k := range t.forms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Has reports whether the table has a key.
func (t *Table) Has(key string) bool {
	_, ok := t.plain[key]
	if !ok {
		_, ok = t.forms[key]
	}
	return ok
}

// lookup finds a message, choosing the plural form for n when the key
// has forms.
func (t *Table) lookup(key string, n float64, hasN bool) (string, bool) {
	if s, ok := t.plain[key]; ok {
		return s, true
	}
	forms, ok := t.forms[key]
	if !ok {
		return "", false
	}
	if !hasN {
		n = 1
	}
	if s, ok := forms[Plural(t.Lang, n)]; ok {
		return s, true
	}
	if s, ok := forms[Other]; ok {
		return s, true
	}
	for _, s := range forms {
		return s, true
	}
	return "", false
}

// Bundle holds every language's table and the fallback order.
type Bundle struct {
	tables map[string]*Table
	// Fallbacks are tried in order for keys a language lacks; the
	// source language, usually "en", goes last.
	Fallbacks []string
}

// NewBundle makes an empty bundle with fallbacks, the source language
// last.
func NewBundle(fallbacks ...string) *Bundle {
	return &Bundle{tables: map[string]*Table{}, Fallbacks: fallbacks}
}

// Add puts a table in the bundle, replacing any for its language.
func (b *Bundle) Add(t *Table) { b.tables[strings.ToLower(t.Lang)] = t }

// Load parses JSON for a language and adds it.
func (b *Bundle) Load(lang string, data []byte) error {
	t, err := ParseTable(lang, data)
	if err != nil {
		return err
	}
	b.Add(t)
	return nil
}

// Table returns a language's table, nil when none is loaded.
func (b *Bundle) Table(lang string) *Table { return b.tables[strings.ToLower(lang)] }

// Languages lists the loaded languages, sorted.
func (b *Bundle) Languages() []string {
	langs := make([]string, 0, len(b.tables))
	for l := range b.tables {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

// Missing lists the keys a language lacks that the source language has,
// for a translator's to-do list.
func (b *Bundle) Missing(lang, source string) []string {
	src, dst := b.Table(source), b.Table(lang)
	if src == nil {
		return nil
	}
	var missing []string
	for _, k := range src.Keys() {
		if dst == nil || !dst.Has(k) {
			missing = append(missing, k)
		}
	}
	return missing
}

// Translator translates for one language. Get one with Bundle.For and
// keep it; changing language means asking for another.
type Translator struct {
	b     *Bundle
	chain []string
	lang  string
}

// For returns a translator for a language ("de", "pt-BR"): the language
// itself, then its base language without the region, then the bundle's
// fallbacks.
func (b *Bundle) For(lang string) *Translator {
	l := strings.ToLower(lang)
	chain := []string{l}
	if i := strings.IndexAny(l, "-_"); i >= 0 {
		chain = append(chain, l[:i])
	}
	for _, f := range b.Fallbacks {
		chain = append(chain, strings.ToLower(f))
	}
	return &Translator{b: b, chain: chain, lang: lang}
}

// Lang is the language the translator was made for.
func (t *Translator) Lang() string { return t.lang }

// T translates a key, filling {name} placeholders from args given as
// alternating names and values ("n", 3, "who", "Ada"). A plural entry
// picks its form from the value named "n", or the first number given.
// A missing key returns "[key]".
// Without a numeric argument a plural entry uses the category for 1.
// Unmatched placeholders stay as written; a trailing unpaired argument
// is ignored. Duplicate placeholder names use the last value.
func (t *Translator) T(key string, args ...any) string {
	n, hasN := float64(0), false
	for i := 0; i+1 < len(args); i += 2 {
		name, _ := args[i].(string)
		if f, ok := toFloat(args[i+1]); ok && (name == "n" || !hasN) {
			n, hasN = f, true
		}
	}
	for _, lang := range t.chain {
		tbl := t.b.tables[lang]
		if tbl == nil {
			continue
		}
		if msg, ok := tbl.lookup(key, n, hasN); ok {
			return expand(msg, args)
		}
	}
	return "[" + key + "]"
}

// N is T for the common case of one count: T(key, "n", n).
func (t *Translator) N(key string, n int) string { return t.T(key, "n", n) }

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// argValue finds the value given for a placeholder name in args, which
// are alternating names and values. The args are scanned from the end
// so that a name given twice takes its last value. Scanning costs less
// than building a map for the handful of arguments a message has.
func argValue(args []any, name string) (any, bool) {
	last := len(args) - 2
	if last%2 != 0 {
		last-- // the pairs start at even positions; a trailing odd value is ignored
	}
	for i := last; i >= 0; i -= 2 {
		if s, _ := args[i].(string); s == name {
			return args[i+1], true
		}
	}
	return nil, false
}

// writeValue writes a placeholder's value, avoiding the string fmt.Sprint
// would allocate for the types a game usually passes.
func writeValue(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case string:
		sb.WriteString(x)
	case int:
		writeInt(sb, int64(x))
	case int32:
		writeInt(sb, int64(x))
	case int64:
		writeInt(sb, x)
	case uint:
		writeInt(sb, int64(x))
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	default:
		sb.WriteString(fmt.Sprint(v))
	}
}

func writeInt(sb *strings.Builder, v int64) {
	var buf [20]byte
	sb.Write(strconv.AppendInt(buf[:0], v, 10))
}

// expand replaces {name} with the value's text; unknown names are left
// as written, and "{{" and "}}" write a brace each. args are the
// alternating names and values T was given.
func expand(msg string, args []any) string {
	if !strings.Contains(msg, "{") {
		return msg
	}
	var sb strings.Builder
	sb.Grow(len(msg) + 8)
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if c == '}' && i+1 < len(msg) && msg[i+1] == '}' {
			sb.WriteByte('}')
			i++
			continue
		}
		if c != '{' {
			sb.WriteByte(c)
			continue
		}
		if i+1 < len(msg) && msg[i+1] == '{' {
			sb.WriteByte('{')
			i++
			continue
		}
		end := strings.IndexByte(msg[i:], '}')
		if end < 0 {
			sb.WriteString(msg[i:])
			break
		}
		name := msg[i+1 : i+end]
		if v, ok := argValue(args, name); ok {
			writeValue(&sb, v)
		} else {
			sb.WriteString(msg[i : i+end+1])
		}
		i += end
	}
	return sb.String()
}
