// SPDX-License-Identifier: Unlicense OR MIT

package text

import (
	"image"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"

	"gioui.org/font/opentype"
	"gioui.org/op"
)

// testEmojiImages has the pictures of two emoji: the grinning face, and
// the thumbs up of a skin tone, of two runes.
type testEmojiImages struct {
	// asked are the pictures asked for: their numbers and sizes.
	asked []image.Point
}

var testEmoji = [][]rune{[]rune("\U0001F600"), []rune("\U0001F44D\U0001F3FD")}

func (e *testEmojiImages) Match(text []rune) (int, int) {
	for id, emoji := range testEmoji {
		if len(text) >= len(emoji) && string(text[:len(emoji)]) == string(emoji) {
			return id, len(emoji)
		}
	}
	return 0, 0
}

func (e *testEmojiImages) Image(id, size int) image.Image {
	e.asked = append(e.asked, image.Pt(id, size))
	if id < 0 || id >= len(testEmoji) {
		return nil
	}
	return image.NewRGBA(image.Rect(0, 0, size, size))
}

// glyphsOf lays text out at 16 pixels for an em in width pixels, and
// returns its glyphs without the one that ends the paragraph.
func glyphsOf(s *Shaper, text string, width int) []Glyph {
	s.LayoutString(Parameters{PxPerEm: fixed.I(16), MaxWidth: width}, text)
	var out []Glyph
	for g, ok := s.NextGlyph(); ok; g, ok = s.NextGlyph() {
		out = append(out, g)
	}
	return out
}

func emojiShapers(t *testing.T) (with, without *Shaper, images *testEmojiImages) {
	face, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	collection := []FontFace{{Face: face, Font: face.Font()}}
	images = new(testEmojiImages)
	with = NewShaper(NoSystemFonts(), WithCollection(collection), WithEmojiImages(images))
	without = NewShaper(NoSystemFonts(), WithCollection(collection))
	return with, without, images
}

func TestEmojiImagesAreGlyphs(t *testing.T) {
	with, without, _ := emojiShapers(t)
	glyphs := glyphsOf(with, "a\U0001F600b\U0001F44D\U0001F3FDc", 1000)
	if len(glyphs) != 5 {
		t.Fatalf("%d glyphs, want a letter, a picture, a letter, a picture and a letter", len(glyphs))
	}
	_, letterFace, _ := splitGlyphID(glyphs[0].ID)
	for i, want := range []struct {
		picture bool
		id      int
		runes   uint16
	}{{false, 0, 1}, {true, 0, 1}, {false, 0, 1}, {true, 1, 2}, {false, 0, 1}} {
		g := glyphs[i]
		_, face, gid := splitGlyphID(g.ID)
		if picture := face != letterFace; picture != want.picture {
			t.Errorf("glyph %d is a picture: %v, want %v", i, picture, want.picture)
			continue
		}
		if g.Runes != want.runes {
			t.Errorf("glyph %d is of %d runes, want %d", i, g.Runes, want.runes)
		}
		if !want.picture {
			continue
		}
		if int(gid) != want.id+1 {
			t.Errorf("glyph %d has the number %d, want %d", i, gid, want.id+1)
		}
		if g.ID.Notdef() {
			t.Errorf("glyph %d, a picture, is taken for the glyph of a missing character", i)
		}
		// A square of 18/16 of the size, 3/16 of the size under the
		// baseline, with a sixteenth of room at each side.
		if size := g.Bounds.Max.Sub(g.Bounds.Min); size.X != fixed.I(18) || size.Y != fixed.I(18) {
			t.Errorf("glyph %d is %v, want 18 pixels square", i, size)
		}
		if g.Bounds.Max.Y != fixed.I(3) || g.Bounds.Min.X != fixed.I(1) || g.Advance != fixed.I(20) {
			t.Errorf("glyph %d: bottom %v, left %v, advance %v", i, g.Bounds.Max.Y, g.Bounds.Min.X, g.Advance)
		}
	}
	// The letters are where a shaper without pictures puts them, and as it
	// shapes them.
	plain := glyphsOf(without, "abc", 1000)
	for i, at := range []int{0, 2, 4} {
		if glyphs[at].ID != plain[i].ID || glyphs[at].Advance != plain[i].Advance {
			t.Errorf("letter %d is shaped another way beside pictures", i)
		}
	}
	// Without pictures the emoji are whatever the fonts make of them.
	for _, g := range glyphsOf(without, "\U0001F600", 1000) {
		if _, face, _ := splitGlyphID(g.ID); face != letterFace && g.Advance > 0 {
			t.Errorf("a shaper without pictures has a glyph of face %d", face)
		}
	}
}

func TestEmojiImagesWrap(t *testing.T) {
	with, _, _ := emojiShapers(t)
	// Each picture advances 20 pixels: five fit into 100, the sixth wraps.
	glyphs := glyphsOf(with, strings.Repeat("\U0001F600", 6), 100)
	lines := map[int32]int{}
	for _, g := range glyphs {
		if g.Advance > 0 {
			lines[g.Y]++
		}
	}
	if len(lines) != 2 {
		t.Fatalf("pictures on %d lines, want 2: %v", len(lines), lines)
	}
	if first := glyphs[0].Y; lines[first] != 5 {
		t.Errorf("%d pictures on the first line, want 5", lines[first])
	}
}

func TestEmojiImagesAreDrawnOnce(t *testing.T) {
	with, _, images := emojiShapers(t)
	glyphs := glyphsOf(with, "\U0001F600x\U0001F600", 1000)
	if call := with.Bitmaps(glyphs); call == (op.CallOp{}) {
		t.Fatal("nothing to draw for the pictures")
	}
	if len(images.asked) != 1 || images.asked[0] != image.Pt(0, 18) {
		t.Errorf("pictures asked for: %v, want the one of the emoji at 18 pixels, once", images.asked)
	}
	// Another text with the emoji takes the picture kept.
	with.Bitmaps(glyphsOf(with, "y\U0001F600", 1000))
	if len(images.asked) != 1 {
		t.Errorf("the picture was asked for again: %v", images.asked)
	}
	// Shape has no outline for a picture, and does not fail on it.
	with.Shape(glyphs)
}
