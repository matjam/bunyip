// Package console is the engine's in-game debug console: a drop-down
// command line over the top of the view, and a window of panels that
// show what every part of the engine is doing.
//
// To turn it on, set Config.Console and draw it last:
//
//	func (g *game) Draw(ctx *bunyip.Context) error {
//		// ... the game's own drawing and interface ...
//		return ctx.Console.Draw(ctx)
//	}
//
// The engine makes the console, attaches what it owns (the graphics
// context, the mixer, the input state and the frame timings) and hands
// it to the game on Context.Console. Draw is the game's call because
// draw order belongs to the game: the console has to be the last thing
// drawn so it sits above the game's own interface. A game that wants a
// console of its own shape calls New instead and drives it the same way.
//
// # The console
//
// The backquote key opens and closes the drop-down (Options.Key chooses
// another). While it is open it takes the keyboard: the game asks
// Console.Open and skips its own key handling for that update. The panel
// holds the log and a command line with history on the up and down
// arrows, completion on Tab, and paging with PageUp and PageDown.
//
//	func (g *game) Update(ctx *bunyip.Context) error {
//		if ctx.Console.Open() {
//			return nil // the console has the keyboard
//		}
//		...
//	}
//
// Commands are registered with Register and read their arguments as
// strings. The built-in commands are help, echo, clear, quit,
// screenshot, exec, bind, unbind, binds, fps, stats, log, timescale,
// panels, set, get and vars.
//
// Variables are registered with Float, Int, Bool, String or Var and
// edited with set and get, so a game exposes its tunables without
// writing commands for them:
//
//	ctx.Console.Float("player.speed", &g.speed, "how fast the player runs")
//
// # The panels
//
// F4, or the panels command, opens a resizable window of tabs: Engine
// (frame timings, profile scopes and draw counts), Graphics (the live
// post-processing settings and the GPU resources), Entities (the
// attached worlds, their entities, components, resources and systems),
// Physics (bodies, contacts, joints, solver settings and collider
// drawing), Audio (voices, buses and the listener), Input (keys, pointer,
// gamepads and action maps) and Services (whatever the game attached).
//
// The engine attaches what it owns. A game attaches its own with one
// call each: Attach for an entity world, AttachActions for an action
// map, AttachInfo for a line of text in the Services tab and AttachLinks
// for a network connection's link statistics.
//
// # Logging
//
// With Config.Console the engine tees the log through the console, so
// every slog record shows in the panel as well as wherever it was going.
// The log command sets the lowest level captured from then on. A console
// a game builds itself installs the tee with Handler.
//
// Every method works on a nil Console and does nothing, so the calls a
// game makes stay put when Config.Console is off and the field is nil.
package console

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/ui"
)

// consoleLayer is the sprite layer the console and its panels draw on,
// above the engine's debug overlay so neither hides the other.
const consoleLayer = 1<<20 + 1

// Options configure a console. Every zero value means the default noted.
type Options struct {
	// Key opens and closes the drop-down; zero means the backquote key.
	Key input.Key
	// PanelKey opens and closes the window of debug panels; zero means F4.
	PanelKey input.Key
	// Height is the fraction of the view the drop-down covers, 0 to 1;
	// zero means 0.4.
	Height float32
	// Lines is how many lines of output are kept; zero means 1000.
	Lines int
	// Level is the lowest log level captured by Handler; zero means Info.
	// The log command changes it while the game runs.
	Level slog.Level
	// Font draws the console; nil makes one from the built-in font at 13
	// view units the first time the console draws.
	Font *gfx.Font
	// Theme styles the panels; nil uses the dark theme over Font.
	Theme *ui.Theme
	// Read is where exec reads scripts from: pass an asset.FS's Read
	// method to run scripts out of a pack file. Nil reads the file system.
	Read func(name string) ([]byte, error)
}

// Console is one debug console: its output, its commands and variables,
// and the panels. Register commands and variables from any goroutine;
// draw it from the goroutine that runs the loop.
// Command callbacks and registered variables are accessed on that loop
// goroutine. Registration protects the registry, not the pointed-to
// game values; synchronize any access from other goroutines. Attachments,
// open state, Run and Draw also belong to the loop goroutine.
type Console struct {
	opts Options

	mu    sync.Mutex // guards the output buffer and the log level
	lines []line
	level slog.Level

	// The command line and its history.
	text    string
	caret   int
	history []string
	histAt  int // index into history while browsing, len(history) when not
	scroll  int // lines scrolled back from the newest

	open      bool
	toggled   bool // the open state changed this frame, so the key's rune is dropped
	blink     int
	commands  map[string]*Command
	order     []string // command names in registration order
	vars      map[string]varEntry
	varOrder  []string
	bindings  map[input.Key]string
	pending   []string // the bound commands this frame fired, run after the walk
	ownedFont bool

	font  *gfx.Font
	theme ui.Theme
	ui    *ui.Context

	// frame is the last frame Draw was given, which the commands read.
	frame Frame

	panels panelState
	attach attachments
}

// line is one line of output, with the level it came from so the panel
// can colour it. A line that is not from the log has level zero.
type line struct {
	text  string
	level slog.Level
	log   bool
}

// New makes a console. Nothing is drawn until Draw is called, and the
// font and interface context are made the first time it is.
func New(opts Options) *Console {
	c := &Console{opts: opts, level: opts.Level, commands: map[string]*Command{},
		vars: map[string]varEntry{}, bindings: map[input.Key]string{}}
	if c.opts.Key == input.KeyUnknown {
		c.opts.Key = input.KeyGrave
	}
	if c.opts.PanelKey == input.KeyUnknown {
		c.opts.PanelKey = input.KeyF4
	}
	if c.opts.Height <= 0 {
		c.opts.Height = 0.4
	}
	if c.opts.Lines <= 0 {
		c.opts.Lines = 1000
	}
	c.font = opts.Font
	c.registerBuiltins()
	return c
}

// Open reports whether the drop-down is showing. A game checks it in
// Update and leaves the keyboard alone while it is true.
// Toggle keys are processed in Draw, so Update observes the state from
// the preceding Draw. The debug panels alone do not make Open true.
func (c *Console) Open() bool { return c != nil && c.open }

// SetOpen opens or closes the drop-down.
func (c *Console) SetOpen(open bool) {
	if c == nil {
		return
	}
	if c.open != open {
		c.open = open
		c.toggled = true
	}
}

// Toggle flips the drop-down open or shut, as the console's key does.
func (c *Console) Toggle() {
	if c != nil {
		c.SetOpen(!c.open)
	}
}

// Destroy frees the font the console made for itself. The engine calls
// it before the graphics context goes; a game that passed its own font
// in Options keeps that font.
func (c *Console) Destroy() {
	if c == nil {
		return
	}
	if c.ownedFont && c.font != nil {
		c.font.Destroy()
	}
	c.font, c.ownedFont = nil, false
	c.ui = nil
}

// Print adds a line of output. It is safe to call from any goroutine.
func (c *Console) Print(text string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		c.push(line{text: s})
	}
}

// Printf adds a formatted line of output, as fmt.Sprintf writes it.
func (c *Console) Printf(format string, args ...any) { c.Print(fmt.Sprintf(format, args...)) }

// push appends a line, dropping the oldest once the buffer is full. The
// caller holds the lock.
func (c *Console) push(l line) {
	c.lines = append(c.lines, l)
	if n := len(c.lines) - c.opts.Lines; n > 0 {
		c.lines = append(c.lines[:0], c.lines[n:]...)
	}
}

// Lines returns the output kept, oldest first, for tests and for a game
// that shows the log somewhere of its own.
func (c *Console) Lines() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.lines))
	for i, l := range c.lines {
		out[i] = l.text
	}
	return out
}

// Clear empties the output.
func (c *Console) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.lines = c.lines[:0]
	c.scroll = 0
	c.mu.Unlock()
}

// Host is anything that can describe the frame the console draws in. The
// engine's *bunyip.Context implements it, so a game passes its context
// straight to Draw.
type Host interface {
	ConsoleFrame() Frame
}

// Draw runs one frame of console: the toggle keys, the key bindings, the
// command line while it is open, and the panels. Call it last in the
// game's Draw so the console sits above everything else. It draws
// nothing while the console is closed and no panel window is open.
func (c *Console) Draw(h Host) error {
	if c == nil || h == nil {
		return nil
	}
	f := h.ConsoleFrame()
	if f.Gfx == nil || f.Input == nil {
		return nil
	}
	c.frame = f
	c.toggled = false
	c.sampleFrame(f.Stats)
	c.finishStep()
	c.keys(f)
	c.drawColliders(f)
	if !c.open && !c.panels.open {
		return nil
	}
	if err := c.prepare(f); err != nil {
		return err
	}
	g := f.Gfx
	g.ScreenSpace()
	layer := g.Layer()
	g.SetLayer(consoleLayer)
	defer g.SetLayer(layer)
	// The panels first, then the drop-down over them: the drop-down has
	// the keyboard while it is open, so it is the one in front.
	if c.panels.open {
		c.ui.Clipboard = f.Clipboard
		c.ui.Begin(f.Input, func() { c.drawPanels(f) })
	}
	if c.open {
		c.drawDropDown(f)
	}
	return nil
}

// prepare makes the font and the interface context on first use.
func (c *Console) prepare(f Frame) error {
	if c.font == nil {
		font, err := f.Gfx.NewFont(goregular.TTF, 13, gfx.FontOptions{})
		if err != nil {
			return err
		}
		c.font, c.ownedFont = font, true
	}
	if c.ui == nil {
		if c.opts.Theme != nil {
			c.theme = *c.opts.Theme
			if c.theme.Font == nil {
				c.theme.Font = c.font
			}
		} else {
			c.theme = ui.DarkTheme(c.font)
			c.theme.RowHeight = 20
			c.theme.Padding = 6
			c.theme.Spacing = 4
		}
		c.ui = ui.New(f.Gfx, c.theme)
	}
	return nil
}

// keys handles the toggle keys and, while the console is shut, the key
// bindings. Draw reads the frame's edges, so every press since the last
// frame is seen even when no update ran.
func (c *Console) keys(f Frame) {
	in := f.Input
	if in.KeyPressed(c.opts.Key) {
		c.Toggle()
		if c.open {
			c.scroll = 0
		}
	}
	if in.KeyPressed(c.opts.PanelKey) {
		c.panels.open = !c.panels.open
	}
	if c.open {
		c.editLine(f)
		return
	}
	// The lines to run are gathered first, because a bound command may
	// be bind or unbind and change the map this walks.
	c.pending = c.pending[:0]
	for key, cmd := range c.bindings {
		if in.KeyPressed(key) {
			c.pending = append(c.pending, cmd)
		}
	}
	for _, cmd := range c.pending {
		c.Run(cmd)
	}
}

// drawDropDown draws the panel, the output and the command line.
func (c *Console) drawDropDown(f Frame) {
	g, th := f.Gfx, c.theme
	h := f.Height * min(c.opts.Height, 1)
	rect := lin.R(0, 0, f.Width, h)
	bg := th.Panel
	bg.A = 0.94
	g.FillRect(rect.X, rect.Y, rect.W, rect.H, bg)
	g.FillRect(rect.X, rect.Y+rect.H-2, rect.W, 2, th.Accent)
	lineH := c.font.LineHeight
	const pad = 6
	inputY := rect.Y + rect.H - lineH - pad - 2
	// The output runs upwards from just above the command line, oldest
	// first, so the newest is always in view.
	c.mu.Lock()
	lines := c.lines
	scroll := min(c.scroll, max(len(lines)-1, 0))
	c.scroll = scroll
	c.mu.Unlock()
	g.Clip(lin.R(rect.X, rect.Y, rect.W, inputY-pad), func() {
		y := inputY - pad - lineH
		opts := gfx.TextOptions{Width: rect.W - 2*pad}
		for i := len(lines) - 1 - scroll; i >= 0 && y > -lineH; i-- {
			l := lines[i]
			_, th2 := c.font.Measure(l.text, opts)
			y -= th2 - lineH
			g.DrawTextBlock(c.font, l.text, rect.X+pad, y, opts, c.lineColor(l))
			y -= lineH
		}
	})
	// The command line, with a caret that blinks while it waits.
	g.FillRect(rect.X+pad/2, inputY-2, rect.W-pad, lineH+4, th.Field)
	prompt := "] "
	pw, _ := c.font.Measure(prompt, gfx.TextOptions{})
	g.DrawText(c.font, prompt, rect.X+pad, inputY, th.TextDim)
	g.Clip(lin.R(rect.X+pad+pw, inputY-2, rect.W-2*pad-pw, lineH+4), func() {
		g.DrawText(c.font, c.text, rect.X+pad+pw, inputY, th.Text)
	})
	c.blink++
	if c.blink%60 < 40 {
		cw, _ := c.font.Measure(string([]rune(c.text)[:c.caret]), gfx.TextOptions{})
		g.FillRect(rect.X+pad+pw+cw, inputY, 1, lineH, th.Text)
	}
	if scroll > 0 {
		note := "-- scrolled back " + strconv.Itoa(scroll) + " lines --"
		nw, _ := c.font.Measure(note, gfx.TextOptions{})
		g.DrawText(c.font, note, rect.X+rect.W-nw-pad, rect.Y+2, th.Accent)
	}
}

// lineColor picks a colour for one output line by the level it came
// from; lines the console printed itself use the plain text colour.
func (c *Console) lineColor(l line) gfx.Color {
	if !l.log {
		return c.theme.Text
	}
	switch {
	case l.level >= slog.LevelError:
		return gfx.RGB(255, 110, 90)
	case l.level >= slog.LevelWarn:
		return gfx.RGB(255, 200, 90)
	case l.level <= slog.LevelDebug:
		return c.theme.TextDim
	}
	return c.theme.Title
}

// editLine applies this frame's keyboard input to the command line.
func (c *Console) editLine(f Frame) {
	in := f.Input
	runes := []rune(c.text)
	c.caret = max(0, min(c.caret, len(runes)))
	set := func(s string, caret int) {
		c.text, runes = s, []rune(s)
		c.caret = max(0, min(caret, len(runes)))
		c.blink = 0
	}
	// The keystroke that opened the console also typed its character, so
	// the first frame's text is dropped.
	if !c.toggled {
		for _, ch := range in.Chars() {
			if ch < ' ' {
				continue
			}
			set(string(runes[:c.caret])+string(ch)+string(runes[c.caret:]), c.caret+1)
		}
	}
	pressed := func(k input.Key) bool { return in.KeyPressed(k) || in.KeyRepeated(k) }
	switch {
	case pressed(input.KeyBackspace):
		if c.caret > 0 {
			set(string(runes[:c.caret-1])+string(runes[c.caret:]), c.caret-1)
		}
	case pressed(input.KeyDelete):
		if c.caret < len(runes) {
			set(string(runes[:c.caret])+string(runes[c.caret+1:]), c.caret)
		}
	case pressed(input.KeyLeft):
		c.caret = max(0, c.caret-1)
	case pressed(input.KeyRight):
		c.caret = min(len(runes), c.caret+1)
	case in.KeyPressed(input.KeyHome):
		c.caret = 0
	case in.KeyPressed(input.KeyEnd):
		c.caret = len(runes)
	case pressed(input.KeyUp):
		c.browse(-1)
	case pressed(input.KeyDown):
		c.browse(1)
	case pressed(input.KeyPageUp):
		c.scroll += 5
	case pressed(input.KeyPageDown):
		c.scroll = max(0, c.scroll-5)
	case in.KeyPressed(input.KeyTab):
		c.complete()
	case in.KeyPressed(input.KeyEnter):
		c.submit()
	case in.KeyPressed(input.KeyEscape):
		c.SetOpen(false)
	}
	if _, dy := in.Scroll(); dy != 0 {
		c.scroll = max(0, c.scroll+int(dy))
	}
}

// browse steps through the command history; going past the newest
// entry leaves the line empty, ready for a fresh command.
func (c *Console) browse(dir int) {
	if len(c.history) == 0 {
		return
	}
	c.histAt = max(0, min(c.histAt+dir, len(c.history)))
	if c.histAt == len(c.history) {
		c.text, c.caret = "", 0
		return
	}
	c.text = c.history[c.histAt]
	c.caret = len([]rune(c.text))
}

// submit runs the line, records it in the history and clears it.
func (c *Console) submit() {
	text := strings.TrimSpace(c.text)
	c.text, c.caret, c.scroll = "", 0, 0
	if text == "" {
		return
	}
	if n := len(c.history); n == 0 || c.history[n-1] != text {
		c.history = append(c.history, text)
	}
	c.histAt = len(c.history)
	c.Print("] " + text)
	c.Run(text)
}
