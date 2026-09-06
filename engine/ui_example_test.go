package engine_test

import (
	"github.com/matjam/bunyip/engine"
	"github.com/matjam/bunyip/ui"
)

func ExampleContext_NewUI() {
	var menu *ui.Context
	g := engine.GameFuncs{
		InitFunc: func(ctx *engine.Context) error {
			var err error
			menu, err = ctx.NewUI(ui.Theme{})
			return err
		},
		DrawFunc: func(ctx *engine.Context) error {
			menu.Begin(ctx.Input, func() {
				menu.Panel("Menu", ui.Rect{X: 8, Y: 8, W: 160, H: 70}, func() {
					menu.Label("Ready")
				})
			})
			return nil
		},
	}
	err := engine.Run(engine.Config{Title: "Interface", Width: 640, Height: 480}, g)
	if err != nil {
		panic(err)
	}
}
