package console

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
)

// Handler returns a slog.Handler that passes records on to next and
// copies those at the console's level and above into its output, so the
// game's log shows in the console as well as wherever it was going. The
// engine installs it when Config.Console is set; a game building its own
// console installs it itself:
//
//	con := console.New(console.Options{})
//	cfg.Log = slog.New(con.Handler(slog.Default().Handler()))
//
// Records are captured as they arrive, so raising the level with the log
// command shows more from then on and never rewrites what is already
// there. Handle is safe to call from any goroutine.
func (c *Console) Handler(next slog.Handler) slog.Handler {
	if c == nil {
		return next
	}
	return &handler{c: c, next: next}
}

// Level is the lowest log level the console captures.
func (c *Console) Level() slog.Level {
	if c == nil {
		return slog.LevelInfo
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.level
}

// SetLevel changes the lowest level captured, as the log command does.
func (c *Console) SetLevel(l slog.Level) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.level = l
	c.mu.Unlock()
}

// handler tees records into a console. It keeps the groups and
// attributes given to it so the text it formats matches the record.
type handler struct {
	c      *Console
	next   slog.Handler
	attrs  []slog.Attr
	groups []string
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	if h.next != nil && h.next.Enabled(ctx, l) {
		return true
	}
	return l >= h.c.Level()
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= h.c.Level() {
		h.c.record(r, h.attrs, h.groups)
	}
	if h.next != nil && h.next.Enabled(ctx, r.Level) {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := *h
	out.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	if h.next != nil {
		out.next = h.next.WithAttrs(attrs)
	}
	return &out
}

func (h *handler) WithGroup(name string) slog.Handler {
	out := *h
	out.groups = append(append([]string(nil), h.groups...), name)
	if h.next != nil {
		out.next = h.next.WithGroup(name)
	}
	return &out
}

// record formats one log record into a console line.
func (c *Console) record(r slog.Record, attrs []slog.Attr, groups []string) {
	var b strings.Builder
	b.WriteString(strings.ToUpper(r.Level.String()[:1]))
	b.WriteString(" ")
	b.WriteString(r.Message)
	prefix := ""
	if len(groups) > 0 {
		prefix = strings.Join(groups, ".") + "."
	}
	write := func(a slog.Attr) {
		b.WriteString(" ")
		b.WriteString(prefix)
		b.WriteString(a.Key)
		b.WriteString("=")
		b.WriteString(attrText(a.Value))
	}
	for _, a := range attrs {
		write(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		write(a)
		return true
	})
	c.mu.Lock()
	c.push(line{text: b.String(), level: r.Level, log: true})
	c.mu.Unlock()
}

// attrText renders one attribute value, quoting what holds spaces.
func attrText(v slog.Value) string {
	s := v.Resolve().String()
	if strings.ContainsAny(s, " \t\"") {
		return strconv.Quote(s)
	}
	return s
}
