---
title: Network
example: network
summary: a chat room over TCP with pointer positions over UDP, turn-based and asleep until a message arrives, with reliable UDP and its round trip and loss
---

This is a small multiplayer program: one copy hosts and the others join.
Typed lines travel over TCP and every peer's pointer position travels
over UDP, so both halves of [network](../pkg/network.html) are in one
place. With `-reliable` the chat goes over UDP too, through
`SendReliable`, and a panel counts how many lines arrived and whether
they arrived in order, with the link's round trip and loss.

Like [roguelike](roguelike.html), it runs turn-based.
`bunyip.Config{TurnBased: true}` lets the engine loop block in the operating
system until events or a wake request arrive. Network goroutines still run,
including the UDP peer's retries and keepalives.
Network traffic has to wake it, which is what `SetOnActivity(ctx.Wake)`
arranges: the network calls `ctx.Wake` from its own goroutine and the
loop runs an update. Read [the window guide](../guides/window.html) for
the two loop modes and
[the game services guide](../guides/services.html) for the rest.

The example needs a peer, so the examples test skips it and this page has
no screenshot. Run two copies:

```bash
go run ./examples/network -listen :7777
go run ./examples/network -join localhost:7777 -name second
```

The flags are `-seconds N` and `-shot file.png`, `-listen addr` to host,
`-join host:port` to connect, `-name` for the name shown to others,
`-bot` to send a greeting once connected, and `-reliable` to send chat
over reliable UDP.

## The messages

A message is a Go type registered with a `network.Registry`, which both
ends build the same way so a type id means the same thing on each. Four
types are enough here: a `join` announcing a name, a `chat` line, a
`welcome` telling a new client which UDP port the host listens on, and a
`pointer` position.

`pointer` implements `MarshalBinary` and `UnmarshalBinary`, so it is sent
as eight bytes and a name rather than through the default encoding. That
is worth doing for a message sent on every mouse move and not worth doing
for anything sent once. The coordinates are rounded to integers on the
way out, which is all a pointer needs.

```go
// Command network is a chat room over the network package. Run one copy
// with -listen and others with -join; typed lines travel over TCP and
// every peer's pointer position is shared over UDP. With -reliable the
// chat lines go over UDP too, with SendReliable, and the panel counts
// how many arrived in order. Both sides run in turn-based mode and
// sleep until input or a message arrives, which the network's activity
// hook signals through Context.Wake.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"time"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/network"
	"github.com/matjam/bunyip/ui"
)

// Messages shared by both ends.
type join struct{ Name string }
type chat struct {
	From, Text string
	Seq        int // the sender's count of reliable lines, to check the order
}
type welcome struct{ UDPPort int }

// pointer is a UDP message with a compact binary encoding.
type pointer struct {
	Name string
	X, Y float32
}

func (p pointer) MarshalBinary() ([]byte, error) {
	b := make([]byte, 8+len(p.Name))
	binary.LittleEndian.PutUint32(b, uint32(int32(p.X)))
	binary.LittleEndian.PutUint32(b[4:], uint32(int32(p.Y)))
	copy(b[8:], p.Name)
	return b, nil
}

func (p *pointer) UnmarshalBinary(b []byte) error {
	if len(b) < 8 {
		return fmt.Errorf("short pointer")
	}
	p.X = float32(int32(binary.LittleEndian.Uint32(b)))
	p.Y = float32(int32(binary.LittleEndian.Uint32(b[4:])))
	p.Name = string(b[8:])
	return nil
}
```

## The game state

The game holds both ends of both protocols, because one process can be
either. `server` and `client` are the TCP halves and only one is set;
`udp` is a peer that exists either way. `peers` is the set of UDP
addresses the host fans messages out to, and `target` is the one address
a client sends to.

```go
type game struct {
	seconds  float64
	shot     string
	listen   string
	joinTo   string
	name     string
	bot      bool
	reliable bool

	font     *gfx.Font
	ui       *ui.Context
	reg      *network.Registry
	server   *network.Server
	client   *network.Client
	udp      *network.Peer
	peers    map[string]*network.Addr // connected UDP addresses to fan out to (server)
	target   *network.Addr            // where a client sends over UDP
	lines    []string
	draft    string
	points   map[string]pointer
	mouseX   float32
	mouseY   float32
	shotDone bool

	sent     int            // reliable lines sent
	received int            // reliable lines received
	lastSeq  map[string]int // last reliable Seq seen per sender
	inOrder  bool
}
```

## Init: registry, sockets and wake-ups

The registry registers the four message types. `network.ListenUDP(":0",
g.reg)` opens a UDP socket on a port the operating system chooses, which
both the host and a client do.

`SetOnActivity(ctx.Wake)` is what makes turn-based mode work with a
network: the socket calls it when traffic arrives, and `ctx.Wake` is safe
to call from another goroutine, unlike almost everything else on the
context.

`network.Listen` starts a TCP server and `network.Dial` connects to one
with a timeout. The client sends its `join` immediately.
`g.ui.OnTextInputRect = ctx.SetTextInputRect` tells the platform where
the text field is, so an input method's candidate window appears in the
right place.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	g.ui.OnTextInputRect = ctx.SetTextInputRect
	g.reg = network.NewRegistry().Register(join{}, chat{}, welcome{}, pointer{})
	g.peers = map[string]*network.Addr{}
	g.points = map[string]pointer{}
	g.lastSeq = map[string]int{}
	g.inOrder = true
	if g.udp, err = network.ListenUDP(":0", g.reg); err != nil {
		return err
	}
	g.udp.SetOnActivity(ctx.Wake)
	switch {
	case g.listen != "":
		if g.server, err = network.Listen(g.listen, g.reg); err != nil {
			return err
		}
		g.server.SetOnActivity(ctx.Wake)
		g.say(fmt.Sprintf("Listening on %s (UDP %d). Others join with -join %s", g.server.Addr(), g.udp.Addr().Port, g.server.Addr()))
	case g.joinTo != "":
		if g.client, err = network.Dial(g.joinTo, g.reg, 5*time.Second); err != nil {
			return err
		}
		g.client.SetOnActivity(ctx.Wake)
		g.client.Send(join{Name: g.name})
		g.say("Connected to " + g.joinTo)
	default:
		g.say("Run with -listen :7777 to host, or -join host:7777 to connect.")
	}
	return nil
}
```

```go
func (g *game) Shutdown(ctx *bunyip.Context) {
	if g.server != nil {
		g.server.Close()
	}
	if g.client != nil {
		g.client.Close()
	}
	g.udp.Close()
	g.font.Destroy()
}
```

`say` appends a line to the log and keeps the last fourteen.

```go
func (g *game) say(line string) {
	g.lines = append(g.lines, line)
	if len(g.lines) > 14 {
		g.lines = g.lines[1:]
	}
}
```

`sendChat` is where the two transports diverge. Over TCP a client sends
to the server and a server broadcasts to everyone. Over reliable UDP each
line is numbered with the sender's own counter and sent to every known
address with `SendReliable`, which retransmits until it is acknowledged
and delivers in order.

```go
// sendChat delivers a line to everyone else: over TCP, or with
// -reliable over UDP with SendReliable.
func (g *game) sendChat(msg chat) {
	if !g.reliable {
		if g.client != nil {
			g.client.Send(msg)
		}
		if g.server != nil {
			g.server.Broadcast(msg)
		}
		return
	}
	msg.Seq = g.sent
	g.sent++
	if g.target != nil {
		g.udp.SendReliable(g.target, msg)
	}
	for _, addr := range g.peers {
		g.udp.SendReliable(addr, msg)
	}
}
```

`receiveChat` checks the numbering: a line whose sequence is not one more
than the last from that sender means the ordering guarantee failed, which
the panel reports.

```go
// receiveChat counts a reliable line and checks it follows the
// sender's previous one.
func (g *game) receiveChat(m *chat) {
	g.received++
	if last, seen := g.lastSeq[m.From]; seen && m.Seq != last+1 {
		g.inOrder = false
	}
	g.lastSeq[m.From] = m.Seq
	g.say(m.From + ": " + m.Text)
}
```

## Update: polling the three sockets

Nothing arrives asynchronously into the game. Each socket is polled once
per update and returns the events that have queued up, so all the game
state changes on one goroutine, in the update, and no locking is needed.

`ctx.RequestRedraw` in a timed run keeps the turn-based loop ticking even
with no input, so the timeout and the screenshot still happen.

The server's events are `Connected`, `Disconnected` and `Message`.
A connection carries a `Data` field the game can use for its own
per-connection state, which is where the peer's name is kept.
`Broadcast(*m, ev.Conn)` sends to everyone except the connections listed,
so the sender does not receive its own line back.

The client's loop handles `welcome` by resolving the host's UDP address
and calling `Connect`, which makes the host aware of this peer before the
first pointer arrives.

The UDP loop fans pointers and reliable chat out to every other peer,
which is what makes the host the relay: clients talk to the host, and the
host repeats to the others.

The last block sends this peer's pointer whenever the mouse moves, which
is the only unreliable traffic here. A dropped pointer does not matter,
because the next one replaces it.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) && !g.ui.WantsKeyboard() || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.seconds > 0 {
		ctx.RequestRedraw() // keep ticking so the timeout and screenshot fire
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	if g.server != nil {
		for _, ev := range g.server.Poll() {
			switch ev.Kind {
			case network.Connected:
				ev.Conn.Send(welcome{UDPPort: g.udp.Addr().Port})
				g.say(fmt.Sprintf("Peer %d connected from %s", ev.Conn.ID, ev.Conn.Addr()))
			case network.Disconnected:
				if ev.Conn != nil {
					g.say(fmt.Sprintf("Peer %d left (%v)", ev.Conn.ID, ev.Err))
				}
			case network.Message:
				switch m := ev.Msg.(type) {
				case *join:
					ev.Conn.Data = m.Name
					g.say(m.Name + " joined")
					g.server.Broadcast(chat{From: "server", Text: m.Name + " joined"})
				case *chat:
					g.say(m.From + ": " + m.Text)
					g.server.Broadcast(*m, ev.Conn) // everyone but the sender, who already has it
				}
			}
		}
	}
	if g.client != nil {
		for _, ev := range g.client.Poll() {
			switch ev.Kind {
			case network.Disconnected:
				g.say(fmt.Sprintf("Disconnected (%v)", ev.Err))
				g.client = nil
			case network.Message:
				switch m := ev.Msg.(type) {
				case *welcome:
					host, _, _ := splitHostPort(g.joinTo)
					g.target, _ = network.Resolve(fmt.Sprintf("%s:%d", host, m.UDPPort))
					g.udp.Connect(g.target) // so the host sees us before the first pointer
					if g.bot && !g.reliable {
						g.client.Send(chat{From: g.name, Text: "hello from a bot"})
					}
					if g.bot && g.reliable {
						for i := range 20 {
							g.sendChat(chat{From: g.name, Text: fmt.Sprintf("reliable line %d", i+1)})
						}
					}
				case *chat:
					g.say(m.From + ": " + m.Text)
				}
			}
			if g.client == nil {
				break
			}
		}
	}
	for _, ev := range g.udp.Poll() {
		switch ev.Kind {
		case network.Connected:
			if g.server != nil {
				g.peers[ev.From.String()] = ev.From
			}
		case network.Disconnected:
			delete(g.peers, ev.From.String())
		case network.Message:
			switch m := ev.Msg.(type) {
			case *pointer:
				g.points[m.Name] = *m
				if g.server != nil { // fan out to every other peer
					for key, addr := range g.peers {
						if key != ev.From.String() {
							g.udp.Send(addr, *m)
						}
					}
				}
			case *chat:
				g.receiveChat(m)
				if g.server != nil { // relay in order to every other peer
					for key, addr := range g.peers {
						if key != ev.From.String() {
							g.udp.SendReliable(addr, *m)
						}
					}
				}
			}
		}
	}
	x, y := in.Mouse()
	if float32(x) != g.mouseX || float32(y) != g.mouseY {
		g.mouseX, g.mouseY = float32(x), float32(y)
		me := pointer{Name: g.name, X: g.mouseX, Y: g.mouseY}
		g.points[g.name] = me
		if g.target != nil {
			g.udp.Send(g.target, me)
		}
		for _, addr := range g.peers {
			g.udp.Send(addr, me)
		}
	}
	return nil
}
```

`splitHostPort` splits an address at the last colon, which keeps IPv6
addresses in brackets intact.

```go
func splitHostPort(s string) (host, port string, err error) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return s[:i], s[i+1:], nil
		}
	}
	return s, "", fmt.Errorf("no port")
}
```

## Draw: pointers, chat and the link

Every known pointer is drawn as a square with a name. The chat panel is a
label per line, the draft panel a text field bound to a string by
pointer, and the reliable panel reports the counters and
`udp.Stats(addr)`, the measured round trip and loss for one link.

Enter is read after the interface is built, so `u.TextField` has had this
frame's input first. Input edges are latched for the whole frame, so a
press is visible here whether or not an update ran.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	for name, p := range g.points {
		gr.FillRect(p.X-5, p.Y-5, 10, 10, gfx.RGB(255, 200, 80))
		gr.DrawText(g.font, name, p.X+8, p.Y-8, gfx.RGB(255, 220, 140))
	}
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Chat", ui.Rect{X: 16, Y: 16, W: 480, H: 380}, func() {
			for _, l := range g.lines {
				u.Label(l)
			}
		})
		u.Panel("", ui.Rect{X: 16, Y: 404, W: 480, H: 52}, func() {
			u.TextField("Type and press Enter", &g.draft)
		})
		if g.reliable {
			u.Panel("Reliable UDP", ui.Rect{X: 512, Y: 16, W: 192, H: 120}, func() {
				u.Label(fmt.Sprintf("Sent %d", g.sent))
				u.Label(fmt.Sprintf("Received %d", g.received))
				order := "in order"
				if !g.inOrder {
					order = "OUT OF ORDER"
				}
				u.Label(order)
				if g.target != nil {
					if s, ok := g.udp.Stats(g.target); ok {
						u.Label(fmt.Sprintf("rtt %.1fms loss %.0f%%", float64(s.RTT)/1e6, s.Loss*100))
					}
				}
			})
		}
	})
	if ctx.Input.KeyPressed(input.KeyEnter) && g.draft != "" {
		msg := chat{From: g.name, Text: g.draft}
		g.draft = ""
		g.say(msg.From + ": " + msg.Text)
		g.sendChat(msg)
	}
	return nil
}
```

## main

`TurnBased: true` is the flag that changes the loop. Everything else is
the usual configuration.

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	listen := flag.String("listen", "", "host on this address, like :7777")
	joinTo := flag.String("join", "", "connect to a host, like 192.168.1.5:7777")
	name := flag.String("name", "player", "your name")
	bot := flag.Bool("bot", false, "send a greeting once connected (20 numbered lines with -reliable)")
	reliable := flag.Bool("reliable", false, "send chat over UDP with SendReliable and count lines delivered in order")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip network", Width: 720, Height: 480, TurnBased: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, listen: *listen, joinTo: *joinTo, name: *name, bot: *bot, reliable: *reliable})
	if err != nil {
		fmt.Fprintln(os.Stderr, "network:", err)
		os.Exit(1)
	}
}
```

## What to try

- Start a host and two clients and watch the host relay both the chat in
  `Update` and the pointers.
- Run with `-reliable -bot` on a client: it sends twenty numbered lines
  through `sendChat`, and the host's panel counts them in order.
- Drop `TurnBased` in `main` and compare the regular frame updates and CPU
  use with the event-driven loop.
- Remove the `SetOnActivity` calls in `Init` and see messages arrive only
  when the mouse moves.
- Send the pointer at a fixed rate from `Update` instead of on every
  move, which is what a game with many players does.
