package console_test

import (
	"fmt"
	"log/slog"

	"github.com/matjam/bunyip/console"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/input"
)

// In a game these come from the game's own state; the console itself
// comes from bunyip.Context.Console once Config.Console is set.
var (
	con     *console.Console
	world   *ecs.World
	actions *input.Actions
	speed   float32 = 6
	noclip  bool
)

// A game registers its commands, variables and worlds once, from Init.
func ExampleConsole_Register() {
	con.Register("give", "give <item> [count]: add an item to the pack",
		func(args []string) (string, error) {
			if len(args) == 0 {
				return "", fmt.Errorf("give: needs an item")
			}
			return "gave " + args[0], nil
		})
	con.Float("player.speed", &speed, "how fast the player runs")
	con.Bool("player.noclip", &noclip, "walk through walls")
	con.Attach("world", world)
	con.AttachActions("player", actions)
	con.AttachInfo("save slot", func() string { return "slot 2" })
}

// A console outside the engine's Config.Console: build it, tee the log
// through it, and hand it the frame every Draw.
func ExampleNew() {
	con := console.New(console.Options{Key: input.KeyF1, Height: 0.5})
	log := slog.New(con.Handler(slog.Default().Handler()))
	log.Info("the console is up")
	// Then, last of all in Draw:
	//	con.Draw(ctx)
}
