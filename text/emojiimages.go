// SPDX-License-Identifier: Unlicense OR MIT

package text

import (
	"image"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"gioui.org/f32"
	giofont "gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// EmojiImages draws emoji as pictures of its own instead of the glyphs of a
// font: a set of emoji sprites, as messengers have. The shaper asks it of
// every text, and lays a picture out as one glyph, a square a little larger
// than the text. It is used by the shapers of all windows at once.
type EmojiImages interface {
	// Match reports the emoji that text begins with: its number, from 0,
	// and how many runes it takes, 0 for none. The longest emoji wins.
	Match(text []rune) (id, length int)
	// Image returns the picture of an emoji, size pixels square, or nil.
	Image(id, size int) image.Image
}

// WithEmojiImages makes the shaper draw the emoji that images has with its
// pictures. The others are drawn with fonts, as without it.
func WithEmojiImages(images EmojiImages) ShaperOption {
	return func(s *Shaper) {
		s.config.emojiImages = images
	}
}

// useEmojiImages gives the shaper its pictures. They take the place of a
// face among the shaper's, so that a glyph tells it is one of them.
func (s *shaperImpl) useEmojiImages(images EmojiImages) {
	if images == nil {
		return
	}
	s.emojiImages = images
	// The face has no font of a file: only its identity is used.
	s.imageFace = &font.Face{Font: new(font.Font)}
	s.faceToIndex[s.imageFace.Font] = len(s.faces)
	s.imageFaceIndex = len(s.faces)
	// Shape and the font code pass a nil face over.
	s.faces = append(s.faces, nil)
	s.faceMeta = append(s.faceMeta, giofont.Font{})
}

// splitByEmojiImages cuts the emoji that have pictures out of the inputs,
// into inputs of the image face of one emoji each.
func (s *shaperImpl) splitByEmojiImages(inputs []shaping.Input) []shaping.Input {
	if s.emojiImages == nil {
		return inputs
	}
	var out []shaping.Input
	for n, input := range inputs {
		start := input.RunStart
		for i := input.RunStart; i < input.RunEnd; {
			_, length := s.emojiImages.Match(input.Text[i:input.RunEnd])
			if length <= 0 {
				i++
				continue
			}
			if out == nil {
				out = append(make([]shaping.Input, 0, len(inputs)+2), inputs[:n]...)
			}
			if i > start {
				text := input
				text.RunStart, text.RunEnd = start, i
				out = append(out, text)
			}
			emoji := input
			emoji.RunStart, emoji.RunEnd, emoji.Face = i, i+length, s.imageFace
			out = append(out, emoji)
			i += length
			start = i
		}
		if out == nil {
			continue
		}
		if start < input.RunEnd {
			rest := input
			rest.RunStart = start
			out = append(out, rest)
		}
	}
	if out == nil {
		return inputs
	}
	return out
}

// The box of an emoji picture, in sixteenths of the text's size: a little
// larger than the text, centered on its capitals, with a little room at
// its sides. It advances by one and a quarter of the size, as the emoji of
// fonts do: what is laid out for those fits these.
const (
	emojiImageSide    = 18
	emojiImageDescent = 3
	emojiImagePad     = 1
)

// shapeEmojiImage lays the emoji of an input of the image face out as one
// glyph, whose number is the picture's and one more: 0 is the glyph of a
// character no font has.
func (s *shaperImpl) shapeEmojiImage(input shaping.Input) shaping.Output {
	id, _ := s.emojiImages.Match(input.Text[input.RunStart:input.RunEnd])
	unit := input.Size / 16
	side, descent, pad := unit*emojiImageSide, unit*emojiImageDescent, unit*emojiImagePad
	bounds := shaping.Bounds{Ascent: side - descent, Descent: -descent}
	return shaping.Output{
		Advance: side + 2*pad,
		Size:    input.Size,
		Glyphs: []shaping.Glyph{{
			Width:        side,
			Height:       -side,
			XBearing:     pad,
			YBearing:     side - descent,
			Advance:      side + 2*pad,
			ClusterIndex: input.RunStart,
			RuneCount:    input.RunEnd - input.RunStart,
			GlyphCount:   1,
			GlyphID:      font.GID(id + 1),
		}},
		LineBounds:  bounds,
		GlyphBounds: bounds,
		Direction:   input.Direction,
		Runes:       shaping.Range{Offset: input.RunStart, Count: input.RunEnd - input.RunStart},
		Face:        s.imageFace,
	}
}

// emojiImageOps draws the picture of glyph g, of the image face, at x from
// the first glyph of its run.
func (s *shaperImpl) emojiImageOps(ops *op.Ops, g Glyph, x fixed.Int26_6) {
	_, _, gid := splitGlyphID(g.ID)
	shown, ok := s.bitmapGlyphCache.Get(g.ID)
	if !ok {
		size := (g.Bounds.Max.X - g.Bounds.Min.X).Round()
		if img := s.emojiImages.Image(int(gid)-1, size); img != nil && size > 0 {
			shown = bitmap{img: paint.NewImageOp(img), size: img.Bounds().Size()}
		}
		s.bitmapGlyphCache.Put(g.ID, shown)
	}
	if shown.size == (image.Point{}) {
		return
	}
	// On whole pixels: the picture is of the size it is shown at.
	off := op.Affine(f32.AffineId().Offset(f32.Point{
		X: float32((x - g.Offset.X + g.Bounds.Min.X).Round()),
		Y: float32((g.Bounds.Min.Y - g.Offset.Y).Round()),
	})).Push(ops)
	cl := clip.Rect{Max: shown.size}.Push(ops)
	shown.img.Add(ops)
	paint.PaintOp{}.Add(ops)
	cl.Pop()
	off.Pop()
}
