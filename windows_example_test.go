package bunyip_test

import (
	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
)

func ExampleContext_NewWindow() {
	bunyip.Run(bunyip.Config{Title: "Editor"}, bunyip.GameFuncs{
		InitFunc: func(ctx *bunyip.Context) error {
			_, err := ctx.NewWindow(bunyip.Config{Title: "Preview", Width: 480, Height: 320}, bunyip.GameFuncs{
				DrawFunc: func(preview *bunyip.Context) error {
					preview.Gfx.FillRect(20, 20, 100, 100, gfx.RGB(100, 180, 255))
					return nil
				},
			})
			return err
		},
	})
}
