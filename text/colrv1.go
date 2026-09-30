// SPDX-License-Identifier: Unlicense OR MIT

package text

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"

	"github.com/go-text/typesetting/font"
	gotextot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"golang.org/x/image/vector"
)

// The paints of COLR version 1: a glyph is a tree of them, drawn here on
// the CPU into an image, once for a glyph and a size. Outlines clip, solid
// colors and gradients fill, transforms move what is under them, and a
// composite blends two subtrees. Variable paints are drawn as their default
// instance: the shaper sets no variation coordinates.

// affine maps (x, y) to (a*x + c*y + e, b*x + d*y + f).
type affine struct{ a, b, c, d, e, f float64 }

func (m affine) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

// mul returns the transform that applies t, then m.
func (m affine) mul(t affine) affine {
	return affine{
		a: m.a*t.a + m.c*t.b, b: m.b*t.a + m.d*t.b,
		c: m.a*t.c + m.c*t.d, d: m.b*t.c + m.d*t.d,
		e: m.a*t.e + m.c*t.f + m.e, f: m.b*t.e + m.d*t.f + m.f,
	}
}

func (m affine) inverse() (affine, bool) {
	det := m.a*m.d - m.b*m.c
	if math.Abs(det) < 1e-12 {
		return affine{}, false
	}
	return affine{
		a: m.d / det, b: -m.b / det,
		c: -m.c / det, d: m.a / det,
		e: (m.c*m.f - m.d*m.e) / det, f: (m.b*m.e - m.a*m.f) / det,
	}, true
}

func translation(dx, dy float64) affine { return affine{a: 1, d: 1, e: dx, f: dy} }

// around returns t applied about the center (cx, cy).
func around(t affine, cx, cy float64) affine {
	return translation(cx, cy).mul(t).mul(translation(-cx, -cy))
}

// f214 is the value of an F2DOT14 number.
func f214(v tables.Fixed214) float64 { return float64(v) / (1 << 14) }

// turns is the angle of an F2DOT14 number, 180° counter-clockwise for 1.0.
func turns(v tables.Fixed214) float64 { return f214(v) * math.Pi }

func rotation(angle float64) affine {
	sin, cos := math.Sincos(angle)
	return affine{a: cos, b: sin, c: -sin, d: cos}
}

func skew(x, y tables.Fixed214) affine {
	return affine{a: 1, b: math.Tan(turns(y)), c: -math.Tan(turns(x)), d: 1}
}

// paintTransform returns the transform of a transforming paint and what it
// transforms, or false for a paint of another kind.
func paintTransform(paint tables.PaintTable) (affine, tables.PaintTable, bool) {
	scale := func(x, y tables.Fixed214) affine { return affine{a: f214(x), d: f214(y)} }
	switch p := paint.(type) {
	case tables.PaintTransform:
		t := p.Transform
		return affine{float64(t.Xx), float64(t.Yx), float64(t.Xy), float64(t.Yy), float64(t.Dx), float64(t.Dy)}, p.Paint, true
	case tables.PaintVarTransform:
		t := p.Transform
		return affine{float64(t.Xx), float64(t.Yx), float64(t.Xy), float64(t.Yy), float64(t.Dx), float64(t.Dy)}, p.Paint, true
	case tables.PaintTranslate:
		return translation(float64(p.Dx), float64(p.Dy)), p.Paint, true
	case tables.PaintVarTranslate:
		return translation(float64(p.Dx), float64(p.Dy)), p.Paint, true
	case tables.PaintScale:
		return scale(p.ScaleX, p.ScaleY), p.Paint, true
	case tables.PaintVarScale:
		return scale(p.ScaleX, p.ScaleY), p.Paint, true
	case tables.PaintScaleAroundCenter:
		return around(scale(p.ScaleX, p.ScaleY), float64(p.CenterX), float64(p.CenterY)), p.Paint, true
	case tables.PaintVarScaleAroundCenter:
		return around(scale(p.ScaleX, p.ScaleY), float64(p.CenterX), float64(p.CenterY)), p.Paint, true
	case tables.PaintScaleUniform:
		return scale(p.Scale, p.Scale), p.Paint, true
	case tables.PaintVarScaleUniform:
		return scale(p.Scale, p.Scale), p.Paint, true
	case tables.PaintScaleUniformAroundCenter:
		return around(scale(p.Scale, p.Scale), float64(p.CenterX), float64(p.CenterY)), p.Paint, true
	case tables.PaintVarScaleUniformAroundCenter:
		return around(scale(p.Scale, p.Scale), float64(p.CenterX), float64(p.CenterY)), p.Paint, true
	case tables.PaintRotate:
		return rotation(turns(p.Angle)), p.Paint, true
	case tables.PaintVarRotate:
		return rotation(turns(p.Angle)), p.Paint, true
	case tables.PaintRotateAroundCenter:
		return around(rotation(turns(p.Angle)), float64(p.CenterX), float64(p.CenterY)), p.Paint, true
	case tables.PaintVarRotateAroundCenter:
		return around(rotation(turns(p.Angle)), float64(p.CenterX), float64(p.CenterY)), p.Paint, true
	case tables.PaintSkew:
		return skew(p.XSkewAngle, p.YSkewAngle), p.Paint, true
	case tables.PaintVarSkew:
		return skew(p.XSkewAngle, p.YSkewAngle), p.Paint, true
	case tables.PaintSkewAroundCenter:
		return around(skew(p.XSkewAngle, p.YSkewAngle), float64(p.CenterX), float64(p.CenterY)), p.Paint, true
	case tables.PaintVarSkewAroundCenter:
		return around(skew(p.XSkewAngle, p.YSkewAngle), float64(p.CenterX), float64(p.CenterY)), p.Paint, true
	}
	return affine{}, nil, false
}

// maxPaintDepth bounds the depth of a paint tree, which a font can make
// endless with a glyph that paints itself.
const maxPaintDepth = 32

// colrPainter draws the paints of one font.
type colrPainter struct {
	// outline is the outline of a glyph, in font units.
	outline func(gid tables.GlyphID) (font.GlyphOutline, bool)
	// base is the paint of a color glyph another paint refers to.
	base func(gid tables.GlyphID) (tables.PaintTable, bool)
	// layers are the paints of a PaintColrLayers.
	layers  func(tables.PaintColrLayers) ([]tables.PaintTable, error)
	palette []tables.ColorRecord

	// rect is the canvas: what is drawn is clipped to it.
	rect   image.Rectangle
	raster *vector.Rasterizer
}

// rgba is a premultiplied color with components in 0..1.
type rgba struct{ r, g, b, a float64 }

// paletteColor is the color of a palette entry with alpha applied. The
// entry 0xFFFF is the color of the text, which an image drawn once for
// every text does not know: it is black.
func (p *colrPainter) paletteColor(index uint16, alpha tables.Fixed214) rgba {
	c := tables.ColorRecord{Alpha: 0xff}
	if index != foregroundIndex {
		if int(index) >= len(p.palette) {
			return rgba{}
		}
		c = p.palette[index]
	}
	a := float64(c.Alpha) / 255 * min(max(f214(alpha), 0), 1)
	return rgba{float64(c.Red) / 255 * a, float64(c.Green) / 255 * a, float64(c.Blue) / 255 * a, a}
}

func (c rgba) color() color.RGBA64 {
	to := func(v float64) uint16 { return uint16(min(max(v, 0), 1)*0xffff + .5) }
	return color.RGBA64{R: to(c.r), G: to(c.g), B: to(c.b), A: to(c.a)}
}

// bounds is the box of the outlines of a paint tree, in the space m maps
// to. ok is false when the tree draws no outline.
func (p *colrPainter) bounds(paint tables.PaintTable, m affine, depth int) (minX, minY, maxX, maxY float64, ok bool) {
	minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	if depth > maxPaintDepth {
		return
	}
	union := func(paint tables.PaintTable, m affine) {
		x0, y0, x1, y1, sub := p.bounds(paint, m, depth+1)
		if sub {
			minX, minY, maxX, maxY, ok = min(minX, x0), min(minY, y0), max(maxX, x1), max(maxY, y1), true
		}
	}
	if t, sub, isTransform := paintTransform(paint); isTransform {
		union(sub, m.mul(t))
		return
	}
	switch paint := paint.(type) {
	case tables.PaintColrLayers:
		layers, err := p.layers(paint)
		if err != nil {
			return
		}
		for _, l := range layers {
			union(l, m)
		}
	case tables.PaintGlyph:
		outline, has := p.outline(tables.GlyphID(paint.GlyphID))
		if !has {
			return
		}
		for _, seg := range outline.Segments {
			for _, pt := range seg.ArgsSlice() {
				x, y := m.apply(float64(pt.X), float64(pt.Y))
				minX, minY, maxX, maxY, ok = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y), true
			}
		}
	case tables.PaintColrGlyph:
		if sub, has := p.base(tables.GlyphID(paint.GlyphID)); has {
			union(sub, m)
		}
	case tables.PaintComposite:
		union(paint.BackdropPaint, m)
		union(paint.SourcePaint, m)
	}
	return
}

// draw draws a paint over dst, through clip when it is not nil. m maps the
// paint's font units to the pixels of dst.
func (p *colrPainter) draw(dst *image.RGBA, paint tables.PaintTable, m affine, clip *image.Alpha, depth int) {
	if depth > maxPaintDepth || paint == nil {
		return
	}
	if t, sub, ok := paintTransform(paint); ok {
		p.draw(dst, sub, m.mul(t), clip, depth+1)
		return
	}
	switch paint := paint.(type) {
	case tables.PaintColrLayers:
		layers, err := p.layers(paint)
		if err != nil {
			return
		}
		for _, l := range layers {
			p.draw(dst, l, m, clip, depth+1)
		}
	case tables.PaintGlyph:
		mask := p.glyphMask(tables.GlyphID(paint.GlyphID), m)
		if mask == nil {
			return
		}
		if clip != nil {
			for i, v := range mask.Pix {
				mask.Pix[i] = uint8((uint32(v)*uint32(clip.Pix[i]) + 127) / 255)
			}
		}
		p.draw(dst, paint.Paint, m, mask, depth+1)
	case tables.PaintColrGlyph:
		if sub, ok := p.base(tables.GlyphID(paint.GlyphID)); ok {
			p.draw(dst, sub, m, clip, depth+1)
		}
	case tables.PaintComposite:
		backdrop := image.NewRGBA(p.rect)
		p.draw(backdrop, paint.BackdropPaint, m, clip, depth+1)
		source := image.NewRGBA(p.rect)
		p.draw(source, paint.SourcePaint, m, clip, depth+1)
		composite(backdrop, source, paint.CompositeMode)
		draw.Draw(dst, p.rect, backdrop, image.Point{}, draw.Over)
	case tables.PaintSolid:
		p.fill(dst, image.NewUniform(p.paletteColor(paint.PaletteIndex, paint.Alpha).color()), clip)
	case tables.PaintVarSolid:
		p.fill(dst, image.NewUniform(p.paletteColor(paint.PaletteIndex, paint.Alpha).color()), clip)
	default:
		if g := p.gradient(paint, m); g != nil {
			p.fill(dst, g, clip)
		}
	}
}

// fill draws src over the canvas, through clip when it is not nil.
func (p *colrPainter) fill(dst *image.RGBA, src image.Image, clip *image.Alpha) {
	if clip == nil {
		draw.Draw(dst, p.rect, src, image.Point{}, draw.Over)
		return
	}
	draw.DrawMask(dst, p.rect, src, image.Point{}, clip, image.Point{}, draw.Over)
}

// glyphMask is the coverage of a glyph's outline under m, nil for a glyph
// without one.
func (p *colrPainter) glyphMask(gid tables.GlyphID, m affine) *image.Alpha {
	outline, ok := p.outline(gid)
	if !ok || len(outline.Segments) == 0 {
		return nil
	}
	at := func(pt gotextot.SegmentPoint) (float32, float32) {
		x, y := m.apply(float64(pt.X), float64(pt.Y))
		return float32(x), float32(y)
	}
	r := p.raster
	r.Reset(p.rect.Dx(), p.rect.Dy())
	r.DrawOp = draw.Src
	for i, seg := range outline.Segments {
		switch seg.Op {
		case gotextot.SegmentOpMoveTo:
			// The rasterizer does not close a contour on its own.
			if i > 0 {
				r.ClosePath()
			}
			x, y := at(seg.Args[0])
			r.MoveTo(x, y)
		case gotextot.SegmentOpLineTo:
			x, y := at(seg.Args[0])
			r.LineTo(x, y)
		case gotextot.SegmentOpQuadTo:
			bx, by := at(seg.Args[0])
			cx, cy := at(seg.Args[1])
			r.QuadTo(bx, by, cx, cy)
		case gotextot.SegmentOpCubeTo:
			bx, by := at(seg.Args[0])
			cx, cy := at(seg.Args[1])
			dx, dy := at(seg.Args[2])
			r.CubeTo(bx, by, cx, cy, dx, dy)
		}
	}
	r.ClosePath()
	mask := image.NewAlpha(p.rect)
	r.Draw(mask, p.rect, image.Opaque, image.Point{})
	return mask
}

// colorStop is a stop of a gradient's color line.
type colorStop struct {
	offset float64
	color  rgba
}

// gradientImage is a gradient as the source of a fill: the color of a
// pixel is that of its center's place in the paint's space.
type gradientImage struct {
	// inverse maps pixels to the paint's font units.
	inverse affine
	stops   []colorStop
	extend  tables.Extend
	// at returns where on the color line a point is, false where the
	// gradient draws nothing.
	at func(x, y float64) (float64, bool)
}

func (g *gradientImage) ColorModel() color.Model { return color.RGBA64Model }

func (g *gradientImage) Bounds() image.Rectangle {
	return image.Rect(-1e9, -1e9, 1e9, 1e9)
}

func (g *gradientImage) At(x, y int) color.Color {
	px, py := g.inverse.apply(float64(x)+.5, float64(y)+.5)
	t, ok := g.at(px, py)
	if !ok {
		return color.RGBA64{}
	}
	return g.colorAt(t).color()
}

// colorAt is the color of the line at t. Colors are interpolated
// premultiplied.
func (g *gradientImage) colorAt(t float64) rgba {
	first, last := g.stops[0], g.stops[len(g.stops)-1]
	if span := last.offset - first.offset; span > 0 {
		switch g.extend {
		case tables.ExtendRepeat:
			t = first.offset + math.Mod(math.Mod(t-first.offset, span)+span, span)
		case tables.ExtendReflect:
			u := math.Mod(math.Mod(t-first.offset, 2*span)+2*span, 2*span)
			if u > span {
				u = 2*span - u
			}
			t = first.offset + u
		}
	}
	if t <= first.offset {
		return first.color
	}
	if t >= last.offset {
		return last.color
	}
	i := sort.Search(len(g.stops), func(i int) bool { return g.stops[i].offset > t })
	a, b := g.stops[i-1], g.stops[i]
	if b.offset <= a.offset {
		return b.color
	}
	u := (t - a.offset) / (b.offset - a.offset)
	mix := func(x, y float64) float64 { return x + (y-x)*u }
	return rgba{mix(a.color.r, b.color.r), mix(a.color.g, b.color.g), mix(a.color.b, b.color.b), mix(a.color.a, b.color.a)}
}

// gradient returns a gradient paint as the source of a fill, nil for a
// paint of another kind or a gradient that draws nothing.
func (p *colrPainter) gradient(paint tables.PaintTable, m affine) image.Image {
	inverse, ok := m.inverse()
	if !ok {
		return nil
	}
	g := &gradientImage{inverse: inverse}
	line := func(l tables.ColorLine) {
		g.extend = l.Extend
		for _, s := range l.ColorStops {
			g.stops = append(g.stops, colorStop{f214(s.StopOffset), p.paletteColor(s.PaletteIndex, s.Alpha)})
		}
	}
	varLine := func(l tables.VarColorLine) {
		g.extend = l.Extend
		for _, s := range l.ColorStops {
			g.stops = append(g.stops, colorStop{f214(s.StopOffset), p.paletteColor(s.PaletteIndex, s.Alpha)})
		}
	}
	switch paint := paint.(type) {
	case tables.PaintLinearGradient:
		line(paint.ColorLine)
		g.at = linearGradient(float64(paint.X0), float64(paint.Y0), float64(paint.X1), float64(paint.Y1), float64(paint.X2), float64(paint.Y2))
	case tables.PaintVarLinearGradient:
		varLine(paint.ColorLine)
		g.at = linearGradient(float64(paint.X0), float64(paint.Y0), float64(paint.X1), float64(paint.Y1), float64(paint.X2), float64(paint.Y2))
	case tables.PaintRadialGradient:
		line(paint.ColorLine)
		g.at = radialGradient(float64(paint.X0), float64(paint.Y0), float64(paint.Radius0), float64(paint.X1), float64(paint.Y1), float64(paint.Radius1))
	case tables.PaintVarRadialGradient:
		varLine(paint.ColorLine)
		g.at = radialGradient(float64(paint.X0), float64(paint.Y0), float64(paint.Radius0), float64(paint.X1), float64(paint.Y1), float64(paint.Radius1))
	case tables.PaintSweepGradient:
		line(paint.ColorLine)
		g.at = sweepGradient(float64(paint.CenterX), float64(paint.CenterY), turns(paint.StartAngle), turns(paint.EndAngle))
	case tables.PaintVarSweepGradient:
		varLine(paint.ColorLine)
		g.at = sweepGradient(float64(paint.CenterX), float64(paint.CenterY), turns(paint.StartAngle), turns(paint.EndAngle))
	default:
		return nil
	}
	if g.at == nil || len(g.stops) == 0 {
		return nil
	}
	sort.SliceStable(g.stops, func(i, j int) bool { return g.stops[i].offset < g.stops[j].offset })
	return g
}

// linearGradient is the gradient from p0 to p1 whose lines of one color
// are parallel to p0p2: nil when it has no direction.
func linearGradient(x0, y0, x1, y1, x2, y2 float64) func(x, y float64) (float64, bool) {
	dx, dy := x1-x0, y1-y0
	// The normal of p0p2, onto which p0p1 is projected.
	nx, ny := y2-y0, -(x2 - x0)
	nn := nx*nx + ny*ny
	if nn == 0 {
		return nil
	}
	k := (dx*nx + dy*ny) / nn
	vx, vy := nx*k, ny*k
	vv := vx*vx + vy*vy
	if vv == 0 {
		return nil
	}
	return func(x, y float64) (float64, bool) {
		return ((x-x0)*vx + (y-y0)*vy) / vv, true
	}
}

// radialGradient is the gradient between two circles: a point has the
// color of the largest t whose circle, of center c0 + t(c1 - c0) and radius
// r0 + t(r1 - r0) ≥ 0, passes through it.
func radialGradient(x0, y0, r0, x1, y1, r1 float64) func(x, y float64) (float64, bool) {
	cdx, cdy, dr := x1-x0, y1-y0, r1-r0
	a := cdx*cdx + cdy*cdy - dr*dr
	if cdx == 0 && cdy == 0 && dr == 0 {
		return nil
	}
	return func(x, y float64) (float64, bool) {
		pdx, pdy := x-x0, y-y0
		b := pdx*cdx + pdy*cdy + r0*dr
		c := pdx*pdx + pdy*pdy - r0*r0
		if math.Abs(a) < 1e-9 {
			if b == 0 {
				return 0, false
			}
			t := c / (2 * b)
			return t, r0+t*dr >= 0
		}
		disc := b*b - a*c
		if disc < 0 {
			return 0, false
		}
		root := math.Sqrt(disc)
		t1, t2 := (b+root)/a, (b-root)/a
		if t1 < t2 {
			t1, t2 = t2, t1
		}
		if r0+t1*dr >= 0 {
			return t1, true
		}
		if r0+t2*dr >= 0 {
			return t2, true
		}
		return 0, false
	}
}

// sweepGradient is the gradient around a center, from the angle start to
// end, counter-clockwise from the x axis.
func sweepGradient(cx, cy, start, end float64) func(x, y float64) (float64, bool) {
	if end == start {
		return nil
	}
	return func(x, y float64) (float64, bool) {
		angle := math.Atan2(y-cy, x-cx)
		if angle < 0 {
			angle += 2 * math.Pi
		}
		return (angle - start) / (end - start), true
	}
}

// composite blends source into backdrop, in place, as the mode says. The
// modes that are not separable by channel (hue, saturation, color and
// luminosity) are drawn as source over.
func composite(backdrop, source *image.RGBA, mode tables.CompositeMode) {
	for i := 0; i+3 < len(backdrop.Pix); i += 4 {
		d := rgba{float64(backdrop.Pix[i]) / 255, float64(backdrop.Pix[i+1]) / 255, float64(backdrop.Pix[i+2]) / 255, float64(backdrop.Pix[i+3]) / 255}
		s := rgba{float64(source.Pix[i]) / 255, float64(source.Pix[i+1]) / 255, float64(source.Pix[i+2]) / 255, float64(source.Pix[i+3]) / 255}
		o := blend(s, d, mode)
		to := func(v float64) uint8 { return uint8(min(max(v, 0), 1)*255 + .5) }
		// Premultiplied: a channel is never over alpha.
		a := min(max(o.a, 0), 1)
		backdrop.Pix[i], backdrop.Pix[i+1], backdrop.Pix[i+2], backdrop.Pix[i+3] = to(min(o.r, a)), to(min(o.g, a)), to(min(o.b, a)), to(a)
	}
}

// blend is one pixel of composite: s over d, premultiplied.
func blend(s, d rgba, mode tables.CompositeMode) rgba {
	// Porter-Duff: the source and the backdrop each take a fraction.
	duff := func(fs, fd float64) rgba {
		return rgba{s.r*fs + d.r*fd, s.g*fs + d.g*fd, s.b*fs + d.b*fd, s.a*fs + d.a*fd}
	}
	switch mode {
	case tables.CompositeClear:
		return rgba{}
	case tables.CompositeSrc:
		return s
	case tables.CompositeDest:
		return d
	case tables.CompositeDestOver:
		return duff(1-d.a, 1)
	case tables.CompositeSrcIn:
		return duff(d.a, 0)
	case tables.CompositeDestIn:
		return duff(0, s.a)
	case tables.CompositeSrcOut:
		return duff(1-d.a, 0)
	case tables.CompositeDestOut:
		return duff(0, 1-s.a)
	case tables.CompositeSrcAtop:
		return duff(d.a, 1-s.a)
	case tables.CompositeDestAtop:
		return duff(1-d.a, s.a)
	case tables.CompositeXor:
		return duff(1-d.a, 1-s.a)
	case tables.CompositePlus:
		return duff(1, 1)
	}
	var f func(cb, cs float64) float64
	switch mode {
	case tables.CompositeScreen:
		f = func(cb, cs float64) float64 { return cb + cs - cb*cs }
	case tables.CompositeOverlay:
		f = func(cb, cs float64) float64 { return hardLight(cs, cb) }
	case tables.CompositeDarken:
		f = func(cb, cs float64) float64 { return min(cb, cs) }
	case tables.CompositeLighten:
		f = func(cb, cs float64) float64 { return max(cb, cs) }
	case tables.CompositeColorDodge:
		f = func(cb, cs float64) float64 {
			if cb == 0 {
				return 0
			}
			if cs >= 1 {
				return 1
			}
			return min(1, cb/(1-cs))
		}
	case tables.CompositeColorBurn:
		f = func(cb, cs float64) float64 {
			if cb >= 1 {
				return 1
			}
			if cs <= 0 {
				return 0
			}
			return 1 - min(1, (1-cb)/cs)
		}
	case tables.CompositeHardLight:
		f = hardLight
	case tables.CompositeSoftLight:
		f = func(cb, cs float64) float64 {
			if cs <= .5 {
				return cb - (1-2*cs)*cb*(1-cb)
			}
			g := math.Sqrt(cb)
			if cb <= .25 {
				g = ((16*cb-12)*cb + 4) * cb
			}
			return cb + (2*cs-1)*(g-cb)
		}
	case tables.CompositeDifference:
		f = func(cb, cs float64) float64 { return math.Abs(cb - cs) }
	case tables.CompositeExclusion:
		f = func(cb, cs float64) float64 { return cb + cs - 2*cb*cs }
	case tables.CompositeMultiply:
		f = func(cb, cs float64) float64 { return cb * cs }
	default:
		return duff(1, 1-s.a)
	}
	// A blend mode mixes the colors where both are, and leaves each alone
	// elsewhere.
	channel := func(sc, dc float64) float64 {
		out := (1-d.a)*sc + (1-s.a)*dc
		if s.a > 0 && d.a > 0 {
			out += s.a * d.a * f(dc/d.a, sc/s.a)
		}
		return out
	}
	return rgba{channel(s.r, d.r), channel(s.g, d.g), channel(s.b, d.b), s.a + d.a - s.a*d.a}
}

func hardLight(cb, cs float64) float64 {
	if cs <= .5 {
		return cb * 2 * cs
	}
	return cb + (2*cs - 1) - cb*(2*cs-1)
}

// maxColorGlyphSide is the largest side of a color glyph's image, in
// pixels: a transform of a broken font could ask for any.
const maxColorGlyphSide = 2048

// paintImage draws a COLR version 1 glyph at scale pixels for a font unit.
// The image's origin is at off from the glyph's, in pixels with y down.
// clip is the glyph's clip box, in font units, when it has one; the image
// is as large as the box, or else as the outlines of the tree.
func (p *colrPainter) paintImage(paint tables.PaintTable, scale float64, clip *[4]float64) (img *image.RGBA, off image.Point, ok bool) {
	// Font units have y up, pixels down.
	device := affine{a: scale, d: -scale}
	var minX, minY, maxX, maxY float64
	if clip != nil {
		minX, maxY = device.apply(clip[0], clip[1])
		maxX, minY = device.apply(clip[2], clip[3])
	} else if minX, minY, maxX, maxY, ok = p.bounds(paint, device, 0); !ok {
		return nil, image.Point{}, false
	}
	// A rotation leaves whole numbers a hair off.
	const hair = 1e-6
	bounds := image.Rect(int(math.Floor(minX+hair)), int(math.Floor(minY+hair)), int(math.Ceil(maxX-hair)), int(math.Ceil(maxY-hair)))
	if bounds.Empty() || bounds.Dx() > maxColorGlyphSide || bounds.Dy() > maxColorGlyphSide {
		return nil, image.Point{}, false
	}
	off = bounds.Min
	p.rect = image.Rectangle{Max: bounds.Size()}
	p.raster = vector.NewRasterizer(p.rect.Dx(), p.rect.Dy())
	img = image.NewRGBA(p.rect)
	p.draw(img, paint, translation(float64(-off.X), float64(-off.Y)).mul(device), nil, 0)
	return img, off, true
}

// colorGlyphPaintImage draws a COLR version 1 glyph of face at scale pixels
// for a font unit.
func colorGlyphPaintImage(face *font.Face, gid font.GID, paint tables.PaintTable, scale float64) (*image.RGBA, image.Point, bool) {
	if face.COLR == nil || len(face.CPAL) == 0 {
		return nil, image.Point{}, false
	}
	p := &colrPainter{
		outline: face.GlyphDataOutline,
		base:    face.COLR.Search,
		layers:  face.COLR.LayerList.Resolve,
		palette: face.CPAL[0],
	}
	var clip *[4]float64
	if box, ok := face.COLR.ClipList.Search(tables.GlyphID(gid)); ok {
		switch box := box.(type) {
		case tables.ClipBoxFormat1:
			clip = &[4]float64{float64(box.XMin), float64(box.YMin), float64(box.XMax), float64(box.YMax)}
		case tables.ClipBoxFormat2:
			clip = &[4]float64{float64(box.XMin), float64(box.YMin), float64(box.XMax), float64(box.YMax)}
		}
	}
	return p.paintImage(paint, scale, clip)
}
