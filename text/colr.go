// SPDX-License-Identifier: Unlicense OR MIT

package text

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	gotextot "github.com/go-text/typesetting/font/opentype"
)

// foregroundIndex is the palette index of a layer drawn in the color of the
// text instead of one of the palette.
const foregroundIndex = 0xFFFF

// colorGlyphOutline returns what Shape draws of a color glyph, in the color
// of the text: the layers of a COLR version 0 glyph that ask for that color,
// and the glyph's own outline when its paint is one colorGlyphImage does not
// draw (COLR version 1), so that it shows in one color instead of not at all.
func colorGlyphOutline(face *font.Face, gid font.GID, glyph font.GlyphColor) font.GlyphOutline {
	layers, ok := glyph.Paint.(tables.PaintColrLayersResolved)
	if !ok {
		outline, _ := face.GlyphDataOutline(tables.GlyphID(gid))
		return outline
	}
	var outline font.GlyphOutline
	for _, l := range layers {
		if l.PaletteIndex != foregroundIndex {
			continue
		}
		if o, ok := face.GlyphDataOutline(l.GlyphID); ok {
			outline.Segments = append(outline.Segments, o.Segments...)
		}
	}
	return outline
}

// colorGlyphImage draws the layers of a COLR version 0 glyph, as Segoe UI
// Emoji has them, at ppem pixels per em: each layer is the outline of a
// glyph filled with a color of the font's first palette. The image's origin
// is at off from the glyph's, in pixels with y down. Layers in the color of
// the text are left to colorGlyphOutline. It reports false for other paints
// and for a glyph with nothing to draw.
func colorGlyphImage(face *font.Face, glyph font.GlyphColor, ppem fixed.Int26_6) (img *image.RGBA, off image.Point, ok bool) {
	layers, isLayers := glyph.Paint.(tables.PaintColrLayersResolved)
	if !isLayers || len(face.CPAL) == 0 {
		return nil, image.Point{}, false
	}
	palette := face.CPAL[0]
	scale := fixedToFloat(ppem) / float32(face.Upem())

	type layer struct {
		outline font.GlyphOutline
		color   color.NRGBA
	}
	var (
		draws                  []layer
		minX, minY, maxX, maxY = float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.Inf(-1))
	)
	for _, l := range layers {
		if l.PaletteIndex == foregroundIndex || int(l.PaletteIndex) >= len(palette) {
			continue
		}
		outline, ok := face.GlyphDataOutline(l.GlyphID)
		if !ok || len(outline.Segments) == 0 {
			continue
		}
		// The control points of a curve bound it.
		for _, seg := range outline.Segments {
			for _, p := range seg.ArgsSlice() {
				x, y := p.X*scale, -p.Y*scale
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
		c := palette[l.PaletteIndex]
		draws = append(draws, layer{outline, color.NRGBA{R: c.Red, G: c.Green, B: c.Blue, A: c.Alpha}})
	}
	if len(draws) == 0 {
		return nil, image.Point{}, false
	}
	bounds := image.Rect(int(math.Floor(float64(minX))), int(math.Floor(float64(minY))), int(math.Ceil(float64(maxX))), int(math.Ceil(float64(maxY))))
	if bounds.Empty() {
		return nil, image.Point{}, false
	}
	off = bounds.Min
	img = image.NewRGBA(image.Rectangle{Max: bounds.Size()})
	raster := vector.NewRasterizer(img.Rect.Dx(), img.Rect.Dy())
	raster.DrawOp = draw.Over
	at := func(p gotextot.SegmentPoint) (x, y float32) {
		return p.X*scale - float32(off.X), -p.Y*scale - float32(off.Y)
	}
	for _, l := range draws {
		raster.Reset(img.Rect.Dx(), img.Rect.Dy())
		for i, seg := range l.outline.Segments {
			switch seg.Op {
			case gotextot.SegmentOpMoveTo:
				// The rasterizer does not close a contour on its own.
				if i > 0 {
					raster.ClosePath()
				}
				x, y := at(seg.Args[0])
				raster.MoveTo(x, y)
			case gotextot.SegmentOpLineTo:
				x, y := at(seg.Args[0])
				raster.LineTo(x, y)
			case gotextot.SegmentOpQuadTo:
				bx, by := at(seg.Args[0])
				cx, cy := at(seg.Args[1])
				raster.QuadTo(bx, by, cx, cy)
			case gotextot.SegmentOpCubeTo:
				bx, by := at(seg.Args[0])
				cx, cy := at(seg.Args[1])
				dx, dy := at(seg.Args[2])
				raster.CubeTo(bx, by, cx, cy, dx, dy)
			}
		}
		raster.ClosePath()
		raster.Draw(img, img.Rect, image.NewUniform(l.color), image.Point{})
	}
	return img, off, true
}
