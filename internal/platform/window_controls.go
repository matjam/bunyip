package platform

import (
	"errors"
	"image"
	"image/draw"
)

// ErrUnsupported identifies an operation unavailable in the active backend.
var ErrUnsupported = errors.New("platform: operation unsupported by this backend")

// WindowCapabilities reports requests the active backend can issue. A supported
// request can still be declined by desktop policy or fail at runtime.
type WindowCapabilities struct {
	Resize, Show, Hide, Focus, AlwaysOnTop, CursorImage, PointerPosition bool
	EmbeddedBounds                                                       bool
}

// VideoMode describes a mode reported by the display system. Dimensions are
// physical pixels; RefreshHz is zero when the system does not report a rate.
type VideoMode struct {
	Width, Height int
	RefreshHz     float64
}

// Display describes an attached display. Bounds is in the backend's desktop
// coordinate space, with a top-left origin, and is valid only when BoundsKnown.
// Scale is logical-to-physical scaling, or zero when unknown. Modes contains
// only advertised modes, which may be limited to the current mode by the OS.
// Names and ordering are snapshots, not persistent device identifiers.
type Display struct {
	Name        string
	Bounds      image.Rectangle
	BoundsKnown bool
	Scale       float64
	Current     VideoMode
	Modes       []VideoMode
}

func cursorPixels(img image.Image, hotX, hotY int) ([]byte, int, int, error) {
	if img == nil {
		return nil, 0, 0, errors.New("platform: cursor image is nil")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil, 0, 0, errors.New("platform: cursor dimensions must be between 1 and 4096 pixels")
	}
	if hotX < 0 || hotY < 0 || hotX >= w || hotY >= h {
		return nil, 0, 0, errors.New("platform: cursor hotspot is outside the image")
	}
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	for i := 0; i < len(rgba.Pix); i += 4 {
		rgba.Pix[i], rgba.Pix[i+2] = rgba.Pix[i+2], rgba.Pix[i]
	}
	return rgba.Pix, w, h, nil
}
