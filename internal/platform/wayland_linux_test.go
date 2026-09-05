package platform

import (
	"image"
	"image/color"
	"testing"
)

// signatureArgs counts the arguments a wayland-scanner signature
// describes: leading digits are the version the message arrived in and
// "?" marks the argument that follows as nullable, so neither is one.
func signatureArgs(sig string) int {
	n := 0
	for i, c := range sig {
		switch {
		case c >= '0' && c <= '9' && i == n:
			continue // the since version, only ever at the front
		case c == '?':
			continue
		}
		n++
	}
	return n
}

// The types list is read in step with the signature, so a table whose
// two disagree points at the wrong interface for an object argument, or
// past the end of the list.
func TestProtocolTablesMatchTheirSignatures(t *testing.T) {
	for _, iface := range wlProtocols {
		for _, group := range []struct {
			kind string
			msgs []protoMessage
		}{{"request", iface.methods}, {"event", iface.events}} {
			for i, m := range group.msgs {
				want := signatureArgs(m.sig)
				if len(m.types) != 0 && len(m.types) != want {
					t.Errorf("%s %s %s (opcode %d): signature %q has %d arguments, types has %d",
						iface.name, group.kind, m.name, i, m.sig, want, len(m.types))
				}
			}
		}
	}
}

func TestSquareIcon(t *testing.T) {
	// A wide image is padded top and bottom and centred.
	wide := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for x := 0; x < 8; x++ {
		for y := 0; y < 4; y++ {
			wide.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	out := squareIcon(wide)
	if got := out.Bounds(); got != image.Rect(0, 0, 8, 8) {
		t.Fatalf("bounds = %v, want 8x8", got)
	}
	if _, _, _, alpha := out.At(0, 0).RGBA(); alpha != 0 {
		t.Errorf("the padding at the top has alpha %d, want 0", alpha)
	}
	if _, _, _, alpha := out.At(0, 7).RGBA(); alpha != 0 {
		t.Errorf("the padding at the bottom has alpha %d, want 0", alpha)
	}
	for y := 2; y < 6; y++ {
		r, _, _, alpha := out.At(4, y).RGBA()
		if alpha == 0 || r == 0 {
			t.Errorf("row %d of the image is missing: r=%d alpha=%d", y, r, alpha)
		}
	}

	// A tall image is padded left and right.
	tall := squareIcon(image.NewNRGBA(image.Rect(0, 0, 3, 9)))
	if got := tall.Bounds(); got != image.Rect(0, 0, 9, 9) {
		t.Errorf("bounds = %v, want 9x9", got)
	}

	// A square image is handed back as it is, with no copy.
	square := image.NewNRGBA(image.Rect(0, 0, 6, 6))
	if squareIcon(square) != image.Image(square) {
		t.Error("a square image was copied, want it returned as it is")
	}

	// An image with an offset origin still lands in a square starting at
	// the origin, which is what the buffer needs.
	offset := squareIcon(image.NewNRGBA(image.Rect(10, 20, 14, 22)))
	if got := offset.Bounds(); got != image.Rect(0, 0, 4, 4) {
		t.Errorf("bounds = %v, want 4x4 at the origin", got)
	}
}
