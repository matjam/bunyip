package gfx

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"math"
	"strings"
)

// HDRImage is a floating-point RGB image: linear radiance, no gamma,
// values above 1 for bright light sources. DecodeHDR reads one from a
// Radiance .hdr file and NewEnvironmentHDR lights a scene with it.
type HDRImage struct {
	Width, Height int
	Pix           []float32 // row-major RGB
}

// At returns the radiance at a pixel.
func (h *HDRImage) At(x, y int) (r, g, b float32) {
	i := (y*h.Width + x) * 3
	return h.Pix[i], h.Pix[i+1], h.Pix[i+2]
}

// DecodeHDR reads a Radiance RGBE (.hdr) file, flat or run-length
// encoded, as most panoramas are distributed.
func DecodeHDR(data []byte) (*HDRImage, error) {
	br := bufio.NewReader(bytes.NewReader(data))
	magic, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(magic, "#?") {
		return nil, fmt.Errorf("gfx: not a Radiance HDR file")
	}
	// Header lines until a blank one; FORMAT must be 32-bit_rle_rgbe.
	format := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("gfx: hdr header: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "FORMAT=") {
			format = strings.TrimPrefix(line, "FORMAT=")
		}
	}
	if format != "" && format != "32-bit_rle_rgbe" {
		return nil, fmt.Errorf("gfx: hdr format %q is not supported", format)
	}
	res, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("gfx: hdr resolution: %w", err)
	}
	var ya, xa string
	var h, w int
	if _, err := fmt.Sscanf(strings.TrimSpace(res), "%s %d %s %d", &ya, &h, &xa, &w); err != nil || ya != "-Y" || xa != "+X" {
		return nil, fmt.Errorf("gfx: hdr resolution line %q is not supported (want -Y h +X w)", strings.TrimSpace(res))
	}
	if w <= 0 || h <= 0 || w*h > 1<<28 {
		return nil, fmt.Errorf("gfx: hdr size %dx%d", w, h)
	}
	img := &HDRImage{Width: w, Height: h, Pix: make([]float32, w*h*3)}
	scan := make([]byte, w*4)
	for y := range h {
		if err := readScanline(br, scan, w); err != nil {
			return nil, fmt.Errorf("gfx: hdr row %d: %w", y, err)
		}
		for x := range w {
			e := scan[x*4+3]
			if e == 0 {
				continue
			}
			f := float32(math.Ldexp(1, int(e)-136)) // 2^(e-128) / 256
			i := (y*w + x) * 3
			img.Pix[i] = float32(scan[x*4]) * f
			img.Pix[i+1] = float32(scan[x*4+1]) * f
			img.Pix[i+2] = float32(scan[x*4+2]) * f
		}
	}
	return img, nil
}

// readScanline fills scan with w RGBE pixels, decoding the new-style
// run-length encoding when the row starts with its marker.
func readScanline(br *bufio.Reader, scan []byte, w int) error {
	var head [4]byte
	if _, err := io.ReadFull(br, head[:]); err != nil {
		return err
	}
	if head[0] != 2 || head[1] != 2 || head[2]&0x80 != 0 || w < 8 || w > 0x7fff {
		// Flat: the four bytes are the first pixel.
		copy(scan, head[:])
		_, err := io.ReadFull(br, scan[4:])
		return err
	}
	if int(head[2])<<8|int(head[3]) != w {
		return fmt.Errorf("scanline width mismatch")
	}
	// Four planes, each run-length encoded.
	for c := range 4 {
		x := 0
		for x < w {
			n, err := br.ReadByte()
			if err != nil {
				return err
			}
			if n > 128 {
				count := int(n) - 128
				v, err := br.ReadByte()
				if err != nil {
					return err
				}
				if x+count > w {
					return fmt.Errorf("run overflows scanline")
				}
				for range count {
					scan[x*4+c] = v
					x++
				}
			} else {
				count := int(n)
				if count == 0 || x+count > w {
					return fmt.Errorf("bad run length")
				}
				for range count {
					v, err := br.ReadByte()
					if err != nil {
						return err
					}
					scan[x*4+c] = v
					x++
				}
			}
		}
	}
	return nil
}

// DecodePanorama reads an equirectangular panorama from encoded bytes,
// whichever format it is in: an OpenEXR file, a Radiance .hdr file, or
// any image the program has registered a decoder for, whose sRGB colours
// are converted to linear radiance. Pass the result to NewEnvironmentHDR.
// A program that loads PNG or JPEG panoramas must import image/png or
// image/jpeg for their decoders, as it would for image.Decode.
func DecodePanorama(data []byte) (*HDRImage, error) {
	switch {
	case len(data) >= 4 && binary.LittleEndian.Uint32(data) == 20000630:
		return DecodeEXR(data)
	case len(data) >= 2 && data[0] == '#' && data[1] == '?':
		return DecodeHDR(data)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: panorama: %w", err)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("gfx: panorama has empty bounds")
	}
	out := &HDRImage{Width: b.Dx(), Height: b.Dy(), Pix: make([]float32, b.Dx()*b.Dy()*3)}
	for y := range out.Height {
		for x := range out.Width {
			r, g, bb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*out.Width + x) * 3
			out.Pix[i] = srgbToLinear(uint8(r >> 8))
			out.Pix[i+1] = srgbToLinear(uint8(g >> 8))
			out.Pix[i+2] = srgbToLinear(uint8(bb >> 8))
		}
	}
	return out, nil
}

// NewEnvironmentHDR builds an environment from a floating-point
// panorama, keeping its full range so a bright sun in it lights the
// scene as strongly as it should.
func (g *Graphics) NewEnvironmentHDR(panorama *HDRImage, opts EnvironmentOptions) (*Environment, error) {
	if panorama == nil || panorama.Width <= 0 || panorama.Height <= 0 {
		return nil, fmt.Errorf("gfx: environment image is empty")
	}
	src := &radianceMap{w: panorama.Width, h: panorama.Height, pix: panorama.Pix}
	return g.newEnvironment(src, opts)
}
