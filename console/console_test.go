package console

import (
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/bunyip/input"
)

// TestCommands runs the built-in commands and a registered one, and
// checks that an unknown command is reported as an error.
func TestCommands(t *testing.T) {
	c := New(Options{})
	cases := []struct {
		name string
		line string
		want string
	}{
		{"echo", "echo hello world", "hello world"},
		{"quoted argument", `echo "one two"`, "one two"},
		{"help of one command", "help echo", "echo <text...>"},
		{"unknown command", "wibble", `unknown command "wibble"`},
		{"unknown variable", "get nothing", `no variable "nothing"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c.Clear()
			c.Run(tc.line)
			out := strings.Join(c.Lines(), "\n")
			if !strings.Contains(out, tc.want) {
				t.Errorf("%q printed %q, want it to hold %q", tc.line, out, tc.want)
			}
		})
	}
	// A registered command runs with the arguments after its name.
	var got []string
	c.Register("give", "give <item>: hand over an item", func(args []string) (string, error) {
		got = args
		return "gave " + strings.Join(args, " "), nil
	})
	c.Clear()
	c.Run("give sword 2")
	if len(got) != 2 || got[0] != "sword" || got[1] != "2" {
		t.Errorf("command got args %q, want [sword 2]", got)
	}
	if out := strings.Join(c.Lines(), "\n"); !strings.Contains(out, "gave sword 2") {
		t.Errorf("output %q missing the command's result", out)
	}
	if out := strings.Join(c.Lines(), "\n"); strings.Contains(out, "help") {
		t.Errorf("output %q holds more than the command printed", out)
	}
}

// TestVariables registers each kind of variable, changes it with set and
// reads it back with get.
func TestVariables(t *testing.T) {
	c := New(Options{})
	var (
		speed float32 = 1.5
		count         = 3
		on            = false
		name          = "player"
	)
	c.Float("player.speed", &speed, "how fast the player runs")
	c.Int("player.lives", &count, "")
	c.Bool("player.invincible", &on, "")
	c.String("player.name", &name, "")
	cases := []struct {
		name, line, want string
		check            func() bool
	}{
		{"float", "set player.speed 4.25", "player.speed = 4.25", func() bool { return speed == 4.25 }},
		{"int", "set player.lives 7", "player.lives = 7", func() bool { return count == 7 }},
		{"bool", "set player.invincible on", "player.invincible = true", func() bool { return on }},
		{"string", "set player.name kate", "player.name = kate", func() bool { return name == "kate" }},
		{"bad number", "set player.speed fast", `"fast" is not a number`, func() bool { return speed == 4.25 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c.Clear()
			c.Run(tc.line)
			out := strings.Join(c.Lines(), "\n")
			if !strings.Contains(out, tc.want) {
				t.Errorf("%q printed %q, want it to hold %q", tc.line, out, tc.want)
			}
			if !tc.check() {
				t.Errorf("%q did not change the variable", tc.line)
			}
		})
	}
	c.Clear()
	c.Run("get player.speed")
	if out := strings.Join(c.Lines(), "\n"); !strings.Contains(out, "player.speed = 4.25") {
		t.Errorf("get printed %q", out)
	}
	c.Clear()
	c.Run("vars player.")
	out := strings.Join(c.Lines(), "\n")
	for _, want := range []string{"player.speed", "player.lives", "player.invincible", "player.name",
		"how fast the player runs"} {
		if !strings.Contains(out, want) {
			t.Errorf("vars printed %q, missing %q", out, want)
		}
	}
}

// TestCompletion completes command names on the first word and variable
// names after set.
func TestCompletion(t *testing.T) {
	c := New(Options{})
	var speed, spin float32
	c.Float("player.speed", &speed, "")
	c.Float("player.spin", &spin, "")
	cases := []struct {
		name, typed, want string
	}{
		{"one command", "scre", "screenshot "},
		{"several commands", "s", "s"},
		{"variable after set", "set player.spe", "set player.speed "},
		{"common prefix of variables", "set player.sp", "set player.sp"},
		{"nothing to complete", "zzz", "zzz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c.text = tc.typed
			c.caret = len(c.text)
			c.complete()
			if c.text != tc.want {
				t.Errorf("completing %q gave %q, want %q", tc.typed, c.text, tc.want)
			}
			if c.caret != len([]rune(c.text)) {
				t.Errorf("caret at %d, want the end of %q", c.caret, c.text)
			}
		})
	}
	// Several matches list what they are.
	c.Clear()
	c.text, c.caret = "set player.sp", len("set player.sp")
	c.complete()
	if out := strings.Join(c.Lines(), "\n"); !strings.Contains(out, "player.speed") || !strings.Contains(out, "player.spin") {
		t.Errorf("ambiguous completion printed %q, want both names", out)
	}
}

// TestLogCapture checks that records logged through the console's
// handler appear in its output, and that the level command filters them.
func TestLogCapture(t *testing.T) {
	c := New(Options{})
	log := slog.New(c.Handler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	log.Info("loaded the level", "name", "cave", "seconds", 1.5)
	log.Debug("this is below the level")
	out := strings.Join(c.Lines(), "\n")
	if !strings.Contains(out, "loaded the level") {
		t.Errorf("output %q missing the record", out)
	}
	if !strings.Contains(out, "name=cave") || !strings.Contains(out, "seconds=1.5") {
		t.Errorf("output %q missing the record's attributes", out)
	}
	if strings.Contains(out, "below the level") {
		t.Errorf("output %q holds a record below the console's level", out)
	}
	c.Run("log debug")
	log.Debug("now it shows")
	if out := strings.Join(c.Lines(), "\n"); !strings.Contains(out, "now it shows") {
		t.Errorf("output %q missing the record after log debug", out)
	}
	if c.Level() != slog.LevelDebug {
		t.Errorf("level is %v, want debug", c.Level())
	}
	// Attributes and groups added to the handler are kept.
	c.Clear()
	log.With("world", "1-1").WithGroup("net").Info("connected", "peer", "abc")
	if out := strings.Join(c.Lines(), "\n"); !strings.Contains(out, "world=1-1") || !strings.Contains(out, "net.peer=abc") {
		t.Errorf("output %q missing the handler's attributes", out)
	}
}

// TestExec runs a script of commands from a file, skipping comments.
func TestExec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "startup.cfg")
	script := "# a comment\n\nset speed 9\necho done\n"
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{})
	var speed float32
	c.Float("speed", &speed, "")
	c.Run("exec " + path)
	if speed != 9 {
		t.Errorf("speed = %v, want 9", speed)
	}
	if out := strings.Join(c.Lines(), "\n"); !strings.Contains(out, "done") {
		t.Errorf("output %q missing the script's echo", out)
	}
	c.Clear()
	c.Run("exec " + filepath.Join(dir, "missing.cfg"))
	if out := strings.Join(c.Lines(), "\n"); !strings.Contains(out, "exec:") {
		t.Errorf("a missing script printed %q, want an error", out)
	}
	// The Read hook replaces the file system.
	c2 := New(Options{Read: func(name string) ([]byte, error) {
		if name != "boot.cfg" {
			t.Errorf("read %q, want boot.cfg", name)
		}
		return []byte("echo from the pack"), nil
	}})
	c2.Run("exec boot.cfg")
	if out := strings.Join(c2.Lines(), "\n"); !strings.Contains(out, "from the pack") {
		t.Errorf("output %q missing the script read through the hook", out)
	}
}

// TestBindings runs a command when its key is pressed, and stops when it
// is unbound. Bindings fire only while the console is shut.
func TestBindings(t *testing.T) {
	r := newRig(t, Options{})
	c := r.con
	fired := 0
	c.Register("boom", "", func([]string) (string, error) { fired++; return "", nil })
	c.Run("bind F5 boom")
	r.in.press(input.KeyF5)
	r.draw(t)
	if fired != 1 {
		t.Fatalf("binding fired %d times, want 1", fired)
	}
	// While the console is open the keys go to the command line.
	c.SetOpen(true)
	r.in.press(input.KeyF5)
	r.draw(t)
	if fired != 1 {
		t.Errorf("binding fired while the console was open")
	}
	c.SetOpen(false)
	c.Run("unbind F5")
	r.in.press(input.KeyF5)
	r.draw(t)
	if fired != 1 {
		t.Errorf("binding fired %d times after unbind, want 1", fired)
	}
	if c.Run("bind Nonsense boom"); !strings.Contains(strings.Join(c.Lines(), "\n"), "no key") {
		t.Errorf("binding an unknown key was not reported")
	}
}

// TestKeyCapture opens the console with its key, types a command, runs
// it with Enter and closes the console with Escape.
func TestKeyCapture(t *testing.T) {
	r := newRig(t, Options{})
	c := r.con
	if c.Open() {
		t.Fatal("the console starts open")
	}
	// The key that opens it also types its character, which is dropped.
	r.in.press(input.KeyGrave)
	r.in.typed("`")
	r.draw(t)
	if !c.Open() {
		t.Fatal("the console did not open on its key")
	}
	if c.text != "" {
		t.Errorf("the opening keystroke typed %q", c.text)
	}
	r.in.typed("echo hi")
	r.draw(t)
	if c.text != "echo hi" {
		t.Fatalf("the command line holds %q, want %q", c.text, "echo hi")
	}
	r.in.press(input.KeyBackspace)
	r.draw(t)
	if c.text != "echo h" {
		t.Errorf("backspace left %q", c.text)
	}
	r.in.typed("i")
	r.in.press(input.KeyEnter)
	r.draw(t)
	if out := r.output(); !strings.Contains(out, "] echo hi") || !strings.Contains(out, "\nhi\n") {
		t.Errorf("output %q, want the command and its result", out)
	}
	if c.text != "" {
		t.Errorf("the command line kept %q after Enter", c.text)
	}
	// The up arrow brings the command back.
	r.in.press(input.KeyUp)
	r.draw(t)
	if c.text != "echo hi" {
		t.Errorf("history gave %q, want %q", c.text, "echo hi")
	}
	r.in.press(input.KeyDown)
	r.draw(t)
	if c.text != "" {
		t.Errorf("history past the newest gave %q, want an empty line", c.text)
	}
	r.in.press(input.KeyEscape)
	r.draw(t)
	if c.Open() {
		t.Error("Escape did not close the console")
	}
	// Closed again, typing goes nowhere.
	r.in.typed("xyz")
	r.draw(t)
	if c.text != "" {
		t.Errorf("the closed console took %q", c.text)
	}
}

// TestDropDownDraws checks that an open console covers the top of the
// view and leaves the rest of the frame alone, and that a closed one
// draws nothing at all.
func TestDropDownDraws(t *testing.T) {
	r := newRig(t, Options{Height: 0.25})
	r.con.Print("a line of output")
	// Closed: the frame stays as it was cleared.
	if painted := painted(r.capture(t), 0, 480); painted != 0 {
		t.Errorf("a closed console painted %d pixels", painted)
	}
	r.con.SetOpen(true)
	r.con.text = "echo hello"
	r.drawN(t, 2) // a glyph first drawn in a frame reaches the atlas for the next
	img := r.capture(t)
	if got := painted(img, 0, 120); got < 1000 {
		t.Errorf("the drop-down painted %d pixels of the top quarter, want the panel", got)
	}
	if got := painted(img, 200, 480); got != 0 {
		t.Errorf("the console painted %d pixels below its own panel", got)
	}
}

// painted counts the pixels between two rows that are not the colour the
// frame was cleared to.
func painted(img *image.RGBA, top, bottom int) int {
	n := 0
	b := img.Bounds()
	for y := max(top, b.Min.Y); y < min(bottom, b.Max.Y); y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := img.RGBAAt(x, y); c.R != 0 || c.G != 0 || c.B != 0 {
				n++
			}
		}
	}
	return n
}

// TestEngineCommands checks the commands that read the frame the engine
// gives the console.
func TestEngineCommands(t *testing.T) {
	r := newRig(t, Options{})
	c := r.con
	r.draw(t) // the console sees a frame, and with it the engine's hooks
	c.Run("fps")
	if out := r.output(); !strings.Contains(out, "60 fps") {
		t.Errorf("fps printed %q", out)
	}
	c.Clear()
	c.Run("stats")
	if out := r.output(); !strings.Contains(out, "ms/frame") || !strings.Contains(out, "2D") {
		t.Errorf("stats printed %q", out)
	}
	c.Run("screenshot shot.png")
	if r.shot != "shot.png" {
		t.Errorf("screenshot asked for %q", r.shot)
	}
	c.Run("timescale 0.5")
	if r.scale != 0.5 {
		t.Errorf("timescale set %v, want 0.5", r.scale)
	}
	c.Run("quit")
	if !r.quit {
		t.Error("quit did not ask the engine to stop")
	}
}
