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
	// Quarters of a tile: 0 TL, 1 TR, 2 BL, 3 BR.
	quarter := func(q int) image.Point { return image.Pt(q%2*half, q/2*half) }
	// blit fills quadrant dq of the output tile at dst from quadrant sq
	// of template tile (tx, ty). An edge piece reads a different quarter
	// than it fills: the left edge of the output's top-left quadrant is
	// the block's lower-left quarter, where the rim runs on without a
	// corner.
	blit := func(dst image.Point, dq, tx, ty, sq int) {
		o := template.Bounds().Min.Add(image.Pt(tx*tile, ty*tile)).Add(quarter(sq))
		d := dst.Add(quarter(dq))
		draw.Draw(out, image.Rect(d.X, d.Y, d.X+half, d.Y+half), template, o, draw.Src)
	}
	// Template tiles.
	const (
		inner = iota // inside corners
		_            // preview, unused
		blockTL
		blockTR
		blockBL
		blockBR
	)
	pos := [6][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0, 2}, {1, 2}}
	from := func(dst image.Point, dq, t, sq int) { blit(dst, dq, pos[t][0], pos[t][1], sq) }
	for i, mask := range BlobMasks() {
		frames[i] = i
		dst := image.Pt(i%columns*tile, i/columns*tile)
		n := mask&(1<<DirN) != 0
		e := mask&(1<<DirE) != 0
		s := mask&(1<<DirS) != 0
		w := mask&(1<<DirW) != 0
		// Each quadrant is shaped by its two edge neighbours, with the
		// diagonal deciding interior against inside corner. Interior
		// quarters come from the diagonally opposite block tile.
		switch { // top-left: north and west
		case !n && !w:
			from(dst, 0, blockTL, 0) // outer corner
		case n && !w:
			from(dst, 0, blockTL, 2) // left edge runs on
		case !n && w:
			from(dst, 0, blockTL, 1) // top edge runs on
		case mask&(1<<DirNW) != 0:
			from(dst, 0, blockBR, 0)
		default:
			from(dst, 0, inner, 0)
		}
		switch { // top-right: north and east
		case !n && !e:
			from(dst, 1, blockTR, 1)
		case n && !e:
			from(dst, 1, blockTR, 3)
		case !n && e:
			from(dst, 1, blockTR, 0)
		case mask&(1<<DirNE) != 0:
			from(dst, 1, blockBL, 1)
		default:
			from(dst, 1, inner, 1)
		}
		switch { // bottom-left: south and west
		case !s && !w:
			from(dst, 2, blockBL, 2)
		case s && !w:
			from(dst, 2, blockBL, 0)
		case !s && w:
			from(dst, 2, blockBL, 3)
		case mask&(1<<DirSW) != 0:
			from(dst, 2, blockTR, 2)
		default:
			from(dst, 2, inner, 2)
		}
		switch { // bottom-right: south and east
		case !s && !e:
			from(dst, 3, blockBR, 3)
		case s && !e:
			from(dst, 3, blockBR, 1)
		case !s && e:
			from(dst, 3, blockBR, 2)
		case mask&(1<<DirSE) != 0:
			from(dst, 3, blockTL, 3)
		default:
			from(dst, 3, inner, 3)
		}
	}
	return out, frames
}
