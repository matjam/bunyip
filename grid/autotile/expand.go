package autotile

import (
	"image"
	"image/draw"
)

// ExpandBlob composes the 47 blob tiles from a six-tile template, so an
// artist draws six tiles instead of 47. The template is two tiles wide
// and three tall, each tile square with the given side:
//
//	inner  preview
//	 TL      TR
//	 BL      BR
//
// TL, TR, BL and BR are the four corner tiles of a filled two-by-two
// block, which supply the outer corners, the edges and the interior.
// The inner tile holds the four inside-corner pieces, drawn as if a
// hole sat at each diagonal. The preview tile is not read; RPG Maker
// style autotile blocks put a display tile there.
//
// Every output tile is built from four quarter-tiles chosen by the
// neighbour mask. The result is a sheet eight tiles wide holding the 47
// tiles in canonical Blob47 order, and the matching frames array:
// upload it, cut it with a sheet of the same tile size, and pass the
// frames to Blob47.
func ExpandBlob(template image.Image, tile int) (*image.RGBA, [47]int) {
	const columns = 8
	rows := (47 + columns - 1) / columns
	out := image.NewRGBA(image.Rect(0, 0, columns*tile, rows*tile))
	var frames [47]int
	half := tile / 2
	// src copies one quarter of a template tile into one quarter of an
	// output tile. Quarters are 0 TL, 1 TR, 2 BL, 3 BR.
	src := func(dst image.Point, tx, ty, q int) {
		o := template.Bounds().Min
		sp := image.Pt(o.X+tx*tile+q%2*half, o.Y+ty*tile+q/2*half)
		r := image.Rect(dst.X+q%2*half, dst.Y+q/2*half, dst.X+q%2*half+half, dst.Y+q/2*half+half)
		draw.Draw(out, r, template, sp, draw.Src)
	}
	for i, mask := range BlobMasks() {
		frames[i] = i
		dst := image.Pt(i%columns*tile, i/columns*tile)
		n := mask&(1<<DirN) != 0
		e := mask&(1<<DirE) != 0
		s := mask&(1<<DirS) != 0
		w := mask&(1<<DirW) != 0
		// Top-left quarter: shaped by the north and west neighbours,
		// with the diagonal deciding interior against inside corner.
		switch {
		case !n && !w:
			src(dst, 0, 1, 0) // outer corner from block TL
		case n && !w:
			src(dst, 0, 1, 2) // left edge continues
		case !n && w:
			src(dst, 0, 1, 1) // top edge continues
		case mask&(1<<DirNW) != 0:
			src(dst, 1, 2, 0) // interior from block BR
		default:
			src(dst, 0, 0, 0) // inside corner
		}
		switch { // top-right quarter: north and east
		case !n && !e:
			src(dst, 1, 1, 1)
		case n && !e:
			src(dst, 1, 1, 3)
		case !n && e:
			src(dst, 1, 1, 0)
		case mask&(1<<DirNE) != 0:
			src(dst, 0, 2, 1)
		default:
			src(dst, 0, 0, 1)
		}
		switch { // bottom-left quarter: south and west
		case !s && !w:
			src(dst, 0, 2, 2)
		case s && !w:
			src(dst, 0, 2, 0)
		case !s && w:
			src(dst, 0, 2, 3)
		case mask&(1<<DirSW) != 0:
			src(dst, 1, 1, 2)
		default:
			src(dst, 0, 0, 2)
		}
		switch { // bottom-right quarter: south and east
		case !s && !e:
			src(dst, 1, 2, 3)
		case s && !e:
			src(dst, 1, 2, 1)
		case !s && e:
			src(dst, 1, 2, 2)
		case mask&(1<<DirSE) != 0:
			src(dst, 0, 1, 3)
		default:
			src(dst, 0, 0, 3)
		}
	}
	return out, frames
}
