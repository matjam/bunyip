// Command network is a chat room over the network package. Run one copy
// with -listen and others with -join; typed lines travel over TCP and
// every peer's pointer position is shared over UDP. Both sides run in
// turn-based mode and sleep until input or a message arrives, which the
// network's activity hook signals through Context.Wake.
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
type chat struct{ From, Text string }
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
	seconds float64
	shot    string
	listen  string
	joinTo  string
	name    string
	bot     bool

	font     *gfx.Font
	ui       *ui.Context
	reg      *network.Registry
	server   *network.Server
	client   *network.Client
	udp      *network.Peer
	peers    map[string]*network.Addr // UDP addresses to fan pointers out to (server)
	target   *network.Addr            // where a client sends pointers
	lines    []string
	draft    string
	points   map[string]pointer
	mouseX   float32
	mouseY   float32
	shotDone bool
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

func (g *game) say(line string) {
	g.lines = append(g.lines, line)
	if len(g.lines) > 14 {
		g.lines = g.lines[1:]
	}
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
					if g.bot {
						g.client.Send(chat{From: g.name, Text: "hello from a bot"})
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
		if p, ok := ev.Msg.(*pointer); ok {
			g.points[p.Name] = *p
			if g.server != nil { // fan out to every other peer
				g.peers[ev.From.String()] = ev.From
				for key, addr := range g.peers {
					if key != ev.From.String() {
						g.udp.Send(addr, *p)
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
		if g.server != nil {
			for _, addr := range g.peers {
				g.udp.Send(addr, me)
			}
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
	u.Begin(ctx.Input)
	u.Panel("Chat", ui.Rect{X: 16, Y: 16, W: 480, H: 380})
	for _, l := range g.lines {
		u.Label(l)
	}
	u.EndPanel()
	u.Panel("", ui.Rect{X: 16, Y: 404, W: 480, H: 52})
	u.TextField("Type and press Enter", &g.draft)
	u.EndPanel()
	u.End()
	if ctx.Input.KeyPressed(input.KeyEnter) && g.draft != "" {
		msg := chat{From: g.name, Text: g.draft}
		g.draft = ""
		g.say(msg.From + ": " + msg.Text)
		if g.client != nil {
			g.client.Send(msg)
		}
		if g.server != nil {
			g.server.Broadcast(msg)
		}
	}
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	listen := flag.String("listen", "", "host on this address, like :7777")
	joinTo := flag.String("join", "", "connect to a host, like 192.168.1.5:7777")
	name := flag.String("name", "player", "your name")
	bot := flag.Bool("bot", false, "send a greeting once connected")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip network", Width: 720, Height: 480, TurnBased: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, listen: *listen, joinTo: *joinTo, name: *name, bot: *bot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "network:", err)
		os.Exit(1)
	}
}
