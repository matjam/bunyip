package console

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/matjam/bunyip/input"
)

// Command is one console command. Fn reads the arguments after the
// command's name and returns the text to print, which may be empty, or
// an error, which prints in red.
type Command struct {
	Name string
	Help string
	Fn   func(args []string) (string, error)
}

// Register adds a command, replacing any command of the same name. Help
// is the one-line description the help command prints:
//
//	con.Register("give", "give <item> [count]: add an item", func(args []string) (string, error) {
//		...
//	})
func (c *Console) Register(name, help string, fn func(args []string) (string, error)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.commands[name]; !ok {
		c.order = append(c.order, name)
	}
	c.commands[name] = &Command{Name: name, Help: help, Fn: fn}
}

// Commands returns every registered command, sorted by name.
func (c *Console) Commands() []Command {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Command, 0, len(c.commands))
	for _, name := range c.order {
		out = append(out, *c.commands[name])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Run parses and runs one command line, printing what it returns. An
// unknown command is an error. Call it from the goroutine that draws.
func (c *Console) Run(line string) {
	if c == nil {
		return
	}
	args := splitArgs(line)
	if len(args) == 0 {
		return
	}
	c.mu.Lock()
	cmd := c.commands[args[0]]
	c.mu.Unlock()
	if cmd == nil {
		c.error(fmt.Errorf("unknown command %q; try help", args[0]))
		return
	}
	out, err := cmd.Fn(args[1:])
	if err != nil {
		c.error(err)
		return
	}
	if out != "" {
		c.Print(out)
	}
}

// error prints an error at the error level, so it is coloured as one.
func (c *Console) error(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.push(line{text: err.Error(), level: slog.LevelError, log: true})
}

// splitArgs cuts a command line into words, keeping what is inside
// double quotes together so an argument may hold spaces.
func splitArgs(line string) []string {
	var args []string
	var cur strings.Builder
	quoted, any := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted, any = !quoted, true
		case !quoted && (r == ' ' || r == '\t'):
			if cur.Len() > 0 || any {
				args = append(args, cur.String())
				cur.Reset()
				any = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 || any {
		args = append(args, cur.String())
	}
	return args
}

// complete finishes the word the caret is in from the command names, or
// from the variable names after set and get. One candidate is filled in;
// several print as a list with their common prefix filled in.
func (c *Console) complete() {
	args := splitArgs(c.text)
	trailing := strings.HasSuffix(c.text, " ")
	word := ""
	if !trailing && len(args) > 0 {
		word = args[len(args)-1]
		args = args[:len(args)-1]
	}
	var candidates []string
	switch {
	case len(args) == 0:
		for _, cmd := range c.Commands() {
			candidates = append(candidates, cmd.Name)
		}
	case len(args) == 1 && (args[0] == "set" || args[0] == "get" || args[0] == "vars"):
		candidates = c.VarNames()
	case len(args) == 1 && args[0] == "help":
		for _, cmd := range c.Commands() {
			candidates = append(candidates, cmd.Name)
		}
	default:
		return
	}
	var matches []string
	for _, cand := range candidates {
		if strings.HasPrefix(cand, word) {
			matches = append(matches, cand)
		}
	}
	if len(matches) == 0 {
		return
	}
	prefix := matches[0]
	for _, m := range matches[1:] {
		for !strings.HasPrefix(m, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	head := strings.Join(args, " ")
	if head != "" {
		head += " "
	}
	c.text = head + prefix
	if len(matches) == 1 {
		c.text += " "
	} else {
		c.Print(strings.Join(matches, "  "))
	}
	c.caret = len([]rune(c.text))
}

// registerBuiltins adds the commands every console has.
func (c *Console) registerBuiltins() {
	c.Register("help", "help [command]: list the commands, or describe one", c.cmdHelp)
	c.Register("echo", "echo <text...>: print the arguments", func(args []string) (string, error) {
		return strings.Join(args, " "), nil
	})
	c.Register("clear", "clear: empty the output", func([]string) (string, error) {
		c.Clear()
		return "", nil
	})
	c.Register("quit", "quit: end the game", func([]string) (string, error) {
		if c.frame.Quit == nil {
			return "", fmt.Errorf("quit: the engine did not give the console a way to quit")
		}
		c.frame.Quit()
		return "", nil
	})
	c.Register("screenshot", "screenshot [file]: write the next frame to a PNG", func(args []string) (string, error) {
		if c.frame.Screenshot == nil {
			return "", fmt.Errorf("screenshot: the engine did not give the console a way to take one")
		}
		path := "screenshot.png"
		if len(args) > 0 {
			path = args[0]
		}
		c.frame.Screenshot(path)
		return "writing " + path, nil
	})
	c.Register("exec", "exec <file>: run a file of commands, one per line", c.cmdExec)
	c.Register("bind", "bind <key> <command...>: run a command when a key is pressed", c.cmdBind)
	c.Register("unbind", "unbind <key>: drop a key's binding", c.cmdUnbind)
	c.Register("binds", "binds: list the key bindings", c.cmdBinds)
	c.Register("fps", "fps: the frame rate and frame time", func([]string) (string, error) {
		s := c.frame.Stats
		return fmt.Sprintf("%.0f fps, %.2f ms/frame (update %.2f, draw %.2f, present %.2f)",
			s.FPS, s.FrameMS, s.UpdateMS, s.DrawMS, s.PresentMS), nil
	})
	c.Register("stats", "stats: the frame timings, the GPU pass times, profile scopes and draw counts", c.cmdStats)
	c.Register("log", "log <level>: show log records at this level and above (debug, info, warn, error)", c.cmdLog)
	c.Register("timescale", "timescale [x]: read or set how fast game time runs", c.cmdTimescale)
	c.Register("panels", "panels [tab]: open or close the debug panels, on a named tab", c.cmdPanels)
	c.Register("set", "set <name> <value>: change a registered variable", c.cmdSet)
	c.Register("get", "get <name>: print a registered variable", c.cmdGet)
	c.Register("vars", "vars [prefix]: list the registered variables", c.cmdVars)
}

func (c *Console) cmdHelp(args []string) (string, error) {
	cmds := c.Commands()
	if len(args) > 0 {
		for _, cmd := range cmds {
			if cmd.Name == args[0] {
				if cmd.Help == "" {
					return cmd.Name + ": no help", nil
				}
				return cmd.Help, nil
			}
		}
		return "", fmt.Errorf("help: no command %q", args[0])
	}
	var b strings.Builder
	for i, cmd := range cmds {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("  ")
		if cmd.Help != "" {
			b.WriteString(cmd.Help)
		} else {
			b.WriteString(cmd.Name)
		}
	}
	return b.String(), nil
}

func (c *Console) cmdExec(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("exec: needs a file")
	}
	read := c.opts.Read
	if read == nil {
		read = os.ReadFile
	}
	data, err := read(args[0])
	if err != nil {
		return "", fmt.Errorf("exec: %w", err)
	}
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
			continue
		}
		c.Run(l)
	}
	return "", nil
}

// parseKey reads a key by the name input.Key.String gives it.
func parseKey(name string) (input.Key, error) {
	src, err := input.ParseSource("key:" + name)
	if err != nil {
		return input.KeyUnknown, fmt.Errorf("no key called %q", name)
	}
	return input.Key(src.Code), nil
}

func (c *Console) cmdBind(args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("bind: needs a key and a command")
	}
	key, err := parseKey(args[0])
	if err != nil {
		return "", fmt.Errorf("bind: %w", err)
	}
	c.bindings[key] = strings.Join(args[1:], " ")
	return "bound " + key.String() + " to " + c.bindings[key], nil
}

func (c *Console) cmdUnbind(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("unbind: needs a key")
	}
	key, err := parseKey(args[0])
	if err != nil {
		return "", fmt.Errorf("unbind: %w", err)
	}
	delete(c.bindings, key)
	return "", nil
}

func (c *Console) cmdBinds([]string) (string, error) {
	if len(c.bindings) == 0 {
		return "no key bindings", nil
	}
	lines := make([]string, 0, len(c.bindings))
	for key, cmd := range c.bindings {
		lines = append(lines, "  "+key.String()+"  "+cmd)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

func (c *Console) cmdStats([]string) (string, error) {
	s := c.frame.Stats
	var b strings.Builder
	fmt.Fprintf(&b, "%.0f fps  %.2f ms/frame\n", s.FPS, s.FrameMS)
	fmt.Fprintf(&b, "update %.2f ms x%d  draw %.2f ms  present %.2f ms\n", s.UpdateMS, s.Updates, s.DrawMS, s.PresentMS)
	if g := c.frame.Gfx; g != nil {
		gs := g.Stats()
		fmt.Fprintf(&b, "2D %d draws %d verts (%d culled)\n", gs.Draws2D, gs.Vertices2D, gs.Culled2D)
		fmt.Fprintf(&b, "3D %d draws %d instances (%d culled, %d lights dropped)\n", gs.Draws3D, gs.Instances, gs.Culled, gs.LightsDropped)
	}
	if len(s.GPU) > 0 {
		fmt.Fprintf(&b, "gpu %.2f ms a frame\n", s.GPUFrameMS)
		for _, sp := range s.GPU {
			fmt.Fprintf(&b, "  %s %.2f ms\n", sp.Name, sp.MS)
		}
	}
	for _, sc := range s.Scopes {
		fmt.Fprintf(&b, "  %s %.2f ms\n", sc.Name, sc.MS)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (c *Console) cmdLog(args []string) (string, error) {
	if len(args) == 0 {
		return "log level " + c.Level().String(), nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(args[0])); err != nil {
		return "", fmt.Errorf("log: no level %q; use debug, info, warn or error", args[0])
	}
	c.SetLevel(level)
	return "log level " + level.String(), nil
}

func (c *Console) cmdTimescale(args []string) (string, error) {
	if c.frame.TimeScale == nil || c.frame.SetTimeScale == nil {
		return "", fmt.Errorf("timescale: the engine did not give the console one")
	}
	if len(args) == 0 {
		return fmt.Sprintf("timescale %g", c.frame.TimeScale()), nil
	}
	v, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		return "", fmt.Errorf("timescale: %q is not a number", args[0])
	}
	c.frame.SetTimeScale(v)
	return fmt.Sprintf("timescale %g", c.frame.TimeScale()), nil
}

func (c *Console) cmdPanels(args []string) (string, error) {
	if len(args) == 0 {
		c.panels.open = !c.panels.open
		return "", nil
	}
	for i, name := range tabNames {
		if strings.EqualFold(name, args[0]) {
			c.panels.open, c.panels.tab = true, i
			return "", nil
		}
	}
	return "", fmt.Errorf("panels: no tab %q; try %s", args[0], strings.Join(tabNames, ", "))
}
