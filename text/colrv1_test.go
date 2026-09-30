// SPDX-License-Identifier: Unlicense OR MIT

package text

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/go-text/typesetting/font"
	gotextot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// one is 1.0 as an F2DOT14 number.
const one = tables.Fixed214(1 << 14)

// The palette of the tests.
const (
	red = iota
	blue
	halfGreen
)

// testPainter paints with two glyphs: 1 is the square from (0, 0) to
// (100, 100) in font units, 2 the same 50 units to the right. Glyph 9 is a
// color glyph of two layers. layers are what PaintColrLayers lists.
func testPainter(layers ...tables.PaintTable) *colrPainter {
	square := func(x float32) font.GlyphOutline {
		pt := func(x, y float32) [3]gotextot.SegmentPoint { return [3]gotextot.SegmentPoint{{X: x, Y: y}} }
		return font.GlyphOutline{Segments: []gotextot.Segment{
			{Op: gotextot.SegmentOpMoveTo, Args: pt(x, 0)},
			{Op: gotextot.SegmentOpLineTo, Args: pt(x+100, 0)},
			{Op: gotextot.SegmentOpLineTo, Args: pt(x+100, 100)},
			{Op: gotextot.SegmentOpLineTo, Args: pt(x, 100)},
		}}
	}
	return &colrPainter{
		outline: func(gid tables.GlyphID) (font.GlyphOutline, bool) {
			switch gid {
			case 1:
				return square(0), true
			case 2:
				return square(50), true
			}
			return font.GlyphOutline{}, false
		},
		base: func(gid tables.GlyphID) (tables.PaintTable, bool) {
			if gid == 9 {
				return tables.PaintColrLayers{NumLayers: 2}, true
			}
			return nil, false
		},
		layers: func(p tables.PaintColrLayers) ([]tables.PaintTable, error) {
			if int(p.FirstLayerIndex)+int(p.NumLayers) > len(layers) {
				return nil, errors.New("out of bounds")
			}
			return layers[p.FirstLayerIndex : p.FirstLayerIndex+uint32(p.NumLayers)], nil
		},
		palette: []tables.ColorRecord{
			red:       {Red: 0xff, Alpha: 0xff},
			blue:      {Blue: 0xff, Alpha: 0xff},
			halfGreen: {Green: 0xff, Alpha: 0x80},
		},
	}
}

func solidPaint(index uint16) tables.PaintTable {
	return tables.PaintSolid{PaletteIndex: index, Alpha: one}
}

func outlined(gid uint16, paint tables.PaintTable) tables.PaintTable {
	return tables.PaintGlyph{GlyphID: gid, Paint: paint}
}

func redToBlue() tables.ColorLine {
	return tables.ColorLine{ColorStops: []tables.ColorStop{
		{StopOffset: 0, PaletteIndex: red, Alpha: one},
		{StopOffset: one, PaletteIndex: blue, Alpha: one},
	}}
}

// painted draws a paint at one pixel for a font unit, and returns a
// function that tells the color at a point given in font units, y up, and
// the box of the image in those units.
func painted(t *testing.T, p *colrPainter, paint tables.PaintTable) (at func(x, y int) color.RGBA, box image.Rectangle) {
	t.Helper()
	img, off, ok := p.paintImage(paint, 1, nil)
	if !ok {
		t.Fatal("nothing was drawn")
	}
	// The pixel row y of the image is the unit row -y-1.
	box = image.Rect(off.X, -(off.Y + img.Rect.Dy()), off.X+img.Rect.Dx(), -off.Y)
	return func(x, y int) color.RGBA {
		return img.RGBAAt(x-off.X, -y-1-off.Y)
	}, box
}

func near(a, b color.RGBA, tolerance int) bool {
	d := func(x, y uint8) bool { return int(x)-int(y) <= tolerance && int(y)-int(x) <= tolerance }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

var (
	opaqueRed  = color.RGBA{R: 0xff, A: 0xff}
	opaqueBlue = color.RGBA{B: 0xff, A: 0xff}
	nothing    = color.RGBA{}
	magenta    = color.RGBA{R: 0xff, B: 0xff, A: 0xff}
	black      = color.RGBA{A: 0xff}
	halfway    = color.RGBA{R: 0x7f, B: 0x80, A: 0xff}
)

func TestPaintSolidGlyph(t *testing.T) {
	at, box := painted(t, testPainter(), outlined(1, solidPaint(red)))
	if box != image.Rect(0, 0, 100, 100) {
		t.Errorf("box %v, want the square's", box)
	}
	for _, pt := range []image.Point{{1, 1}, {50, 50}, {98, 98}} {
		if c := at(pt.X, pt.Y); c != opaqueRed {
			t.Errorf("%v is %v, want red", pt, c)
		}
	}
}

func TestPaintLayersInOrder(t *testing.T) {
	p := testPainter(outlined(1, solidPaint(red)), outlined(2, solidPaint(blue)), outlined(1, solidPaint(halfGreen)))
	at, box := painted(t, p, tables.PaintColrLayers{NumLayers: 2})
	if box != image.Rect(0, 0, 150, 100) {
		t.Errorf("box %v, want both squares'", box)
	}
	if c := at(25, 50); c != opaqueRed {
		t.Errorf("the first layer alone is %v, want red", c)
	}
	if c := at(75, 50); c != opaqueBlue {
		t.Errorf("the layers' overlap is %v, want the later layer's blue", c)
	}
	// A translucent layer lets what is under it through.
	at, _ = painted(t, p, tables.PaintColrLayers{FirstLayerIndex: 1, NumLayers: 2})
	if c := at(75, 50); !near(c, color.RGBA{G: 0x80, B: 0x7f, A: 0xff}, 2) {
		t.Errorf("half green over blue is %v", c)
	}
	if c := at(25, 50); !near(c, color.RGBA{G: 0x80, A: 0x80}, 2) {
		t.Errorf("half green alone is %v", c)
	}
}

func TestPaintLinearGradient(t *testing.T) {
	for _, c := range []struct {
		name           string
		x1, y1, x2, y2 int16
		vertical       bool
	}{
		{"along x", 100, 0, 0, 100, false},
		{"along y", 0, 100, 100, 0, true},
		// p0p1 is a diagonal; the lines of one color are parallel to p0p2,
		// the y axis, so the gradient still runs along x.
		{"rotated", 100, 100, 0, 100, false},
	} {
		at, _ := painted(t, testPainter(), outlined(1, tables.PaintLinearGradient{ColorLine: redToBlue(), X1: c.x1, Y1: c.y1, X2: c.x2, Y2: c.y2}))
		along := func(v int) color.RGBA {
			if c.vertical {
				return at(50, v)
			}
			return at(v, 50)
		}
		across := func(v int) color.RGBA {
			if c.vertical {
				return at(v, 50)
			}
			return at(50, v)
		}
		if got := along(0); !near(got, opaqueRed, 4) {
			t.Errorf("%s: the start is %v, want red", c.name, got)
		}
		if got := along(99); !near(got, opaqueBlue, 4) {
			t.Errorf("%s: the end is %v, want blue", c.name, got)
		}
		if got := along(50); !near(got, halfway, 4) {
			t.Errorf("%s: the middle is %v, want half red, half blue", c.name, got)
		}
		if a, b := across(10), across(90); !near(a, b, 1) {
			t.Errorf("%s: the color changes across the gradient: %v, %v", c.name, a, b)
		}
	}
}

func TestPaintGradientExtend(t *testing.T) {
	// The line is a quarter of the square: pad keeps the last color after
	// it, repeat starts over, reflect turns back.
	gradient := func(extend tables.Extend) tables.PaintTable {
		line := redToBlue()
		line.Extend = extend
		return outlined(1, tables.PaintLinearGradient{ColorLine: line, X1: 25, X2: 0, Y2: 100})
	}
	at, _ := painted(t, testPainter(), gradient(tables.ExtendPad))
	if got := at(80, 50); got != opaqueBlue {
		t.Errorf("padded, after the line: %v, want blue", got)
	}
	at, _ = painted(t, testPainter(), gradient(tables.ExtendRepeat))
	if got := at(51, 50); !near(got, opaqueRed, 20) {
		t.Errorf("repeated, at the start of the third period: %v, want red", got)
	}
	at, _ = painted(t, testPainter(), gradient(tables.ExtendReflect))
	if got := at(49, 50); !near(got, opaqueRed, 20) {
		t.Errorf("reflected, at the end of the second period: %v, want red", got)
	}
	if got := at(26, 50); !near(got, opaqueBlue, 20) {
		t.Errorf("reflected, at the start of the second period: %v, want blue", got)
	}
}

func TestPaintRadialGradient(t *testing.T) {
	at, _ := painted(t, testPainter(), outlined(1, tables.PaintRadialGradient{ColorLine: redToBlue(), X0: 50, Y0: 50, X1: 50, Y1: 50, Radius1: 50}))
	if got := at(50, 50); !near(got, opaqueRed, 6) {
		t.Errorf("the center is %v, want red", got)
	}
	for _, pt := range []image.Point{{0, 50}, {99, 50}, {50, 0}, {50, 99}, {1, 1}} {
		if got := at(pt.X, pt.Y); !near(got, opaqueBlue, 6) {
			t.Errorf("%v, at the radius or past it, is %v, want blue", pt, got)
		}
	}
	if got := at(75, 50); !near(got, halfway, 6) {
		t.Errorf("half the radius is %v, want half red, half blue", got)
	}
}

func TestPaintSweepGradient(t *testing.T) {
	// From the x axis counter-clockwise, all the way round.
	at, _ := painted(t, testPainter(), outlined(1, tables.PaintSweepGradient{ColorLine: redToBlue(), CenterX: 50, CenterY: 50, StartAngle: 0, EndAngle: 0x7fff}))
	if got := at(50, 95); !near(got, color.RGBA{R: 0xbf, B: 0x40, A: 0xff}, 6) {
		t.Errorf("a quarter of the turn, up, is %v, want three quarters red", got)
	}
	if got := at(5, 50); !near(got, halfway, 6) {
		t.Errorf("half the turn, left, is %v, want half red", got)
	}
	if got := at(50, 5); !near(got, color.RGBA{R: 0x40, B: 0xbf, A: 0xff}, 6) {
		t.Errorf("three quarters of the turn, down, is %v, want a quarter red", got)
	}
}

func TestPaintTransforms(t *testing.T) {
	square := outlined(1, solidPaint(red))
	for _, c := range []struct {
		name  string
		paint tables.PaintTable
		box   image.Rectangle
	}{
		{"translate", tables.PaintTranslate{Paint: square, Dx: 50, Dy: -20}, image.Rect(50, -20, 150, 80)},
		{"scale", tables.PaintScale{Paint: square, ScaleX: one / 2, ScaleY: one + one/2}, image.Rect(0, 0, 50, 150)},
		{"scale around the center", tables.PaintScaleUniformAroundCenter{Paint: square, Scale: one / 2, CenterX: 50, CenterY: 50}, image.Rect(25, 25, 75, 75)},
		// A quarter of a turn counter-clockwise puts the square left of the
		// origin.
		{"rotate", tables.PaintRotate{Paint: square, Angle: one / 2}, image.Rect(-100, 0, 0, 100)},
		{"rotate around the center", tables.PaintRotateAroundCenter{Paint: square, Angle: one / 2, CenterX: 100, CenterY: 0}, image.Rect(0, -100, 100, 0)},
		{"matrix", tables.PaintTransform{Paint: square, Transform: tables.Affine2x3{Xx: 2, Yy: 1, Dx: 10, Dy: 5}}, image.Rect(10, 5, 210, 105)},
		// The transform nearest the glyph applies first.
		{"nested", tables.PaintTranslate{Paint: tables.PaintScaleUniform{Paint: square, Scale: one / 2}, Dx: 100}, image.Rect(100, 0, 150, 50)},
	} {
		at, box := painted(t, testPainter(), c.paint)
		if box != c.box {
			t.Errorf("%s: box %v, want %v", c.name, box, c.box)
			continue
		}
		center := c.box.Min.Add(c.box.Size().Div(2))
		if got := at(center.X, center.Y); got != opaqueRed {
			t.Errorf("%s: the center %v is %v, want red", c.name, center, got)
		}
	}
	// A gradient is transformed with its glyph: scaled to half, it ends at
	// half the square.
	at, _ := painted(t, testPainter(), tables.PaintScaleUniform{Scale: one / 2, Paint: outlined(1, tables.PaintLinearGradient{ColorLine: redToBlue(), X1: 100, Y2: 100})})
	if got := at(49, 25); !near(got, opaqueBlue, 8) {
		t.Errorf("the end of a gradient scaled to half is %v, want blue", got)
	}
}

func TestPaintNestedGlyphsClip(t *testing.T) {
	at, _ := painted(t, testPainter(), outlined(1, outlined(2, solidPaint(blue))))
	if got := at(75, 50); got != opaqueBlue {
		t.Errorf("inside both outlines: %v, want blue", got)
	}
	if got := at(25, 50); got != nothing {
		t.Errorf("inside the outer outline alone: %v, want nothing", got)
	}
}

func TestPaintComposite(t *testing.T) {
	backdrop, source := outlined(1, solidPaint(red)), outlined(2, solidPaint(blue))
	for _, c := range []struct {
		mode                      tables.CompositeMode
		backdrop, overlap, source color.RGBA
	}{
		{tables.CompositeSrcOver, opaqueRed, opaqueBlue, opaqueBlue},
		{tables.CompositeSrcIn, nothing, opaqueBlue, nothing},
		{tables.CompositeDestIn, nothing, opaqueRed, nothing},
		{tables.CompositeSrcOut, nothing, nothing, opaqueBlue},
		{tables.CompositeDestOut, opaqueRed, nothing, nothing},
		{tables.CompositeDestOver, opaqueRed, opaqueRed, opaqueBlue},
		{tables.CompositeXor, opaqueRed, nothing, opaqueBlue},
		{tables.CompositePlus, opaqueRed, magenta, opaqueBlue},
		{tables.CompositeMultiply, opaqueRed, black, opaqueBlue},
		{tables.CompositeScreen, opaqueRed, magenta, opaqueBlue},
		{tables.CompositeLighten, opaqueRed, magenta, opaqueBlue},
		{tables.CompositeDarken, opaqueRed, black, opaqueBlue},
	} {
		at, _ := painted(t, testPainter(), tables.PaintComposite{SourcePaint: source, CompositeMode: c.mode, BackdropPaint: backdrop})
		if got := at(25, 50); got != c.backdrop {
			t.Errorf("mode %d: the backdrop alone is %v, want %v", c.mode, got, c.backdrop)
		}
		if got := at(75, 50); got != c.overlap {
			t.Errorf("mode %d: the overlap is %v, want %v", c.mode, got, c.overlap)
		}
		if got := at(125, 50); got != c.source {
			t.Errorf("mode %d: the source alone is %v, want %v", c.mode, got, c.source)
		}
	}
	// A fill without an outline covers everything: inside a glyph's shape
	// it is how a font colors that shape.
	at, _ := painted(t, testPainter(), tables.PaintComposite{SourcePaint: solidPaint(blue), CompositeMode: tables.CompositeSrcIn, BackdropPaint: backdrop})
	if got := at(50, 50); got != opaqueBlue {
		t.Errorf("an unbounded source in the backdrop: %v, want blue", got)
	}
}

func TestPaintOfItselfEnds(t *testing.T) {
	// Glyph 9 is two layers, and one of them is glyph 9 again.
	p := testPainter(outlined(1, solidPaint(red)), tables.PaintColrGlyph{GlyphID: 9})
	at, _ := painted(t, p, tables.PaintColrGlyph{GlyphID: 9})
	if got := at(50, 50); got != opaqueRed {
		t.Errorf("the layer beside the endless one is %v, want red", got)
	}
}

func TestPaintClipBox(t *testing.T) {
	p := testPainter()
	img, off, ok := p.paintImage(outlined(1, solidPaint(red)), 1, &[4]float64{-10, 20, 60, 80})
	if !ok {
		t.Fatal("nothing was drawn")
	}
	if img.Rect.Size() != image.Pt(70, 60) || off != image.Pt(-10, -80) {
		t.Errorf("image %v at %v, want the clip box's 70x60 at (-10,-80)", img.Rect.Size(), off)
	}
	if got := img.RGBAAt(5, 30); got != nothing {
		t.Errorf("left of the square, in the box: %v, want nothing", got)
	}
	if got := img.RGBAAt(40, 30); got != opaqueRed {
		t.Errorf("in the square and the box: %v, want red", got)
	}
	// Twice the size for twice the scale.
	img, _, _ = p.paintImage(outlined(1, solidPaint(red)), 2, nil)
	if img.Rect.Size() != image.Pt(200, 200) {
		t.Errorf("at two pixels for a unit: %v", img.Rect.Size())
	}
}
