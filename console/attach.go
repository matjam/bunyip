package console

import (
	"time"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/input"
)

// attachments are the things a game has given the console to show. The
// engine attaches what it owns through Frame; everything here is the
// game's.
type attachments struct {
	worlds  []attachedWorld
	actions []attachedActions
	info    []attachedInfo
	links   []attachedLinks
}

type attachedWorld struct {
	name  string
	world *ecs.World
}

type attachedActions struct {
	name    string
	actions *input.Actions
}

type attachedInfo struct {
	name string
	text func() string
}

type attachedLinks struct {
	name  string
	links func() []Link
}

// Attach adds an entity world to the Entities and Physics panels under a
// name. Attach several worlds and the panels show a row of them to pick
// from. Attaching the same name again replaces that world.
func (c *Console) Attach(name string, w *ecs.World) {
	if c == nil {
		return
	}
	if w == nil {
		return
	}
	for i := range c.attach.worlds {
		if c.attach.worlds[i].name == name {
			c.attach.worlds[i].world = w
			return
		}
	}
	c.attach.worlds = append(c.attach.worlds, attachedWorld{name: name, world: w})
}

// Worlds returns the attached worlds' names, in the order they were
// attached.
func (c *Console) Worlds() []string {
	if c == nil {
		return nil
	}
	out := make([]string, len(c.attach.worlds))
	for i, w := range c.attach.worlds {
		out[i] = w.name
	}
	return out
}

// AttachActions adds an action map to the Input panel, which lists every
// bound action with its sources and its value this frame.
func (c *Console) AttachActions(name string, a *input.Actions) {
	if c == nil {
		return
	}
	if a == nil {
		return
	}
	for i := range c.attach.actions {
		if c.attach.actions[i].name == name {
			c.attach.actions[i].actions = a
			return
		}
	}
	c.attach.actions = append(c.attach.actions, attachedActions{name: name, actions: a})
}

// AttachInfo adds a line to the Services panel, redrawn from text every
// frame. It is how a game shows a service the console knows nothing
// about: the locale, a save slot, a scheduler's timer count.
//
//	con.AttachInfo("locale", func() string { return tr.Lang() })
//	con.AttachInfo("timers", func() string { return strconv.Itoa(sched.Pending()) })
func (c *Console) AttachInfo(name string, text func() string) {
	if c == nil {
		return
	}
	if text == nil {
		return
	}
	for i := range c.attach.info {
		if c.attach.info[i].name == name {
			c.attach.info[i].text = text
			return
		}
	}
	c.attach.info = append(c.attach.info, attachedInfo{name: name, text: text})
}

// Link is one network link as the Services panel shows it.
type Link struct {
	Peer      string        // who the link is to
	RTT       time.Duration // round trip time
	Loss      float32       // fraction of packets lost, 0 to 1
	Pending   int           // messages sent and not yet acknowledged
	Connected bool
}

// AttachLinks adds a connection's links to the Services panel. The
// console does not depend on the network package, so the game reads the
// statistics it wants shown:
//
//	con.AttachLinks("server", func() []console.Link {
//		var out []console.Link
//		for _, a := range peer.Peers() {
//			s, _ := peer.Stats(a)
//			out = append(out, console.Link{Peer: a.String(), RTT: s.RTT,
//				Loss: s.Loss, Pending: s.Pending, Connected: s.Connected})
//		}
//		return out
//	})
func (c *Console) AttachLinks(name string, links func() []Link) {
	if c == nil {
		return
	}
	if links == nil {
		return
	}
	for i := range c.attach.links {
		if c.attach.links[i].name == name {
			c.attach.links[i].links = links
			return
		}
	}
	c.attach.links = append(c.attach.links, attachedLinks{name: name, links: links})
}

// Prefabs is a named set of prefabs the Entities panel can spawn. Set it
// as a resource on a world and the panel lists the names with a button
// each:
//
//	ecs.SetResource(w, console.Prefabs{"goblin": goblinPrefab})
type Prefabs map[string]*ecs.Prefab
