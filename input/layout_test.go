package input

import (
	"github.com/matjam/bunyip/internal/hook"
	"slices"
	"testing"
)

func TestKeyboardLayoutSymbolsAndReverseLookup(t *testing.T) {
	l := KeyboardLayout{Name: "non-US"}
	l.Keys[KeyQ] = KeyDescription{Label: "A", Symbol: TextSymbol("a")}
	l.Keys[KeyA] = KeyDescription{Label: "Й", Symbol: TextSymbol("й")}
	l.Keys[Key1] = KeyDescription{Label: "1", Symbol: TextSymbol("1")}
	l.Keys[KeyKeypad1] = l.Keys[Key1]
	l.Keys[KeyEnter] = KeyDescription{Label: "Entrée", Symbol: "key:Enter"}
	l.Keys[KeyGrave] = KeyDescription{Label: "´", Symbol: "dead:acute"}
	if l.Label(KeyQ) != "A" || l.Symbol(KeyQ) != TextSymbol("a") || !slices.Equal(l.KeysFor(TextSymbol("1")), []Key{Key1, KeyKeypad1}) || !slices.Equal(l.KeysFor(TextSymbol("й")), []Key{KeyA}) {
		t.Fatal("layout mapping lost non-US/duplicate keys")
	}
	if TextSymbol("key:Enter") == l.Symbol(KeyEnter) || TextSymbol("dead:acute") == l.Symbol(KeyGrave) {
		t.Fatal("symbol namespaces collide")
	}
	if l.KeysFor("") != nil || l.Symbol(KeyCount) != "" || l.Label(KeyZ) != KeyZ.String() || l.Symbol(KeyZ) != "" {
		t.Fatal("unknown mapping invented a logical key")
	}
	copy := l
	copy.Keys[KeyQ] = KeyDescription{}
	if l.Label(KeyQ) != "A" {
		t.Fatal("snapshot aliases another copy")
	}
}

func TestGamepadInfoFeedsCopiesAndClearsOnDisconnect(t *testing.T) {
	s := &State{}
	d := driver{s}
	d.FeedGamepad(0, true, "native", [hook.GamepadButtonCount]bool{}, [hook.GamepadAxisCount]float32{})
	i := hook.GamepadInfo{Name: "native", Backend: "test", VendorID: 0x1234, ProductID: 7}
	i.Buttons[ButtonA] = true
	i.Axes[AxisLeftX] = true
	d.FeedGamepadInfo(0, i)
	g := s.Gamepad(0)
	if !g.Info.HasButton(ButtonA) || g.Info.HasButton(ButtonB) || !g.Info.HasAxis(AxisLeftX) || g.Info.VendorID != 0x1234 || g.Info.HasButton(GamepadButtonCount) {
		t.Fatalf("metadata=%+v", g.Info)
	}
	g.Info.Buttons[ButtonA] = false
	if !s.Gamepad(0).Info.HasButton(ButtonA) {
		t.Fatal("metadata snapshot aliases state")
	}
	d.FeedGamepad(0, false, "", [hook.GamepadButtonCount]bool{}, [hook.GamepadAxisCount]float32{})
	d.FeedGamepadInfo(0, i)
	if s.Gamepad(0).Info != (GamepadInfo{}) {
		t.Fatal("disconnected controller retained metadata")
	}
}
