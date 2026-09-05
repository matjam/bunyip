package ui

import (
	"strings"
	"unicode"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

// Clipboard is what text fields cut, copy and paste through; the
// engine's Context satisfies it, so a game sets ui.Clipboard = ctx.
type Clipboard interface {
	Clipboard() (string, error)
	SetClipboard(string) error
}

// editState is a text field's caret, selection and history, kept per
// widget across frames.
type editState struct {
	caret, anchor int // rune offsets; the selection is between them
	undo, redo    []editSnapshot
	scroll        float32 // horizontal scroll of a single-line field
	blink         int
	lastText      string
	// The last wrap of a text area, kept while the text and width stand.
	wrapText  string
	wrapWidth float32
	wrapLines [][2]int
}

// wrapped returns the line breaks of text at width, reusing the last
// result when nothing changed; a text area asks several times a frame.
func (st *editState) wrapped(f *gfx.Font, text string, width float32) [][2]int {
	if st.wrapLines != nil && text == st.wrapText && width == st.wrapWidth {
		return st.wrapLines
	}
	st.wrapLines = wrapRunes(f, text, width)
	st.wrapText, st.wrapWidth = text, width
	return st.wrapLines
}

type editSnapshot struct {
	text  string
	caret int
}

func (c *Context) editFor(id widgetID) *editState {
	st := c.edits[id]
	if st == nil {
		st = &editState{}
		c.edits[id] = st
	}
	return st
}

func (st *editState) selection() (lo, hi int) {
	if st.anchor < st.caret {
		return st.anchor, st.caret
	}
	return st.caret, st.anchor
}

func (st *editState) clamp(n int) {
	st.caret = max(0, min(st.caret, n))
	st.anchor = max(0, min(st.anchor, n))
}

// edit applies this update's keyboard input to a focused field's text
// and reports whether the text changed. multiline lets Enter insert a
// newline; otherwise Enter and Escape drop focus.
func (c *Context) edit(id widgetID, value *string, multiline bool, lines func(string) [][2]int) bool {
	st := c.editFor(id)
	in := c.in
	runes := []rune(*value)
	st.clamp(len(runes))
	changed := false
	shift := in.Mods()&input.ModShift != 0
	cmd := in.Mods()&(input.ModControl|input.ModSuper) != 0
	snapshot := func() {
		if n := len(st.undo); n > 0 && st.undo[n-1].text == *value {
			return
		}
		st.undo = append(st.undo, editSnapshot{*value, st.caret})
		if len(st.undo) > 100 {
			st.undo = st.undo[1:]
		}
		st.redo = st.redo[:0]
	}
	replace := func(lo, hi int, s string) {
		snapshot()
		ins := []rune(s)
		runes = append(runes[:lo], append(ins, runes[hi:]...)...)
		*value = string(runes)
		st.caret = lo + len(ins)
		st.anchor = st.caret
		changed = true
	}
	deleteSelection := func() bool {
		lo, hi := st.selection()
		if lo == hi {
			return false
		}
		replace(lo, hi, "")
		return true
	}
	move := func(to int) {
		st.caret = max(0, min(to, len(runes)))
		if !shift {
			st.anchor = st.caret
		}
	}
	wordLeft := func(i int) int {
		for i > 0 && !unicode.IsLetter(runes[i-1]) && !unicode.IsDigit(runes[i-1]) {
			i--
		}
		for i > 0 && (unicode.IsLetter(runes[i-1]) || unicode.IsDigit(runes[i-1])) {
			i--
		}
		return i
	}
	wordRight := func(i int) int {
		for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])) {
			i++
		}
		for i < len(runes) && !unicode.IsLetter(runes[i]) && !unicode.IsDigit(runes[i]) {
			i++
		}
		return i
	}
	// Shortcuts first, so their letters are not typed.
	if cmd {
		lo, hi := st.selection()
		switch {
		case in.KeyPressed(input.KeyA):
			st.anchor, st.caret = 0, len(runes)
		case in.KeyPressed(input.KeyC):
			if lo != hi && c.Clipboard != nil {
				_ = c.Clipboard.SetClipboard(string(runes[lo:hi]))
			}
		case in.KeyPressed(input.KeyX):
			if lo != hi {
				if c.Clipboard != nil {
					_ = c.Clipboard.SetClipboard(string(runes[lo:hi]))
				}
				replace(lo, hi, "")
			}
		case in.KeyPressed(input.KeyV):
			if c.Clipboard != nil {
				if s, err := c.Clipboard.Clipboard(); err == nil && s != "" {
					if !multiline {
						s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", "")
					}
					replace(lo, hi, s)
				}
			}
		case in.KeyPressed(input.KeyZ) && shift, in.KeyPressed(input.KeyY):
			if n := len(st.redo); n > 0 {
				st.undo = append(st.undo, editSnapshot{*value, st.caret})
				snap := st.redo[n-1]
				st.redo = st.redo[:n-1]
				*value = snap.text
				runes = []rune(*value)
				st.caret, st.anchor = snap.caret, snap.caret
				changed = true
			}
		case in.KeyPressed(input.KeyZ):
			if n := len(st.undo); n > 0 {
				st.redo = append(st.redo, editSnapshot{*value, st.caret})
				snap := st.undo[n-1]
				st.undo = st.undo[:n-1]
				*value = snap.text
				runes = []rune(*value)
				st.caret, st.anchor = snap.caret, snap.caret
				changed = true
			}
		case in.KeyPressed(input.KeyLeft):
			move(wordLeft(st.caret))
		case in.KeyPressed(input.KeyRight):
			move(wordRight(st.caret))
		}
		st.clamp(len(runes))
		return changed
	}
	for _, ch := range in.Chars() {
		if ch >= ' ' || (multiline && ch == '\t') {
			lo, hi := st.selection()
			replace(lo, hi, string(ch))
		}
	}
	switch {
	case in.KeyPressed(input.KeyBackspace):
		if !deleteSelection() && st.caret > 0 {
			replace(st.caret-1, st.caret, "")
		}
	case in.KeyPressed(input.KeyDelete):
		if !deleteSelection() && st.caret < len(runes) {
			replace(st.caret, st.caret+1, "")
		}
	case in.KeyPressed(input.KeyLeft):
		if lo, hi := st.selection(); lo != hi && !shift {
			st.caret, st.anchor = lo, lo
		} else {
			move(st.caret - 1)
		}
	case in.KeyPressed(input.KeyRight):
		if lo, hi := st.selection(); lo != hi && !shift {
			st.caret, st.anchor = hi, hi
		} else {
			move(st.caret + 1)
		}
	case in.KeyPressed(input.KeyHome):
		move(lineStart(runes, st.caret, multiline, lines, *value))
	case in.KeyPressed(input.KeyEnd):
		move(lineEnd(runes, st.caret, multiline, lines, *value))
	case multiline && in.KeyPressed(input.KeyUp):
		move(lineStep(lines(*value), runes, st.caret, -1))
	case multiline && in.KeyPressed(input.KeyDown):
		move(lineStep(lines(*value), runes, st.caret, 1))
	case in.KeyPressed(input.KeyEnter):
		if multiline {
			lo, hi := st.selection()
			replace(lo, hi, "\n")
		} else {
			c.focus = 0
		}
	case in.KeyPressed(input.KeyEscape):
		c.focus = 0
	}
	st.clamp(len(runes))
	return changed
}

// lineOf finds the wrapped line holding rune offset i.
func lineOf(ls [][2]int, i int) int {
	for n, l := range ls {
		if i >= l[0] && i <= l[1] && (n == len(ls)-1 || i < ls[n+1][0]) {
			return n
		}
	}
	return max(len(ls)-1, 0)
}

func lineStart(runes []rune, i int, multiline bool, lines func(string) [][2]int, text string) int {
	if !multiline {
		return 0
	}
	ls := lines(text)
	if len(ls) == 0 {
		return 0
	}
	return ls[lineOf(ls, i)][0]
}

func lineEnd(runes []rune, i int, multiline bool, lines func(string) [][2]int, text string) int {
	if !multiline {
		return len(runes)
	}
	ls := lines(text)
	if len(ls) == 0 {
		return len(runes)
	}
	return ls[lineOf(ls, i)][1]
}

// lineStep moves a caret up or down one wrapped line, keeping its column.
func lineStep(ls [][2]int, runes []rune, i, dir int) int {
	if len(ls) == 0 {
		return i
	}
	n := lineOf(ls, i)
	col := i - ls[n][0]
	m := n + dir
	if m < 0 {
		return 0
	}
	if m >= len(ls) {
		return len(runes)
	}
	return min(ls[m][0]+col, ls[m][1])
}

// wrapRunes splits text into wrapped lines by greedy word wrap at width,
// as rune offset ranges [start, end); a hard newline ends a line.
func wrapRunes(f *gfx.Font, text string, width float32) [][2]int {
	runes := []rune(text)
	var out [][2]int
	start := 0
	for start <= len(runes) {
		// Find the hard end of this paragraph.
		end := start
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		// Greedy fill within the paragraph: the longest prefix that fits,
		// found by binary search over its length so a line costs a
		// handful of measurements rather than one per word, backed up to
		// the last space when the break would split a word.
		lineStart := start
		for {
			fit := end
			if width > 0 {
				lo, hi := lineStart, end
				for lo < hi {
					mid := (lo + hi + 1) / 2
					if w, _ := f.Measure(string(runes[lineStart:mid]), gfx.TextOptions{}); w <= width {
						lo = mid
					} else {
						hi = mid - 1
					}
				}
				fit = lo
				if fit < end {
					k := fit
					for k > lineStart && runes[k] != ' ' {
						k--
					}
					if k > lineStart {
						fit = k
					}
				}
				if fit == lineStart && fit < end {
					fit = lineStart + 1 // a word wider than the box breaks inside itself
				}
			}
			out = append(out, [2]int{lineStart, fit})
			if fit >= end {
				break
			}
			lineStart = fit
			if runes[lineStart] == ' ' {
				lineStart++
			}
		}
		start = end + 1
		if end >= len(runes) {
			break
		}
	}
	if len(out) == 0 {
		out = [][2]int{{0, 0}}
	}
	return out
}

// caretX is the x offset of a rune offset within a line of text.
func (c *Context) caretX(text string, i int) float32 {
	runes := []rune(text)
	i = max(0, min(i, len(runes)))
	w, _ := c.Theme.Font.Measure(string(runes[:i]), gfx.TextOptions{})
	return w
}

// indexAt finds the rune offset nearest an x offset within a line.
func (c *Context) indexAt(text string, x float32) int {
	runes := []rune(text)
	best, bestD := 0, float32(1e9)
	for i := 0; i <= len(runes); i++ {
		if d := abs32(c.caretX(text, i) - x); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// textFocused applies modal ownership to retained text focus. A blocked
// editor does not mark focusSeen, so end releases its focus unless a
// modal editor takes it during the frame.
func (c *Context) textFocused(id widgetID) bool {
	return c.focus == id && (c.modal == 0 || c.inModal)
}

// focusNavigatedText claims this frame's navigation destination without
// moving its caret to the pointer or restoring a previously released focus.
func (c *Context) focusNavigatedText(id widgetID) {
	if c.navTextFocus == id && (c.modal == 0 || c.inModal) {
		c.focus = id
		c.navTextFocus = 0
		c.editFor(id).blink = 0
	}
}

// TextField edits *value on one line while focused and reports a change.
// It has a caret, a selection (Shift with the arrows, or a drag), Home
// and End, word jumps with Ctrl or Cmd and the arrows, select all, cut,
// copy and paste through the Clipboard, and undo and redo (Ctrl or Cmd
// with Z, Shift+Z or Y). Enter and Escape drop focus. An open modal
// releases focus from fields outside it. Tab and gamepad navigation
// transfer text focus while preserving each field's caret and selection.
func (c *Context) TextField(label string, value *string) bool {
	id := c.id(label)
	r := c.next(c.Theme.RowHeight)
	_, held, clicked := c.interact(id, r)
	st := c.editFor(id)
	c.focusNavigatedText(id)
	inner := Rect{X: r.X + c.Theme.Padding, Y: r.Y, W: r.W - 2*c.Theme.Padding, H: r.H}
	if clicked || (held && c.focus == id) {
		i := c.indexAt(*value, c.mouseX-inner.X+st.scroll)
		if clicked && c.focus != id {
			c.focus = id
			st.caret, st.anchor = i, i
		} else if c.pressed {
			st.caret, st.anchor = i, i
		} else {
			st.caret = i // dragging extends the selection
		}
	}
	if c.textFocused(id) {
		c.focusSeen, c.focusRect = true, r
	}
	changed := false
	if c.textFocused(id) {
		changed = c.edit(id, value, false, nil)
	}
	c.note("textfield", label, *value, c.textFocused(id))
	sk := c.skin()
	border, slice := c.Theme.FieldBorder, sk.Field
	if c.textFocused(id) {
		border, slice = c.Theme.Accent, or(sk.FieldFocus, sk.Field)
	}
	c.box(slice, r, c.Theme.Field, border)
	_, h := c.Theme.Font.Measure("Ag", gfx.TextOptions{})
	ty := r.Y + (r.H-h)/2
	if *value == "" && !c.textFocused(id) {
		c.text(label, inner.X, ty, c.Theme.TextDim)
		return false
	}
	// Keep the caret in view by scrolling the text horizontally.
	if c.textFocused(id) {
		cx := c.caretX(*value, st.caret)
		if cx-st.scroll > inner.W {
			st.scroll = cx - inner.W
		}
		if cx-st.scroll < 0 {
			st.scroll = cx
		}
		st.scroll = max(0, st.scroll)
	} else {
		st.scroll = 0
	}
	c.g.Clip(inner, func() {
		x := inner.X - st.scroll
		if c.textFocused(id) {
			if lo, hi := st.selection(); lo != hi {
				x0, x1 := c.caretX(*value, lo), c.caretX(*value, hi)
				c.fill(Rect{X: x + x0, Y: ty, W: x1 - x0, H: h}, c.Theme.Accent.WithAlpha(0.35))
			}
		}
		c.text(*value, x, ty, c.Theme.Text)
		if c.textFocused(id) {
			composing := c.in.Composition()
			cx := x + c.caretX(*value, st.caret)
			if composing != "" {
				cw, ch := c.Theme.Font.Measure(composing, gfx.TextOptions{})
				c.text(composing, cx, ty, c.Theme.Accent)
				c.fill(Rect{X: cx, Y: ty + ch, W: cw, H: 1}, c.Theme.Accent)
				cx += cw
			}
			st.blink++
			if st.blink%60 < 40 {
				c.fill(Rect{X: cx, Y: ty, W: 1, H: h}, c.Theme.Text)
			}
			if c.OnTextInputRect != nil {
				c.OnTextInputRect(r.X, r.Y, r.W, r.H)
			}
		}
	})
	return changed
}

// TextArea edits *value over several wrapped lines in a box of the given
// height, with the same keys as TextField plus Up, Down and Enter for a
// new line; it scrolls to keep the caret in view. An open modal releases
// focus from areas outside it. Tab and gamepad navigation transfer text
// focus while preserving each area's caret and selection.
func (c *Context) TextArea(label string, value *string, height float32) bool {
	id := c.id("textarea:" + label)
	r := c.next(height)
	_, held, clicked := c.interact(id, r)
	st := c.editFor(id)
	c.focusNavigatedText(id)
	inner := Rect{X: r.X + c.Theme.Padding, Y: r.Y + c.Theme.Padding, W: r.W - 2*c.Theme.Padding, H: r.H - 2*c.Theme.Padding}
	lineH := c.Theme.Font.LineHeight
	lines := func(s string) [][2]int { return st.wrapped(c.Theme.Font, s, inner.W) }
	runes := []rune(*value)
	ls := lines(*value)
	// Which line and x the pointer is over.
	if clicked || (held && c.focus == id) {
		row := max(0, min(int((c.mouseY-inner.Y+st.scroll)/lineH), len(ls)-1))
		l := ls[row]
		i := l[0] + c.indexAt(string(runes[l[0]:l[1]]), c.mouseX-inner.X)
		if clicked && c.focus != id {
			c.focus = id
			st.caret, st.anchor = i, i
		} else if c.pressed {
			st.caret, st.anchor = i, i
		} else {
			st.caret = i
		}
	}
	if c.textFocused(id) {
		c.focusSeen, c.focusRect = true, r
	}
	changed := false
	if c.textFocused(id) {
		changed = c.edit(id, value, true, lines)
		runes = []rune(*value)
		ls = lines(*value)
	}
	c.note("textarea", label, *value, c.textFocused(id))
	sk := c.skin()
	border, slice := c.Theme.FieldBorder, sk.Field
	if c.textFocused(id) {
		border, slice = c.Theme.Accent, or(sk.FieldFocus, sk.Field)
	}
	c.box(slice, r, c.Theme.Field, border)
	if *value == "" && !c.textFocused(id) {
		c.text(label, inner.X, inner.Y, c.Theme.TextDim)
		return false
	}
	// Scroll to the caret's line.
	if c.textFocused(id) {
		row := lineOf(ls, st.caret)
		top := float32(row) * lineH
		if top-st.scroll < 0 {
			st.scroll = top
		}
		if top+lineH-st.scroll > inner.H {
			st.scroll = top + lineH - inner.H
		}
	}
	total := float32(len(ls)) * lineH
	st.scroll = max(0, min(st.scroll, max(total-inner.H, 0)))
	lo, hi := st.selection()
	c.g.Clip(inner, func() {
		for n, l := range ls {
			y := inner.Y + float32(n)*lineH - st.scroll
			if y+lineH < inner.Y || y > inner.Y+inner.H {
				continue
			}
			line := string(runes[l[0]:l[1]])
			if c.textFocused(id) && lo != hi && hi > l[0] && lo <= l[1] {
				a, b := max(lo, l[0]), min(hi, l[1])
				x0 := c.caretX(line, a-l[0])
				x1 := c.caretX(line, b-l[0])
				if b == l[1] && hi > l[1] {
					x1 += 4 // the selected newline
				}
				c.fill(Rect{X: inner.X + x0, Y: y, W: x1 - x0, H: lineH}, c.Theme.Accent.WithAlpha(0.35))
			}
			c.text(line, inner.X, y, c.Theme.Text)
			if c.textFocused(id) && lineOf(ls, st.caret) == n {
				st.blink++
				if st.blink%60 < 40 {
					cx := inner.X + c.caretX(line, st.caret-l[0])
					c.fill(Rect{X: cx, Y: y, W: 1, H: lineH}, c.Theme.Text)
				}
			}
		}
	})
	if c.textFocused(id) && c.OnTextInputRect != nil {
		c.OnTextInputRect(r.X, r.Y, r.W, r.H)
	}
	_ = lin.Vec2{}
	return changed
}
