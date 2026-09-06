package platform

import (
	"github.com/matjam/bunyip/input"
	"strconv"
	"unicode"
	"unicode/utf8"
)

func keyDescription(key input.Key, label, text string, dead bool) input.KeyDescription {
	d := input.KeyDescription{Label: label}
	if dead {
		if text != "" {
			d.Symbol = input.KeySymbol("dead:" + text)
			if d.Label == "" {
				d.Label = text
			}
		}
		return d
	}
	printable := text != "" && utf8.ValidString(text)
	for _, r := range text {
		if unicode.IsControl(r) || r >= 0xf700 && r <= 0xf8ff {
			printable = false
		}
	}
	if printable {
		d.Symbol = input.TextSymbol(text)
		if d.Label == "" {
			d.Label = text
		}
	} else {
		if d.Label == text {
			d.Label = ""
		}
		if name := translatedControlName(text); name != "" {
			d.Symbol = input.KeySymbol("key:" + name)
			if d.Label == "" {
				d.Label = name
			}
		}
		if d.Label == "" {
			d.Label = key.String()
		}
	}
	return d
}

func translatedControlName(text string) string {
	r, size := utf8.DecodeRuneInString(text)
	if size != len(text) || size == 0 {
		return ""
	}
	if r >= 0xf704 && r <= 0xf726 {
		return "F" + strconv.Itoa(int(r-0xf704)+1)
	}
	// Carbon control characters and AppKit function-key Unicode values.
	return map[rune]string{3: "Enter", 8: "Backspace", 9: "Tab", 13: "Enter", 27: "Escape", 28: "Left", 29: "Right", 30: "Up", 31: "Down", 127: "Delete", 0xf700: "Up", 0xf701: "Down", 0xf702: "Left", 0xf703: "Right", 0xf727: "Insert", 0xf728: "Delete", 0xf729: "Home", 0xf72a: "Clear", 0xf72b: "End", 0xf72c: "PageUp", 0xf72d: "PageDown", 0xf72e: "PrintScreen", 0xf72f: "ScrollLock", 0xf730: "Pause", 0xf735: "Menu", 0xf739: "Clear"}[r]
}
