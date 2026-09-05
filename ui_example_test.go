package bunyip_test

import (
	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/ui"
)

func ExampleContext_NewUI() {
	var menu *ui.Context
	g := bunyip.GameFuncs{
		InitFunc: func(ctx *bunyip.Context) error {
			var err error
			menu, err = ctx.NewUI(ui.Theme{})
			return err
		},
		DrawFunc: func(ctx *bunyip.Context) error {
			menu.Begin(ctx.Input, func() {
				menu.Panel("Menu", ui.Rect{X: 8, Y: 8, W: 160, H: 70}, func() {
					menu.Label("Ready")
				})
			})
			return nil
		},
	}
	err := bunyip.Run(bunyip.Config{Title: "Interface", Width: 640, Height: 480}, g)
	if err != nil {
		panic(err)
	}
}
