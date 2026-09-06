package engine

import (
	"errors"
	"testing"

	"github.com/matjam/bunyip/input"
)

type layoutTestApp struct {
	*headlessApp
	layout input.KeyboardLayout
	err    error
}

func (a *layoutTestApp) KeyboardLayout() (input.KeyboardLayout, error) { return a.layout, a.err }

func TestContextKeyboardLayoutDispatchAndHeadless(t *testing.T) {
	ctx := &Context{app: &headlessApp{}}
	if _, err := ctx.KeyboardLayout(); !errors.Is(err, ErrUnsupported) {
		t.Fatal("headless layout must report unsupported")
	}
	a := &layoutTestApp{headlessApp: &headlessApp{}, layout: input.KeyboardLayout{Name: "native"}}
	a.layout.Keys[input.KeyW] = input.KeyDescription{Label: "Z", Symbol: input.TextSymbol("z")}
	ctx.app = a
	l, err := ctx.KeyboardLayout()
	if err != nil || l.Label(input.KeyW) != "Z" {
		t.Fatalf("layout=%+v err=%v", l, err)
	}
	l.Keys[input.KeyW] = input.KeyDescription{}
	if a.layout.Label(input.KeyW) != "Z" {
		t.Fatal("returned layout aliases native snapshot")
	}
	a.err = errors.New("layout unavailable")
	if _, err := ctx.KeyboardLayout(); !errors.Is(err, a.err) {
		t.Fatal("native error not preserved")
	}
}
