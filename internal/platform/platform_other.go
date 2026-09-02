//go:build !darwin && !windows && !linux

package platform

import (
	"errors"
	"image"

	"github.com/matjam/bunyip/internal/vk"
)

// ErrUnsupported is returned on platforms without a window layer yet.
var ErrUnsupported = errors.New("platform: no window layer for this operating system yet")

type App struct{}

type Window struct{}

func NewApp() (*App, error)                                            { return nil, ErrUnsupported }
func (a *App) Poll(wait bool) []Event                                  { return nil }
func (a *App) NewWindow(cfg Config) (*Window, error)                   { return nil, ErrUnsupported }
func (w *Window) Size() (int, int)                                     { return 0, 0 }
func (w *Window) PixelSize() (int, int)                                { return 0, 0 }
func (w *Window) Scale() float64                                       { return 1 }
func (w *Window) Closed() bool                                         { return true }
func (w *Window) Close()                                               {}
func RequiredInstanceExtensions() []string                             { return nil }
func (w *Window) CreateSurface(vk.VkInstance) (vk.VkSurfaceKHR, error) { return 0, ErrUnsupported }

func (w *Window) Fullscreen() bool                             { return false }
func (w *Window) SetFullscreen(bool)                           {}
func (w *Window) SetCursorCaptured(bool)                       {}
func (w *Window) SetTextInputRect(x, y, width, height float64) {}
func (w *Window) CursorCaptured() bool                         { return false }
func (w *Window) SetTitle(string)                              {}
func (w *Window) SetSizeLimits(minW, minH, maxW, maxH int)     {}
func (w *Window) SetCursorVisible(bool)                        {}
func (w *Window) SetCursor(CursorShape)                        {}
func (w *Window) SetIcon(image.Image)                          {}
func (a *App) Gamepads() []GamepadState                        { return nil }
func (a *App) Wake()                                           {}
func (a *App) Clipboard() (string, error)                      { return "", ErrNoClipboard }
func (a *App) SetClipboard(string) error                       { return ErrNoClipboard }
