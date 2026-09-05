// Command bunyip-tex compresses PNG and JPEG images into KTX2 texture
// files that asset.Texture and gfx.NewCompressedTexture upload straight
// to the GPU, with the whole mip chain built here rather than while a
// game runs.
//
// Usage:
//
//	bunyip-tex [flags] file...
//
// Each input writes <name>.ktx2 beside it, or into -outdir, or to -o
// when there is one input. The flags:
//
//	-format bc1|bc3|bc4|bc5|bc7|rgba   what to compress to (default bc7)
//	-linear                            the file holds data, not sRGB colour
//	-no-mips                           write level 0 alone
//	-fast                              skip BC7's two-subset search
//	-o file                            write one output here
//	-outdir dir                        write every output into this directory
//	-v                                 report each file's size and quality
//
// Pick the format by what the texture is for: bc7 for colour that has to
// hold up close, bc1 for opaque colour at a quarter of the size, bc3 for
// colour with alpha at half of bc7's, bc4 for a one-channel mask or
// height field and bc5 for a tangent-space normal map. A normal map, a
// mask or a roughness map is data rather than colour, so it wants
// -linear; bc4 and bc5 are always linear whatever the flag says.
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/matjam/bunyip/gfx/ktx2"
)

func main() { os.Exit(run()) }

// run does the work and returns the process's exit status, so the
// deferred cleanups in the helpers still happen.
func run() int {
	fs := flag.NewFlagSet("bunyip-tex", flag.ExitOnError)
	format := fs.String("format", "bc7", "block format: bc1, bc3, bc4, bc5, bc7 or rgba")
	linear := fs.Bool("linear", false, "the image holds data rather than sRGB colour")
	noMips := fs.Bool("no-mips", false, "write level 0 alone instead of the whole mip chain")
	fast := fs.Bool("fast", false, "keep BC7 to its one-subset mode, which is quicker and a little worse")
	out := fs.String("o", "", "write the one output to this file")
	outdir := fs.String("outdir", "", "write every output into this directory")
	verbose := fs.Bool("v", false, "report each file's size and quality")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: bunyip-tex [flags] file...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	if *out != "" && fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "bunyip-tex: -o takes one input file; use -outdir for several")
		return 2
	}
	f, err := ktx2.Named(*format, *linear)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-tex:", err)
		return 2
	}
	opts := ktx2.Options{Format: f, NoMipmaps: *noMips, Fast: *fast}
	status := 0
	for _, name := range fs.Args() {
		dst := *out
		if dst == "" {
			base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)) + ".ktx2"
			dir := *outdir
			if dir == "" {
				dir = filepath.Dir(name)
			}
			dst = filepath.Join(dir, base)
		}
		if err := convert(name, dst, opts, *verbose); err != nil {
			fmt.Fprintln(os.Stderr, "bunyip-tex:", err)
			status = 1
		}
	}
	return status
}

// convert compresses one image and writes the file.
func convert(src, dst string, opts ktx2.Options, verbose bool) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	start := time.Now()
	tex, err := ktx2.Encode(img, opts)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	data, err := tex.Bytes()
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	if dir := filepath.Dir(dst); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	if !verbose {
		fmt.Printf("%s -> %s\n", src, dst)
		return nil
	}
	return report(src, dst, img, tex, len(data), time.Since(start))
}

// report prints what one conversion cost and how close it came, so a
// choice of format can be judged rather than guessed at.
func report(src, dst string, orig image.Image, tex *ktx2.File, size int, took time.Duration) error {
	b := orig.Bounds()
	raw := b.Dx() * b.Dy() * 4
	line := fmt.Sprintf("%s -> %s  %dx%d %s  %d levels  %s (%.0f%% of raw)  %s",
		src, dst, tex.Width, tex.Height, tex.Format, len(tex.Levels),
		bytesText(size), 100*float64(size)/float64(raw), took.Round(time.Millisecond))
	if !tex.Decodable() {
		fmt.Println(line)
		return nil
	}
	// The quality figure compares the level the encoder was given against
	// the level it wrote, so it measures the compression alone.
	want, err := ktx2.Encode(orig, ktx2.Options{Format: rawFormat(tex.Format), NoMipmaps: true})
	if err != nil {
		return err
	}
	before, err := want.DecodeLevel(0)
	if err != nil {
		return err
	}
	after, err := tex.DecodeLevel(0)
	if err != nil {
		return err
	}
	channels := 4
	switch tex.Format {
	case ktx2.BC1RGBUnorm, ktx2.BC1RGBSRGB, ktx2.BC1RGBAUnorm, ktx2.BC1RGBASRGB:
		channels = 3
	case ktx2.BC4Unorm:
		channels = 1
	case ktx2.BC5Unorm:
		channels = 2
	}
	fmt.Printf("%s  %.2f dB\n", line, ktx2.PSNR(before, after, channels))
	return nil
}

// rawFormat is the uncompressed format matching a compressed one's
// colour handling, for encoding the reference the quality figure
// measures against.
func rawFormat(f ktx2.Format) ktx2.Format {
	if f.SRGB() {
		return ktx2.R8G8B8A8SRGB
	}
	return ktx2.R8G8B8A8Unorm
}

// bytesText renders a byte count in the largest unit that keeps it above
// one.
func bytesText(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
