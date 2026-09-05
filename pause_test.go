package bunyip

import (
	"testing"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/platform"
)

// newPauseLoop builds the part of the loop the pause logic touches: the
// config, the focus and visibility flags, an input state for the focus
// event to clear, and a mixer to silence.
func newPauseLoop(cfg Config) *loop {
	return &loop{cfg: cfg, input: hook.NewInput(),
		ctx: &Context{Audio: audio.NewMixer(audioRate), focused: true, visible: true}}
}

func TestPausedReadsBothFlags(t *testing.T) {
	cases := []struct {
		name                string
		unfocused, hidden   bool
		hasFocus, canBeSeen bool
		want                bool
	}{
		{"neither setting on, hidden and unfocused", false, false, false, false, false},
		{"PauseUnfocused with focus", true, false, true, true, false},
		{"PauseUnfocused without focus", true, false, false, true, true},
		{"PauseUnfocused while merely hidden", true, false, true, false, false},
		{"PauseHidden while hidden", false, true, true, false, true},
		{"PauseHidden while unfocused", false, true, false, true, false},
		{"both, either one true", true, true, false, true, true},
		{"both, neither true", true, true, true, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := newPauseLoop(Config{PauseUnfocused: c.unfocused, PauseHidden: c.hidden})
			l.ctx.focused, l.ctx.visible = c.hasFocus, c.canBeSeen
			if got := l.paused(); got != c.want {
				t.Errorf("paused = %v, want %v", got, c.want)
			}
		})
	}
}

// A focus event must not undo a pause the game put on its own mixer when
// the loop is not the one deciding focus.
func TestApplyPauseLeavesTheGamesOwnPauseAlone(t *testing.T) {
	l := newPauseLoop(Config{PauseHidden: true})
	l.ctx.Audio.SetPaused(true) // the game opened its own pause menu
	l.handleEvents([]platform.Event{{Kind: platform.EventFocus, Focused: false}})
	if !l.ctx.Audio.Paused() {
		t.Error("losing focus resumed the mixer the game had paused")
	}
	l.handleEvents([]platform.Event{{Kind: platform.EventFocus, Focused: true}})
	if !l.ctx.Audio.Paused() {
		t.Error("regaining focus resumed the mixer the game had paused")
	}
}

// With the matching flag on, the loop silences and resumes the mixer.
func TestApplyPauseFollowsVisibility(t *testing.T) {
	l := newPauseLoop(Config{PauseHidden: true})
	l.handleEvents([]platform.Event{{Kind: platform.EventVisible, Visible: false}})
	if !l.ctx.Audio.Paused() {
		t.Fatal("the mixer kept playing while the window was hidden")
	}
	if !l.paused() {
		t.Error("paused = false while the window is hidden and PauseHidden is on")
	}
	l.handleEvents([]platform.Event{{Kind: platform.EventVisible, Visible: true}})
	if l.ctx.Audio.Paused() {
		t.Error("the mixer stayed silent after the window came back")
	}
}

// Neither setting on means the loop never writes the mixer at all.
func TestApplyPauseIsSilentWithoutEitherSetting(t *testing.T) {
	l := newPauseLoop(Config{})
	l.ctx.Audio.SetPaused(true)
	l.handleEvents([]platform.Event{
		{Kind: platform.EventVisible, Visible: false},
		{Kind: platform.EventFocus, Focused: false},
		{Kind: platform.EventVisible, Visible: true},
		{Kind: platform.EventFocus, Focused: true},
	})
	if !l.ctx.Audio.Paused() {
		t.Error("the loop wrote the mixer with neither PauseUnfocused nor PauseHidden set")
	}
}

// Both settings at once must not have one undo the other: a window that
// is hidden and unfocused stays paused until both come back.
func TestApplyPauseWithBothSettings(t *testing.T) {
	l := newPauseLoop(Config{PauseUnfocused: true, PauseHidden: true})
	l.handleEvents([]platform.Event{
		{Kind: platform.EventFocus, Focused: false},
		{Kind: platform.EventVisible, Visible: false},
	})
	if !l.ctx.Audio.Paused() {
		t.Fatal("the mixer kept playing while the window was hidden and unfocused")
	}
	l.handleEvents([]platform.Event{{Kind: platform.EventFocus, Focused: true}})
	if !l.ctx.Audio.Paused() {
		t.Error("regaining focus resumed the mixer while the window was still hidden")
	}
	l.handleEvents([]platform.Event{{Kind: platform.EventVisible, Visible: true}})
	if l.ctx.Audio.Paused() {
		t.Error("the mixer stayed silent after both focus and visibility came back")
	}
}
