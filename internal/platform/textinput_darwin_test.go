package platform

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/ebitengine/purego/objc"
)

// AppKit insists on the main thread for windows, and the package init
// locks it, so the window-driving part of the test runs from TestMain
// before the test goroutines start; TestTextInput reports its outcome.
var textInputResult, wakeResult error

// testApp is the one App the test binary makes: NewApp registers
// Objective-C classes, which cannot be registered twice.
var testApp = sync.OnceValues(NewApp)

// mainCalls carries work to the main thread, which TestMain keeps
// serving while the tests and benchmarks run on other goroutines.
var mainCalls = make(chan func())

// onMain runs f on the main thread and returns when it has finished.
func onMain(f func()) {
	done := make(chan struct{})
	mainCalls <- func() {
		defer close(done)
		f()
	}
	<-done
}

func TestMain(m *testing.M) {
	flag.Parse()
	if wantsWindowTests() {
		textInputResult = runTextInput()
	}
	code := make(chan int)
	go func() { code <- m.Run() }()
	for {
		select {
		case f := <-mainCalls:
			f()
		case c := <-code:
			os.Exit(c)
		}
	}
}

// wantsWindowTests reports whether -test.run selects TestTextInput or
// TestWake, the tests that put a window on screen. A benchmark run with
// -run '^$' opens no window.
func wantsWindowTests() bool {
	f := flag.Lookup("test.run")
	if f == nil || f.Value.String() == "" {
		return true
	}
	top, _, _ := strings.Cut(f.Value.String(), "/")
	re, err := regexp.Compile(top)
	if err != nil {
		return true
	}
	return re.MatchString("TestTextInput") || re.MatchString("TestWake")
}

// TestWake reports the outcome of runWake, which TestMain drove through
// the shared window.
func TestWake(t *testing.T) {
	if wakeResult != nil {
		t.Fatal(wakeResult)
	}
}

// TestTextInput drives the BunyipView the way AppKit does: a key event
// through keyDown: must come out as text via the input method, and marked
// text must surface as a composition that clears when committed.
func TestTextInput(t *testing.T) {
	if textInputResult != nil {
		t.Fatal(textInputResult)
	}
}

func runTextInput() error {
	app, err := testApp()
	if err != nil {
		return nil // no window system; nothing to test
	}
	w, err := app.NewWindow(Config{Title: "text input test", Width: 200, Height: 100})
	if err != nil {
		return err
	}
	defer w.Close()
	wakeResult = runWake(app, w)
	c := app.c
	app.pending = app.pending[:0]

	// A plain "a" press: interpretKeyEvents: turns it into insertText:.
	keyEvent := objc.RegisterName("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:")
	nsEvent := objc.GetClass("NSEvent")
	ev := objc.ID(nsEvent).Send(keyEvent, uint(nsEventTypeKeyDown), nsPoint{}, uint(0), float64(0),
		int(0), objc.ID(0), c.nsString("a"), c.nsString("a"), false, uint16(0))
	if ev == 0 {
		return fmt.Errorf("NSEvent construction failed")
	}
	w.view.Send(objc.RegisterName("keyDown:"), ev)
	if got := chars(app.pending); got != "a" {
		return fmt.Errorf("keyDown produced %q, want %q (events %v)", got, "a", kinds(app.pending))
	}

	// Composition: marked text arrives as EventCompose, and committing it
	// clears the composition before the text lands.
	app.pending = app.pending[:0]
	setMarked := objc.RegisterName("setMarkedText:selectedRange:replacementRange:")
	w.view.Send(setMarked, c.nsString("か"), nsRange{}, nsRange{Location: nsNotFound})
	if len(app.pending) != 1 || app.pending[0].Kind != EventCompose || app.pending[0].Text != "か" {
		return fmt.Errorf("setMarkedText produced %v", app.pending)
	}
	if !objc.Send[bool](w.view, objc.RegisterName("hasMarkedText")) {
		return fmt.Errorf("hasMarkedText false during composition")
	}
	if r := objc.Send[nsRange](w.view, objc.RegisterName("markedRange")); r.Location != 0 || r.Length != 1 {
		return fmt.Errorf("markedRange = %+v, want {0 1}", r)
	}
	app.pending = app.pending[:0]
	w.view.Send(objc.RegisterName("insertText:replacementRange:"), c.nsString("漢字"), nsRange{Location: nsNotFound})
	if k := kinds(app.pending); len(k) != 3 || k[0] != EventCompose || app.pending[0].Text != "" || chars(app.pending) != "漢字" {
		return fmt.Errorf("commit produced %v %q", k, chars(app.pending))
	}
	return nil
}

func chars(events []Event) string {
	s := ""
	for _, e := range events {
		if e.Kind == EventChar {
			s += string(e.Rune)
		}
	}
	return s
}

func kinds(events []Event) []EventKind {
	var k []EventKind
	for _, e := range events {
		k = append(k, e.Kind)
	}
	return k
}
