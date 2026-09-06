package engine_test

import (
	"github.com/matjam/bunyip/engine"
	"github.com/matjam/bunyip/gfx"
)

func ExampleContext_NewWindow() {
	engine.Run(engine.Config{Title: "Editor"}, engine.GameFuncs{
		InitFunc: func(ctx *engine.Context) error {
			_, err := ctx.NewWindow(engine.Config{Title: "Preview", Width: 480, Height: 320}, engine.GameFuncs{
				DrawFunc: func(preview *engine.Context) error {
					preview.Gfx.FillRect(20, 20, 100, 100, gfx.RGB(100, 180, 255))
					return nil
				},
			})
			return err
		},
	})
}
