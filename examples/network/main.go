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
	ctx.Cleanup(func() { g.udp.Close() })
	g.udp.SetOnActivity(ctx.Wake)
	switch {
	case g.listen != "":
		if g.server, err = network.Listen(g.listen, g.reg); err != nil {
			return err
		}
		ctx.Cleanup(func() { g.server.Close() })
		g.server.SetOnActivity(ctx.Wake)
		g.say(fmt.Sprintf("Listening on %s (UDP %d). Others join with -join %s", g.server.Addr(), g.udp.Addr().Port, g.server.Addr()))
	case g.joinTo != "":
		if g.client, err = network.Dial(g.joinTo, g.reg, 5*time.Second); err != nil {
			return err
		}
		client := g.client // retain this connection even after a disconnect clears g.client
		ctx.Cleanup(func() { client.Close() })
		g.client.SetOnActivity(ctx.Wake)
		g.client.Send(join{Name: g.name})
		g.say("Connected to " + g.joinTo)
	default:
		g.say("Run with -listen :7777 to host, or -join host:7777 to connect.")
	}
	return nil
}

func (g *game) say(line string) {
	g.lines = append(g.lines, line)
	if len(g.lines) > 14 {
		g.lines = g.lines[1:]
	}
}

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

func splitHostPort(s string) (host, port string, err error) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return s[:i], s[i+1:], nil
		}
	}
	return s, "", fmt.Errorf("no port")
}

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
