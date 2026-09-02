package bunyip_test

import (
	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

// A game is a value with Update and Draw; Init and Shutdown are optional.
type game struct {
	x float32
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) {
		ctx.Quit()
	}
	g.x += 100 * float32(ctx.Delta) // Delta is the fixed step in real-time mode
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	ctx.Gfx.FillRect(g.x, 100, 40, 40, gfx.RGB(255, 200, 40))
	return nil
}

func ExampleRun() {
	err := bunyip.Run(bunyip.Config{Title: "Hello", Width: 960, Height: 600, Resizable: true}, &game{})
	if err != nil {
		panic(err)
	}
}

// A turn-based game sleeps in the operating system until input arrives.
func ExampleRun_turnBased() {
	err := bunyip.Run(bunyip.Config{Title: "Roguelike", Width: 960, Height: 600, TurnBased: true}, &game{})
	if err != nil {
		panic(err)
	}
}
