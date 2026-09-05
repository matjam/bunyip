package platform

import (
	"fmt"
	"image"
	"sort"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Displays uses wl_output's advertised modes on Wayland and active RandR
// outputs on X11. wl_output geometry does not define reliable logical bounds,
// so Wayland leaves BoundsKnown false instead of guessing from pixel size.
func (a *App) Displays() ([]Display, error) {
	if a.wl != nil {
		outputs := make([]*wlOutput, 0, len(a.wl.outputs))
		for _, o := range a.wl.outputs {
			outputs = append(outputs, o)
		}
		sort.Slice(outputs, func(i, j int) bool { return outputs[i].name < outputs[j].name })
		out := make([]Display, 0, len(outputs))
		for _, o := range outputs {
			out = append(out, Display{Name: o.description, Scale: float64(o.scale), Current: o.current, Modes: append([]VideoMode(nil), o.modes...)})
		}
		return out, nil
	}
	return a.displaysX11()
}

type randrMode struct {
	ID                                                                         uint32
	Width, Height                                                              uint16
	DotClock                                                                   uint32
	HSyncStart, HSyncEnd, HTotal, HSkew, VSyncStart, VSyncEnd, VTotal, NameLen uint16
	Flags                                                                      uint32
}

func (m randrMode) videoMode() VideoMode {
	v := VideoMode{Width: int(m.Width), Height: int(m.Height)}
	if m.HTotal != 0 && m.VTotal != 0 {
		v.RefreshHz = float64(m.DotClock) / float64(m.HTotal) / float64(m.VTotal)
		if m.Flags&0x10 != 0 {
			v.RefreshHz *= 2
		}
		if m.Flags&0x20 != 0 {
			v.RefreshHz /= 2
		}
	}
	return v
}
func (a *App) displaysX11() ([]Display, error) {
	lib, err := purego.Dlopen("libxcb-randr.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("%w: RandR: %v", ErrUnsupported, err)
	}
	defer purego.Dlclose(lib)
	var resources func(unsafe.Pointer, uint32) xcbCookie
	var resourcesReply, outputReply, crtcReply func(unsafe.Pointer, xcbCookie, unsafe.Pointer) unsafe.Pointer
	var outputInfo, crtcInfo func(unsafe.Pointer, uint32, uint32) xcbCookie
	var outputs, outputModes func(unsafe.Pointer) *uint32
	var modes func(unsafe.Pointer) *randrMode
	var name func(unsafe.Pointer) *byte
	for n, p := range map[string]any{"xcb_randr_get_screen_resources_current": &resources, "xcb_randr_get_screen_resources_current_reply": &resourcesReply, "xcb_randr_get_screen_resources_current_outputs": &outputs, "xcb_randr_get_screen_resources_current_modes": &modes, "xcb_randr_get_output_info": &outputInfo, "xcb_randr_get_output_info_reply": &outputReply, "xcb_randr_get_output_info_modes": &outputModes, "xcb_randr_get_output_info_name": &name, "xcb_randr_get_crtc_info": &crtcInfo, "xcb_randr_get_crtc_info_reply": &crtcReply} {
		if err := load(lib, n, p); err != nil {
			return nil, err
		}
	}
	r := resourcesReply(a.conn, resources(a.conn, a.screen.Root), nil)
	if r == nil {
		return nil, fmt.Errorf("%w: RandR resources unavailable", ErrUnsupported)
	}
	defer a.x.free(r)
	timestamp := *(*uint32)(unsafe.Add(r, 12))
	nOut := *(*uint16)(unsafe.Add(r, 18))
	nMode := *(*uint16)(unsafe.Add(r, 20))
	byID := map[uint32]VideoMode{}
	for _, m := range unsafe.Slice(modes(r), int(nMode)) {
		byID[m.ID] = m.videoMode()
	}
	var result []Display
	for _, id := range unsafe.Slice(outputs(r), int(nOut)) {
		o := outputReply(a.conn, outputInfo(a.conn, id, timestamp), nil)
		if o == nil {
			return nil, fmt.Errorf("platform: RandR output query failed")
		}
		crtc := *(*uint32)(unsafe.Add(o, 12))
		connected := *(*byte)(unsafe.Add(o, 24)) == 0
		if !connected || crtc == 0 {
			a.x.free(o)
			continue
		}
		d := Display{Name: string(unsafe.Slice(name(o), int(*(*uint16)(unsafe.Add(o, 34))))), Scale: 1}
		for _, m := range unsafe.Slice(outputModes(o), int(*(*uint16)(unsafe.Add(o, 28)))) {
			if v, ok := byID[m]; ok {
				d.Modes = append(d.Modes, v)
			}
		}
		a.x.free(o)
		c := crtcReply(a.conn, crtcInfo(a.conn, crtc, timestamp), nil)
		if c == nil {
			return nil, fmt.Errorf("platform: RandR CRTC query failed")
		}
		x, y := int(*(*int16)(unsafe.Add(c, 12))), int(*(*int16)(unsafe.Add(c, 14)))
		w, h := int(*(*uint16)(unsafe.Add(c, 16))), int(*(*uint16)(unsafe.Add(c, 18)))
		d.Bounds = image.Rect(x, y, x+w, y+h)
		d.BoundsKnown = true
		d.Current = byID[*(*uint32)(unsafe.Add(c, 20))]
		a.x.free(c)
		result = append(result, d)
	}
	return result, nil
}
