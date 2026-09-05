package console

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Var is a console variable of a type the console has no helper for: a
// colour, an enumeration, a value behind a lock. String is what get
// prints and Set parses what set is given.
type Var interface {
	String() string
	Set(text string) error
}

// varEntry is a registered variable and its description.
type varEntry struct {
	v    Var
	help string
}

// Var registers a variable of any type, replacing one of the same name.
// The console never copies the value: it reads and writes through v
// whenever the game asks, so a variable stays live.
func (c *Console) Var(name string, v Var, help string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.vars[name]; !ok {
		c.varOrder = append(c.varOrder, name)
	}
	c.vars[name] = varEntry{v: v, help: help}
}

// Float registers a float32 the set command edits, for a tunable a game
// wants to try values for while it runs:
//
//	con.Float("player.speed", &g.speed, "how fast the player runs")
func (c *Console) Float(name string, p *float32, help string) { c.Var(name, (*floatVar)(p), help) }

// Int registers an int the set command edits.
func (c *Console) Int(name string, p *int, help string) { c.Var(name, (*intVar)(p), help) }

// Bool registers a bool the set command edits; set takes true, false,
// 1, 0, on or off.
func (c *Console) Bool(name string, p *bool, help string) { c.Var(name, (*boolVar)(p), help) }

// String registers a string the set command edits.
func (c *Console) String(name string, p *string, help string) { c.Var(name, (*stringVar)(p), help) }

type floatVar float32

func (v *floatVar) String() string { return strconv.FormatFloat(float64(*v), 'g', -1, 32) }
func (v *floatVar) Set(text string) error {
	f, err := strconv.ParseFloat(text, 32)
	if err != nil {
		return fmt.Errorf("%q is not a number", text)
	}
	*v = floatVar(f)
	return nil
}

type intVar int

func (v *intVar) String() string { return strconv.Itoa(int(*v)) }
func (v *intVar) Set(text string) error {
	n, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("%q is not a whole number", text)
	}
	*v = intVar(n)
	return nil
}

type boolVar bool

func (v *boolVar) String() string { return strconv.FormatBool(bool(*v)) }
func (v *boolVar) Set(text string) error {
	switch strings.ToLower(text) {
	case "1", "true", "on", "yes":
		*v = true
	case "0", "false", "off", "no":
		*v = false
	default:
		return fmt.Errorf("%q is not true or false", text)
	}
	return nil
}

type stringVar string

func (v *stringVar) String() string        { return string(*v) }
func (v *stringVar) Set(text string) error { *v = stringVar(text); return nil }

// VarNames returns every registered variable's name, sorted.
func (c *Console) VarNames() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]string(nil), c.varOrder...)
	sort.Strings(out)
	return out
}

// GetVar returns a variable's value as text, and false when no variable
// has that name.
func (c *Console) GetVar(name string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	e, ok := c.vars[name]
	c.mu.Unlock()
	if !ok {
		return "", false
	}
	return e.v.String(), true
}

// SetVar parses text into a registered variable.
func (c *Console) SetVar(name, text string) error {
	if c == nil {
		return fmt.Errorf("no console")
	}
	c.mu.Lock()
	e, ok := c.vars[name]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("no variable %q; try vars", name)
	}
	if err := e.v.Set(text); err != nil {
		return fmt.Errorf("set %s: %w", name, err)
	}
	return nil
}

func (c *Console) cmdSet(args []string) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("set: needs a name and a value")
	}
	if err := c.SetVar(args[0], strings.Join(args[1:], " ")); err != nil {
		return "", err
	}
	v, _ := c.GetVar(args[0])
	return args[0] + " = " + v, nil
}

func (c *Console) cmdGet(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("get: needs a name")
	}
	v, ok := c.GetVar(args[0])
	if !ok {
		return "", fmt.Errorf("get: no variable %q; try vars", args[0])
	}
	return args[0] + " = " + v, nil
}

func (c *Console) cmdVars(args []string) (string, error) {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}
	var lines []string
	for _, name := range c.VarNames() {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		c.mu.Lock()
		e := c.vars[name]
		c.mu.Unlock()
		l := "  " + name + " = " + e.v.String()
		if e.help != "" {
			l += "   " + e.help
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		return "no variables", nil
	}
	return strings.Join(lines, "\n"), nil
}
